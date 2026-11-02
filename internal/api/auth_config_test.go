package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAuthConfigDefault(t *testing.T) {
	os.Setenv("AUTH_CONFIG_PATH", "/nonexistent/auth.yaml")
	defer os.Unsetenv("AUTH_CONFIG_PATH")
	cfg, err := LoadAuthConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	steps, ok := cfg["steps"].([]map[string]any)
	if !ok {
		if s2, ok2 := cfg["steps"].([]interface{}); ok2 {
			if len(s2) == 0 {
				t.Fatalf("expected non-empty steps")
			}
			return
		}
		t.Fatalf("unexpected steps type: %T", cfg["steps"])
	}
	if len(steps) == 0 {
		t.Fatalf("expected non-empty steps")
	}
}

func TestLoadAuthConfigVerifyAndSelect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.yaml")
	content := []byte(`
verify:
  url: "https://verify.example.com/verify-token"
  method: POST
  headers:
    Authorization: "Bearer {{token}}"
  clear_on_error: true
steps:
  - title: "Login"
    fields:
      - name: username
        type: text
        label: Username
        required: true
    api: "https://sso.example.com/login"
    method: POST
    response_field: "access_token"
  - title: "Select Account"
    fields:
      - name: accountId
        type: select
        options: "${accounts}"
        label: Account ID
        required: true
    preCalls:
      - url: "https://sso.example.com/accounts"
        method: GET
        response_mapping:
          accounts: "$.accounts"
    api: "https://sso.example.com/account-token"
    method: POST
    response_field: "access_token"
`)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write temp auth.yaml: %v", err)
	}
	t.Setenv("AUTH_CONFIG_PATH", path)
	cfg, err := LoadAuthConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	verify, ok := cfg["verify"].(map[string]any)
	if !ok {
		t.Fatalf("expected verify block, got %T", cfg["verify"])
	}
	if verify["url"] != "https://verify.example.com/verify-token" {
		t.Fatalf("unexpected verify url: %v", verify["url"])
	}
	if verify["clear_on_error"] != true {
		t.Fatalf("expected clear_on_error true, got %v", verify["clear_on_error"])
	}
	steps, ok := cfg["steps"].([]any)
	if !ok || len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %v", cfg["steps"])
	}
	step2, ok := steps[1].(map[string]any)
	if !ok {
		t.Fatalf("step 2 wrong type: %T", steps[1])
	}
	fields, ok := step2["fields"].([]any)
	if !ok || len(fields) != 1 {
		t.Fatalf("expected 1 field in step 2, got %v", step2["fields"])
	}
	f0, ok := fields[0].(map[string]any)
	if !ok {
		t.Fatalf("field wrong type: %T", fields[0])
	}
	if f0["type"] != "select" {
		t.Fatalf("expected select field, got %v", f0["type"])
	}
	if f0["options"] != "${accounts}" {
		t.Fatalf("expected options ${accounts}, got %v", f0["options"])
	}
	if _, ok := step2["preCalls"]; !ok {
		t.Fatalf("expected preCalls on step 2")
	}
}
