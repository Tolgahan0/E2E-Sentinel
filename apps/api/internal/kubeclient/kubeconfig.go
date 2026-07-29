package kubeclient

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrNotConfigured is returned by Detect when neither a kubeconfig path
// nor an in-cluster ServiceAccount mount is available — Kubernetes
// discovery is simply off, the same "safe default, explicit capability"
// state as an unset SENTINEL_DOCKER_SOCKET-equivalent.
var ErrNotConfigured = errors.New("kubeclient: kubernetes is not configured")

// Config is a resolved (auth + connection) target, built by
// LoadKubeconfig, LoadInCluster, or Detect.
type Config struct {
	ServerURL          string
	Token              string
	CACert             []byte // PEM
	ClientCert         []byte // PEM
	ClientKey          []byte // PEM
	InsecureSkipVerify bool
	// Namespace is the default namespace named by the kubeconfig
	// context (or the in-cluster ServiceAccount's own namespace) — the
	// caller decides whether/how to use it as a scoping default.
	Namespace string
}

// InClusterServiceAccountDir is the standard mount point for a pod's
// projected ServiceAccount credentials — the read-only ClusterRole
// deployment scenario spec §7.5/§24.5 describes.
const InClusterServiceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// inClusterDir is a variable (not the constant directly) purely so
// tests can point it at a temporary directory instead of the real,
// root-owned system path.
var inClusterDir = InClusterServiceAccountDir

// Detect picks in-cluster credentials when kubeConfigPath is empty and
// the standard ServiceAccount mount is present, otherwise loads
// kubeConfigPath, otherwise returns ErrNotConfigured. This is the single
// entry point cmd/sentinel/main.go should use.
func Detect(kubeConfigPath string) (*Client, Config, error) {
	var cfg Config
	var err error

	switch {
	case kubeConfigPath != "":
		cfg, err = LoadKubeconfig(kubeConfigPath)
	case inClusterAvailable():
		cfg, err = LoadInCluster()
	default:
		return nil, Config{}, ErrNotConfigured
	}
	if err != nil {
		return nil, Config{}, err
	}

	client, err := New(cfg)
	if err != nil {
		return nil, Config{}, err
	}
	return client, cfg, nil
}

func inClusterAvailable() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != "" && os.Getenv("KUBERNETES_SERVICE_PORT") != ""
}

// LoadInCluster builds a Config from the standard projected
// ServiceAccount mount — used when sentinel-api itself runs as a pod
// inside the cluster it discovers (spec §24.5's read-only-ServiceAccount
// deployment).
func LoadInCluster() (Config, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return Config{}, fmt.Errorf("kubeclient: not running in-cluster (KUBERNETES_SERVICE_HOST/KUBERNETES_SERVICE_PORT unset)")
	}

	token, err := os.ReadFile(filepath.Join(inClusterDir, "token"))
	if err != nil {
		return Config{}, fmt.Errorf("kubeclient: reading in-cluster service account token: %w", err)
	}
	ca, err := os.ReadFile(filepath.Join(inClusterDir, "ca.crt"))
	if err != nil {
		return Config{}, fmt.Errorf("kubeclient: reading in-cluster CA certificate: %w", err)
	}
	namespace, _ := os.ReadFile(filepath.Join(inClusterDir, "namespace"))

	return Config{
		ServerURL: "https://" + net.JoinHostPort(host, port),
		Token:     strings.TrimSpace(string(token)),
		CACert:    ca,
		Namespace: strings.TrimSpace(string(namespace)),
	}, nil
}

// kubeconfigFile is the subset of a real kubeconfig's schema this
// package understands.
type kubeconfigFile struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthority     string `yaml:"certificate-authority"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster   string `yaml:"cluster"`
			User      string `yaml:"user"`
			Namespace string `yaml:"namespace"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Token                 string         `yaml:"token"`
			ClientCertificate     string         `yaml:"client-certificate"`
			ClientCertificateData string         `yaml:"client-certificate-data"`
			ClientKey             string         `yaml:"client-key"`
			ClientKeyData         string         `yaml:"client-key-data"`
			Exec                  map[string]any `yaml:"exec"`
			AuthProvider          map[string]any `yaml:"auth-provider"`
		} `yaml:"user"`
	} `yaml:"users"`
}

