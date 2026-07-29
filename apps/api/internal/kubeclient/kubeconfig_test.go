package kubeclient

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKubeconfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing kubeconfig fixture: %v", err)
	}
	return path
}

func TestLoadKubeconfig_TokenAuth(t *testing.T) {
	path := writeKubeconfig(t, `
current-context: test
clusters:
- name: test-cluster
  cluster:
    server: https://cluster.example.com:6443
    certificate-authority-data: `+base64.StdEncoding.EncodeToString([]byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----"))+`
contexts:
- name: test
  context:
    cluster: test-cluster
    user: test-user
    namespace: staging
users:
- name: test-user
  user:
    token: abc123token
`)

	cfg, err := LoadKubeconfig(path)
	if err != nil {
		t.Fatalf("LoadKubeconfig() error: %v", err)
	}
	if cfg.ServerURL != "https://cluster.example.com:6443" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Token != "abc123token" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if cfg.Namespace != "staging" {
		t.Errorf("Namespace = %q, want staging", cfg.Namespace)
	}
	if len(cfg.CACert) == 0 {
		t.Error("CACert not decoded")
	}
}

func TestLoadKubeconfig_ClientCertificateDataAuth(t *testing.T) {
	certB64 := base64.StdEncoding.EncodeToString([]byte("fake-cert"))
	keyB64 := base64.StdEncoding.EncodeToString([]byte("fake-key"))
	path := writeKubeconfig(t, `
current-context: test
clusters:
- name: test-cluster
  cluster:
    server: https://cluster.example.com:6443
    insecure-skip-tls-verify: true
contexts:
- name: test
  context:
    cluster: test-cluster
    user: test-user
users:
- name: test-user
  user:
    client-certificate-data: `+certB64+`
    client-key-data: `+keyB64+`
`)

	cfg, err := LoadKubeconfig(path)
	if err != nil {
		t.Fatalf("LoadKubeconfig() error: %v", err)
	}
	if string(cfg.ClientCert) != "fake-cert" || string(cfg.ClientKey) != "fake-key" {
		t.Errorf("ClientCert/Key = %q/%q", cfg.ClientCert, cfg.ClientKey)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be true")
	}
}

func TestLoadKubeconfig_ClientCertificateFileAuthResolvedRelativeToKubeconfig(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "client.crt"), []byte("file-cert"), 0o600)
	os.WriteFile(filepath.Join(dir, "client.key"), []byte("file-key"), 0o600)
	path := filepath.Join(dir, "kubeconfig.yaml")
	os.WriteFile(path, []byte(`
current-context: test
clusters:
- name: c
  cluster:
    server: https://cluster.example.com:6443
    insecure-skip-tls-verify: true
contexts:
- name: test
  context:
    cluster: c
    user: u
users:
- name: u
  user:
    client-certificate: client.crt
    client-key: client.key
`), 0o600)

	cfg, err := LoadKubeconfig(path)
	if err != nil {
		t.Fatalf("LoadKubeconfig() error: %v", err)
	}
	if string(cfg.ClientCert) != "file-cert" || string(cfg.ClientKey) != "file-key" {
		t.Errorf("ClientCert/Key = %q/%q", cfg.ClientCert, cfg.ClientKey)
	}
}

func TestLoadKubeconfig_MissingCurrentContext(t *testing.T) {
	path := writeKubeconfig(t, `clusters: []`)
	if _, err := LoadKubeconfig(path); err == nil {
		t.Fatal("expected an error for a kubeconfig with no current-context")
	}
}

func TestLoadKubeconfig_CurrentContextNotFound(t *testing.T) {
	path := writeKubeconfig(t, `
current-context: does-not-exist
contexts: []
`)
	if _, err := LoadKubeconfig(path); err == nil {
		t.Fatal("expected an error when current-context isn't in contexts")
	}
}

func TestLoadKubeconfig_ClusterNotFound(t *testing.T) {
	path := writeKubeconfig(t, `
current-context: test
clusters: []
contexts:
- name: test
  context:
    cluster: missing-cluster
    user: u
`)
	if _, err := LoadKubeconfig(path); err == nil {
		t.Fatal("expected an error when the context's cluster isn't in clusters")
	}
}

func TestLoadKubeconfig_ExecPluginAuthIsUnsupported(t *testing.T) {
	path := writeKubeconfig(t, `
current-context: test
clusters:
- name: c
  cluster:
    server: https://cluster.example.com:6443
contexts:
- name: test
  context:
    cluster: c
    user: u
users:
- name: u
  user:
    exec:
      command: aws-iam-authenticator
`)
	_, err := LoadKubeconfig(path)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v, want a clear unsupported-auth-method error", err)
	}
}

func TestLoadKubeconfig_MissingFileReturnsError(t *testing.T) {
	if _, err := LoadKubeconfig("/nonexistent/kubeconfig.yaml"); err == nil {
		t.Fatal("expected an error for a missing kubeconfig file")
	}
}

func TestLoadInCluster_ReadsStandardMountPaths(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "token"), []byte("sa-token\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("fake-ca"), 0o600)
	os.WriteFile(filepath.Join(dir, "namespace"), []byte("my-namespace\n"), 0o600)

	original := inClusterDir
	inClusterDir = dir
	t.Cleanup(func() { inClusterDir = original })

	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	cfg, err := LoadInCluster()
	if err != nil {
		t.Fatalf("LoadInCluster() error: %v", err)
	}
	if cfg.ServerURL != "https://10.0.0.1:443" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if cfg.Token != "sa-token" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if string(cfg.CACert) != "fake-ca" {
		t.Errorf("CACert = %q", cfg.CACert)
	}
	if cfg.Namespace != "my-namespace" {
		t.Errorf("Namespace = %q", cfg.Namespace)
	}
}

func TestLoadInCluster_MissingEnvReturnsError(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	if _, err := LoadInCluster(); err == nil {
		t.Fatal("expected an error when not running in-cluster")
	}
}

func TestDetect_ReturnsErrNotConfiguredWhenNothingAvailable(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	_, _, err := Detect("")
	if err != ErrNotConfigured {
		t.Fatalf("error = %v, want ErrNotConfigured", err)
	}
}

func TestDetect_PrefersExplicitKubeconfigPath(t *testing.T) {
	path := writeKubeconfig(t, `
current-context: test
clusters:
- name: c
  cluster:
    server: https://cluster.example.com:6443
    insecure-skip-tls-verify: true
contexts:
- name: test
  context:
    cluster: c
    user: u
users:
- name: u
  user:
    token: from-kubeconfig
`)
	client, cfg, err := Detect(path)
	if err != nil {
		t.Fatalf("Detect() error: %v", err)
	}
	if client == nil {
		t.Fatal("Detect() returned a nil client")
	}
	if cfg.Token != "from-kubeconfig" {
		t.Errorf("Token = %q", cfg.Token)
	}
}
