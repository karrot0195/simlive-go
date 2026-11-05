package api

import (
	"os"
	"path/filepath"
	"testing"
)

const partnerAuthYAML = `
active: default
partners:
  default:
    api:
      base_url: "http://localhost:3030"
    auth:
      verify:
        url: "https://verify.partner.example.com/verify-token"
        method: POST
        clear_on_error: true
      steps:
        - title: "Login"
          api: "https://sso.partner.example.com/login"
          method: POST
          response_field: "access_token"
        - title: "Select Account"
          fields:
            - name: accountId
              type: select
              options: "${accounts}"
              required: true
          preCalls:
            - url: "https://sso.partner.example.com/accounts"
              method: GET
          api: "https://sso.partner.example.com/account-token"
          method: POST
          response_field: "access_token"
`

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// stepCount returns the number of login steps regardless of the concrete slice
// type (YAML yields []any; the built-in default uses []map[string]any).
func stepCount(v any) int {
	switch s := v.(type) {
	case []any:
		return len(s)
	case []map[string]any:
		return len(s)
	default:
		return -1
	}
}

func TestAuthConfigFromPartnerYAML(t *testing.T) {
	t.Setenv("PARTNER_CONFIG", writeFile(t, "partner.yaml", partnerAuthYAML))
	t.Setenv("AUTH_CONFIG_PATH", "")

	cfg, err := LoadAuthConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	verify, ok := cfg["verify"].(map[string]any)
	if !ok {
		t.Fatalf("expected verify block, got %T", cfg["verify"])
	}
	if verify["url"] != "https://verify.partner.example.com/verify-token" {
		t.Fatalf("unexpected verify url: %v", verify["url"])
	}
	if n := stepCount(cfg["steps"]); n != 2 {
		t.Fatalf("expected 2 steps from partner.yaml, got %v", cfg["steps"])
	}
}

func TestAuthConfigFallsBackToLegacyAuthYAML(t *testing.T) {
	t.Setenv("PARTNER_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	legacy := writeFile(t, "auth.yaml", "steps:\n  - title: \"Legacy\"\n    api: \"https://legacy.example.com/login\"\n")
	t.Setenv("AUTH_CONFIG_PATH", legacy)

	cfg, err := LoadAuthConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := stepCount(cfg["steps"]); n != 1 {
		t.Fatalf("expected legacy steps, got %v", cfg["steps"])
	}
}

func TestAuthConfigInvalidPartnerFallsBackToDefault(t *testing.T) {
	t.Setenv("PARTNER_CONFIG", writeFile(t, "partner.yaml", "this: is: not: valid: yaml: ["))
	t.Setenv("AUTH_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-auth.yaml"))

	cfg, err := LoadAuthConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := stepCount(cfg["steps"]); n == 0 || n == -1 {
		t.Fatalf("expected built-in default steps, got %v", cfg["steps"])
	}
}

func TestAuthConfigUnusableAuthBlockFallsBack(t *testing.T) {
	// partner.yaml with an auth block that has no steps.
	content := "active: default\npartners:\n  default:\n    auth:\n      verify:\n        url: \"https://x/verify\"\n"
	t.Setenv("PARTNER_CONFIG", writeFile(t, "partner.yaml", content))
	t.Setenv("AUTH_CONFIG_PATH", filepath.Join(t.TempDir(), "missing-auth.yaml"))

	cfg, err := LoadAuthConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := stepCount(cfg["steps"]); n == 0 || n == -1 {
		t.Fatalf("expected built-in default steps, got %v", cfg["steps"])
	}
}

func TestAuthConfigMissingPartnerFallsBackToDefault(t *testing.T) {
	t.Setenv("PARTNER_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("AUTH_CONFIG_PATH", "/nonexistent/auth.yaml")

	cfg, err := LoadAuthConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := stepCount(cfg["steps"]); n == 0 || n == -1 {
		t.Fatalf("expected built-in default steps, got %v", cfg["steps"])
	}
}
