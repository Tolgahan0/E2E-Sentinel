package dockerclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ContainerConfig describes a container to create. It is deliberately a
// narrow subset of Docker's create options — only what a disposable test
// runner needs (spec §11.1): a fixed, pre-built image (never pulled at
// runtime — no arbitrary image pull surface), a read-only repository
// bind, a writable workspace bind, resource limits, and no network
// beyond what NetworkMode grants.
type ContainerConfig struct {
	Image      string
	Cmd        []string
	Env        []string
	WorkingDir string
	// Binds are "hostPath:containerPath[:ro]" strings.
	Binds []string
	// MemoryBytes and NanoCPUs bound resource usage; zero means "use the
	// daemon default" (not unlimited — operators should set a daemon-wide
	// default via daemon.json).
	MemoryBytes int64
	NanoCPUs    int64
	// NetworkMode "none" gives the runner no network access at all,
	// which is the safest default for a test runner that only needs to
	// reach a target already reachable via a bind-mounted config; set to
	// a real network name if the runner needs to reach other containers.
	NetworkMode string
	// User runs the process as this uid (e.g. "1000:1000") — never root
	// inside the runner container.
	User string
}

func (c *Client) post(ctx context.Context, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("dockerclient: encoding request body: %w", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker"+path, reader)
	if err != nil {
		return 0, fmt.Errorf("dockerclient: building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return res.StatusCode, fmt.Errorf("dockerclient: %s returned %d: %s", path, res.StatusCode, string(respBody))
	}

	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return res.StatusCode, fmt.Errorf("dockerclient: decoding response from %s: %w", path, err)
		}
	}
	return res.StatusCode, nil
}

// CreateContainer creates (but does not start) a container and returns
// its ID.
func (c *Client) CreateContainer(ctx context.Context, name string, cfg ContainerConfig) (string, error) {
	payload := map[string]any{
		"Image":      cfg.Image,
		"Cmd":        cfg.Cmd,
		"Env":        cfg.Env,
		"WorkingDir": cfg.WorkingDir,
		"User":       cfg.User,
		"HostConfig": map[string]any{
			"Binds":       cfg.Binds,
			"Memory":      cfg.MemoryBytes,
			"NanoCPUs":    cfg.NanoCPUs,
			"NetworkMode": cfg.NetworkMode,
			"AutoRemove":  false, // we remove explicitly, after collecting logs/artifacts
		},
	}

	var result struct {
		ID string `json:"Id"`
	}
	path := "/containers/create"
	if name != "" {
		path += "?name=" + name
	}
	if _, err := c.post(ctx, path, payload, &result); err != nil {
		return "", err
	}
	return result.ID, nil
}

// StartContainer starts a previously created container.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	_, err := c.post(ctx, "/containers/"+id+"/start", nil, nil)
	return err
}

// WaitResult is the outcome of waiting for a container to exit.
type WaitResult struct {
	StatusCode int
	Error      string
}

// WaitContainer blocks until the container exits (or ctx is cancelled —
// cancelling ctx here does not stop the container itself; call
// StopContainer for that).
func (c *Client) WaitContainer(ctx context.Context, id string) (WaitResult, error) {
	var result struct {
		StatusCode int `json:"StatusCode"`
		Error      *struct {
			Message string `json:"Message"`
		} `json:"Error"`
	}
	if _, err := c.post(ctx, "/containers/"+id+"/wait", nil, &result); err != nil {
		return WaitResult{}, err
	}
	wr := WaitResult{StatusCode: result.StatusCode}
	if result.Error != nil {
		wr.Error = result.Error.Message
	}
	return wr, nil
}

// StopContainer stops a running container (used for cancellation),
// giving it timeoutSeconds to exit gracefully before SIGKILL. Stopping
// an already-stopped or already-removed container is not an error —
// cancellation must be idempotent.
func (c *Client) StopContainer(ctx context.Context, id string, timeoutSeconds int) error {
	path := fmt.Sprintf("/containers/%s/stop?t=%d", id, timeoutSeconds)
	status, err := c.post(ctx, path, nil, nil)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

// RemoveContainer force-removes a container.
func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://docker/containers/"+id+"?force=true", nil)
	if err != nil {
		return fmt.Errorf("dockerclient: building request: %w", err)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("dockerclient: removing container %s returned %d: %s", id, res.StatusCode, string(body))
	}
	return nil
}

// ContainerLogs fetches the container's stdout and stderr, demultiplexed
// from Docker's framed log stream format (each frame: 1 byte stream
// type, 3 bytes padding, 4-byte big-endian length, then payload).
func (c *Client) ContainerLogs(ctx context.Context, id string) (stdout, stderr string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+id+"/logs?stdout=true&stderr=true", nil)
	if err != nil {
		return "", "", fmt.Errorf("dockerclient: building request: %w", err)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return "", "", fmt.Errorf("dockerclient: logs for %s returned %d: %s", id, res.StatusCode, string(body))
	}

	var outBuf, errBuf bytes.Buffer
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(res.Body, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return outBuf.String(), errBuf.String(), fmt.Errorf("dockerclient: reading log frame header: %w", err)
		}
		size := binary.BigEndian.Uint32(header[4:8])
		payload := make([]byte, size)
		if _, err := io.ReadFull(res.Body, payload); err != nil {
			return outBuf.String(), errBuf.String(), fmt.Errorf("dockerclient: reading log frame payload: %w", err)
		}
		switch header[0] {
		case 2:
			errBuf.Write(payload)
		default:
			outBuf.Write(payload)
		}
	}
	return outBuf.String(), errBuf.String(), nil
}