// LoadKubeconfig parses a kubeconfig YAML file and resolves its
// current-context into a Config. Only bearer-token and client-
// certificate authentication are supported — exec plugins (cloud
// provider CLIs) and auth-provider plugins are deliberately
// unsupported, per this package's "keep the capability surface small"
// philosophy; such a kubeconfig produces a clear error rather than a
// silent partial connection.
func LoadKubeconfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("kubeclient: reading kubeconfig %s: %w", path, err)
	}

	var kc kubeconfigFile
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return Config{}, fmt.Errorf("kubeclient: parsing kubeconfig %s: %w", path, err)
	}
	if kc.CurrentContext == "" {
		return Config{}, fmt.Errorf("kubeclient: kubeconfig %s has no current-context", path)
	}

	var ctxClusterName, ctxUserName, namespace string
	found := false
	for _, c := range kc.Contexts {
		if c.Name == kc.CurrentContext {
			ctxClusterName, ctxUserName, namespace = c.Context.Cluster, c.Context.User, c.Context.Namespace
			found = true
			break
		}
	}
	if !found {
		return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: current-context %q not found in contexts", path, kc.CurrentContext)
	}

	var cluster *struct {
		Server                   string
		CertificateAuthority     string
		CertificateAuthorityData string
		InsecureSkipTLSVerify    bool
	}
	for _, c := range kc.Clusters {
		if c.Name == ctxClusterName {
			cluster = &struct {
				Server                   string
				CertificateAuthority     string
				CertificateAuthorityData string
				InsecureSkipTLSVerify    bool
			}{c.Cluster.Server, c.Cluster.CertificateAuthority, c.Cluster.CertificateAuthorityData, c.Cluster.InsecureSkipTLSVerify}
			break
		}
	}
	if cluster == nil {
		return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: cluster %q (from context %q) not found", path, ctxClusterName, kc.CurrentContext)
	}
	if cluster.Server == "" {
		return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: cluster %q has no server URL", path, ctxClusterName)
	}

	cfg := Config{
		ServerURL:          cluster.Server,
		InsecureSkipVerify: cluster.InsecureSkipTLSVerify,
		Namespace:          namespace,
	}

	switch {
	case cluster.CertificateAuthorityData != "":
		ca, err := base64.StdEncoding.DecodeString(cluster.CertificateAuthorityData)
		if err != nil {
			return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: certificate-authority-data is not valid base64: %w", path, err)
		}
		cfg.CACert = ca
	case cluster.CertificateAuthority != "":
		ca, err := os.ReadFile(resolveRelative(path, cluster.CertificateAuthority))
		if err != nil {
			return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: reading certificate-authority: %w", path, err)
		}
		cfg.CACert = ca
	}

	if ctxUserName != "" {
		for _, u := range kc.Users {
			if u.Name != ctxUserName {
				continue
			}
			user := u.User
			switch {
			case user.Token != "":
				cfg.Token = user.Token
			case user.ClientCertificateData != "" && user.ClientKeyData != "":
				cert, err := base64.StdEncoding.DecodeString(user.ClientCertificateData)
				if err != nil {
					return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: client-certificate-data is not valid base64: %w", path, err)
				}
				key, err := base64.StdEncoding.DecodeString(user.ClientKeyData)
				if err != nil {
					return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: client-key-data is not valid base64: %w", path, err)
				}
				cfg.ClientCert, cfg.ClientKey = cert, key
			case user.ClientCertificate != "" && user.ClientKey != "":
				cert, err := os.ReadFile(resolveRelative(path, user.ClientCertificate))
				if err != nil {
					return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: reading client-certificate: %w", path, err)
				}
				key, err := os.ReadFile(resolveRelative(path, user.ClientKey))
				if err != nil {
					return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: reading client-key: %w", path, err)
				}
				cfg.ClientCert, cfg.ClientKey = cert, key
			case len(user.Exec) > 0 || len(user.AuthProvider) > 0:
				return Config{}, fmt.Errorf("kubeclient: kubeconfig %s: user %q uses an exec/auth-provider plugin, which is not supported — use a plain bearer token or client certificate instead", path, ctxUserName)
			}
			break
		}
	}

	return cfg, nil
}

// resolveRelative resolves a kubeconfig-relative file path (certificate-
// authority/client-certificate/client-key may be given relative to the
// kubeconfig file's own directory, matching kubectl's behavior) against
// kubeconfigPath's directory.
func resolveRelative(kubeconfigPath, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(kubeconfigPath), target)
}
