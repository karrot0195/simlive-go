package partner

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleConfig = `
active: alpha
partners:
  alpha:
    api:
      base_url: "http://alpha.example.com:3030"
      timeout: "5s"
      headers:
        Accept: "application/json"
        X-Partner: "alpha"
      endpoints:
        channel_list: "/cli"
        channel_detail: "/cli/{channel_id}"
        schedule: "/epg/{epg_id}/schedules"
      channel_list_query:
        "$limit": "10"
      schedule_query:
        "$skip": "0"
    auth:
      steps:
        - title: "Login"
    xml:
      channel:
        custom_fields:
          - name: "provider"
            value: "alpha"
  beta:
    api:
      base_url: "http://beta.example.com:3030"
`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestLoadConfigFileMissing(t *testing.T) {
	_, err := LoadConfigFile(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfigFileParsesProfiles(t *testing.T) {
	cfg, err := LoadConfigFile(writeTemp(t, "partner.yaml", sampleConfig))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Active != "alpha" {
		t.Fatalf("active = %q, want alpha", cfg.Active)
	}
	if len(cfg.Partners) != 2 {
		t.Fatalf("partners = %d, want 2", len(cfg.Partners))
	}
}

func TestResolveConfigPathPrefersEnv(t *testing.T) {
	path := writeTemp(t, "custom.yaml", sampleConfig)
	t.Setenv("PARTNER_CONFIG", path)
	if got := ResolveConfigPath(); got != path {
		t.Fatalf("ResolveConfigPath = %q, want %q", got, path)
	}
}

func TestResolveConfigPathDefault(t *testing.T) {
	t.Setenv("PARTNER_CONFIG", "")
	if got := ResolveConfigPath(); got != defaultPartnerPath {
		t.Fatalf("ResolveConfigPath = %q, want %q", got, defaultPartnerPath)
	}
}

func TestLoadConfigUsesEnvPath(t *testing.T) {
	path := writeTemp(t, "env-partner.yaml", sampleConfig)
	t.Setenv("PARTNER_CONFIG", path)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Active != "alpha" {
		t.Fatalf("active = %q, want alpha", cfg.Active)
	}
}

func TestActiveProfile(t *testing.T) {
	cfg, err := LoadConfigFile(writeTemp(t, "partner.yaml", sampleConfig))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	prof, err := cfg.ActiveProfile()
	if err != nil {
		t.Fatalf("ActiveProfile: %v", err)
	}
	if prof.API.BaseURL != "http://alpha.example.com:3030" {
		t.Fatalf("base url = %q", prof.API.BaseURL)
	}
}

func TestActiveProfileUnknown(t *testing.T) {
	cfg, err := LoadConfigFile(writeTemp(t, "partner.yaml", sampleConfig))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.Active = "ghost"
	if _, err := cfg.ActiveProfile(); err == nil {
		t.Fatal("expected error for unknown active profile")
	}
}

func TestProfileLookup(t *testing.T) {
	cfg, _ := LoadConfigFile(writeTemp(t, "partner.yaml", sampleConfig))
	if _, ok := cfg.Profile("beta"); !ok {
		t.Fatal("expected beta profile")
	}
	if _, ok := cfg.Profile("ghost"); ok {
		t.Fatal("did not expect ghost profile")
	}
}

func TestAPIApplyDefaults(t *testing.T) {
	t.Setenv("TZ", "UTC")
	var api PartnerAPI
	api.applyDefaults()
	if api.BaseURL != "http://localhost:3030" {
		t.Fatalf("base url default = %q", api.BaseURL)
	}
	if api.Timeout != "10s" {
		t.Fatalf("timeout default = %q", api.Timeout)
	}
	if api.Endpoints.ChannelList == "" || api.Endpoints.ChannelDetail == "" || api.Endpoints.Schedule == "" {
		t.Fatalf("endpoint defaults not applied: %+v", api.Endpoints)
	}
}


func TestXMLFieldValidation(t *testing.T) {
	cases := []struct {
		name    string
		field   XMLField
		wantErr bool
	}{
		{"valid source", XMLField{Name: "title", Source: "programme.name"}, false},
		{"valid value", XMLField{Name: "provider", Value: "a"}, false},
		{"reserved id", XMLField{Name: "id", Value: "x"}, true},
		{"reserved streamId", XMLField{Name: "streamId", Value: "x"}, true},
		{"both source and value", XMLField{Name: "x", Source: "a", Value: "b"}, true},
		{"neither source nor value", XMLField{Name: "x"}, true},
		{"bad when", XMLField{Name: "x", Value: "y", When: "sometimes"}, true},
		{"metadata when", XMLField{Name: "x", Value: "y", When: "metadata"}, false},
		{"empty name", XMLField{Value: "y"}, true},
		{"invalid xml name", XMLField{Name: "1bad name", Value: "y"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.field.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadConfigFile_DataProviderDefaults(t *testing.T) {
	data := []byte("active: alpha\npartners:\n  alpha:\n    api:\n      base_url: http://x\n")
	path := writeTemp(t, "partner.yaml", string(data))
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Partners["alpha"].DataProvider != "rclone" {
		t.Fatalf("data provider = %q, want rclone", cfg.Partners["alpha"].DataProvider)
	}
}

func TestLoadConfigFile_DataProviderHTTP(t *testing.T) {
	data := []byte("active: alpha\npartners:\n  alpha:\n    api:\n      base_url: http://x\n    data_provider: http\n    data:\n      endpoint: http://example.com/data\n")
	path := writeTemp(t, "partner.yaml", string(data))
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Partners["alpha"].DataProvider != "http" {
		t.Fatalf("data provider = %q, want http", cfg.Partners["alpha"].DataProvider)
	}
}

func TestLoadConfigFile_UnknownDataProvider(t *testing.T) {
	data := []byte("active: alpha\npartners:\n  alpha:\n    api:\n      base_url: http://x\n    data_provider: ftp\n")
	path := writeTemp(t, "partner.yaml", string(data))
	if _, err := LoadConfigFile(path); err == nil {
		t.Fatal("expected error for unknown data provider")
	}
}

func TestLoadConfigFile_HTTPMissingEndpoint(t *testing.T) {
	data := []byte("active: alpha\npartners:\n  alpha:\n    api:\n      base_url: http://x\n    data_provider: http\n")
	path := writeTemp(t, "partner.yaml", string(data))
	if _, err := LoadConfigFile(path); err == nil {
		t.Fatal("expected error for missing http endpoint")
	}
}
