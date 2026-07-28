package redaction

import (
	"strings"
	"testing"
)

func TestRedact_AuthorizationHeader(t *testing.T) {
	input := "GET /api/v1/orders HTTP/1.1\nAuthorization: Bearer sk-live-abc123def456\nHost: example.com"
	got := Redact(input)

	if strings.Contains(got.Text, "sk-live-abc123def456") {
		t.Fatalf("secret leaked into redacted text: %q", got.Text)
	}
	if !containsCategory(got.Categories, CategoryAuthHeader) {
		t.Errorf("Categories = %v, want to include %q", got.Categories, CategoryAuthHeader)
	}
}

func TestRedact_CookieHeader(t *testing.T) {
	input := "Cookie: session=abc123; other=xyz"
	got := Redact(input)

	if strings.Contains(got.Text, "abc123") {
		t.Fatalf("cookie value leaked: %q", got.Text)
	}
	if !containsCategory(got.Categories, CategoryCookie) {
		t.Errorf("Categories = %v, want to include %q", got.Categories, CategoryCookie)
	}
}

func TestRedact_PrivateKeyBlock(t *testing.T) {
	input := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----\nafter"
	got := Redact(input)

	if strings.Contains(got.Text, "MIIEpAIBAAKCAQEA") {
		t.Fatalf("private key material leaked: %q", got.Text)
	}
	if !strings.Contains(got.Text, "before") || !strings.Contains(got.Text, "after") {
		t.Errorf("surrounding text should be preserved, got: %q", got.Text)
	}
	if !containsCategory(got.Categories, CategorySecret) {
		t.Errorf("Categories = %v, want to include %q", got.Categories, CategorySecret)
	}
}

func TestRedact_AWSAccessKey(t *testing.T) {
	got := Redact("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE")
	if strings.Contains(got.Text, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("AWS key leaked: %q", got.Text)
	}
	if !containsCategory(got.Categories, CategorySecret) {
		t.Errorf("Categories = %v, want to include %q", got.Categories, CategorySecret)
	}
}

func TestRedact_APIKeyAssignment(t *testing.T) {
	got := Redact(`api_key: "abcdef1234567890ghijkl"`)
	if strings.Contains(got.Text, "abcdef1234567890ghijkl") {
		t.Fatalf("api key leaked: %q", got.Text)
	}
	if !containsCategory(got.Categories, CategorySecret) {
		t.Errorf("Categories = %v, want to include %q", got.Categories, CategorySecret)
	}
}

func TestRedact_PasswordAssignment(t *testing.T) {
	got := Redact("password=hunter2verysecret")
	if strings.Contains(got.Text, "hunter2verysecret") {
		t.Fatalf("password leaked: %q", got.Text)
	}
	if !containsCategory(got.Categories, CategoryCredential) {
		t.Errorf("Categories = %v, want to include %q", got.Categories, CategoryCredential)
	}
}

func TestRedact_URLCredentials(t *testing.T) {
	got := Redact("postgres://sentinel:supersecret@db.internal:5432/sentinel?sslmode=disable")
	if strings.Contains(got.Text, "supersecret") {
		t.Fatalf("URL credential leaked: %q", got.Text)
	}
	if !strings.Contains(got.Text, "postgres://") || !strings.Contains(got.Text, "db.internal:5432") {
		t.Errorf("scheme and host should be preserved, got: %q", got.Text)
	}
	if !containsCategory(got.Categories, CategoryCredential) {
		t.Errorf("Categories = %v, want to include %q", got.Categories, CategoryCredential)
	}
}

func TestRedact_JWT(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	got := Redact("token=" + jwt)
	if strings.Contains(got.Text, jwt) {
		t.Fatalf("JWT leaked: %q", got.Text)
	}
	if !containsCategory(got.Categories, CategoryToken) {
		t.Errorf("Categories = %v, want to include %q", got.Categories, CategoryToken)
	}
}

func TestRedact_BearerTokenOutsideHeader(t *testing.T) {
	got := Redact("curl -H 'Authorization: Bearer abc123def456' but also logged raw: Bearer zzz999yyy888")
	if strings.Contains(got.Text, "zzz999yyy888") {
		t.Fatalf("bearer token leaked: %q", got.Text)
	}
}

func TestRedact_LeavesOrdinaryTextUnchanged(t *testing.T) {
	input := "This is a normal log line with no secrets in it."
	got := Redact(input)
	if got.Text != input {
		t.Errorf("Text = %q, want unchanged %q", got.Text, input)
	}
	if len(got.Categories) != 0 {
		t.Errorf("Categories = %v, want empty", got.Categories)
	}
}

func TestRedact_CategoriesAreSortedAndDeduped(t *testing.T) {
	got := Redact("password=one\npassword=two\nCookie: a=b")
	if len(got.Categories) != 2 {
		t.Fatalf("Categories = %v, want 2 unique entries", got.Categories)
	}
	if !sortedAscending(got.Categories) {
		t.Errorf("Categories = %v, want sorted", got.Categories)
	}
}

func containsCategory(categories []Category, want Category) bool {
	for _, c := range categories {
		if c == want {
			return true
		}
	}
	return false
}

func sortedAscending(categories []Category) bool {
	for i := 1; i < len(categories); i++ {
		if categories[i-1] > categories[i] {
			return false
		}
	}
	return true
}

func TestPathAllowed(t *testing.T) {
	allowlist := []string{"src/", "README.md"}

	cases := map[string]bool{
		"src/index.ts":       true,
		"src":                false,
		"README.md":          true,
		"secrets/.env":       false,
		"src/nested/file.go": true,
	}
	for path, want := range cases {
		if got := PathAllowed(path, allowlist); got != want {
			t.Errorf("PathAllowed(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestPathAllowed_EmptyAllowlistDeniesEverything(t *testing.T) {
	if PathAllowed("src/index.ts", nil) {
		t.Error("empty allowlist should deny every path")
	}
}

func TestWithinSizeLimit(t *testing.T) {
	if !WithinSizeLimit(100, 200) {
		t.Error("100 should be within a 200 byte limit")
	}
	if WithinSizeLimit(300, 200) {
		t.Error("300 should exceed a 200 byte limit")
	}
	if !WithinSizeLimit(DefaultMaxFileBytes, 0) {
		t.Error("a 0 limit should fall back to DefaultMaxFileBytes")
	}
	if WithinSizeLimit(DefaultMaxFileBytes+1, 0) {
		t.Error("exceeding DefaultMaxFileBytes should fail when limit falls back to it")
	}
}
