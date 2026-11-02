package partner

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultPartnerPath = "./config/partner.yaml"

// Default endpoint paths used when a profile does not override them.
const (
	DefaultChannelListEndpoint   = "/api/v2/common/channel-playouts"
	DefaultChannelDetailEndpoint = "/api/v2/common/channel-playouts/{channel_id}"
	DefaultScheduleEndpoint      = "/api/v2/common/channel-epg/{epg_id}/schedules"
	defaultAPIBaseURL            = "http://localhost:3030"
	defaultAPITimeout            = "10s"
)

// Reserved base element names that custom fields may not collide with.
var reservedFieldNames = map[string]bool{
	"id": true, "name": true, "img": true,
	"start": true, "stop": true, "streamId": true, "ads": true, "channelId": true,
}

// PartnerConfig is the root of config/partner.yaml.
type PartnerConfig struct {
	Active   string                    `yaml:"active"`
	Partners map[string]PartnerProfile `yaml:"partners"`
}

// PartnerProfile describes one partner's upstream connection, auth flow, and XML extensions.
type PartnerProfile struct {
	API  PartnerAPI     `yaml:"api"`
	Auth map[string]any `yaml:"auth"`
	XML  PartnerXML     `yaml:"xml"`
}

// PartnerAPI holds the upstream connection details.
type PartnerAPI struct {
	BaseURL string            `yaml:"base_url"`
	Timeout string            `yaml:"timeout"`
	Headers map[string]string `yaml:"headers"`
	// Endpoints are fixed upstream paths (not operator-configurable); values are
	// always sourced from the defaults below.
	Endpoints        PartnerEndpoints  `yaml:"-"`
	ChannelListQuery map[string]string `yaml:"channel_list_query"`
	ScheduleQuery    map[string]string `yaml:"schedule_query"`
}

// PartnerEndpoints holds the upstream paths; {channel_id} and {epg_id} are substituted.
type PartnerEndpoints struct {
	ChannelList   string `yaml:"channel_list"`
	ChannelDetail string `yaml:"channel_detail"`
	Schedule      string `yaml:"schedule"`
}

// PartnerXML holds the declarative XML extensions for a profile.
type PartnerXML struct {
	Channel XMLExtension `yaml:"channel"`
	EPG     XMLExtension `yaml:"epg"`
}

// XMLExtension is an ordered list of custom fields appended to the base elements.
type XMLExtension struct {
	CustomFields []XMLField `yaml:"custom_fields"`
}

// XMLField is one declarative custom XML element.
type XMLField struct {
	Name    string `yaml:"name"`
	Source  string `yaml:"source"`
	Value   string `yaml:"value"`
	When    string `yaml:"when"`
	Default string `yaml:"default"`
}

// ResolveConfigPath returns the partner config path: PARTNER_CONFIG_PATH or the default.
func ResolveConfigPath() string {
	if p := os.Getenv("PARTNER_CONFIG_PATH"); p != "" {
		return p
	}
	return defaultPartnerPath
}

// ProfileFromBaseURL builds a default profile that points at baseURL. Used by
// callers (e.g. the admin partner client) that only need the default endpoints
// and timeout but want them sourced from the shared partner package.
func ProfileFromBaseURL(baseURL string) *PartnerProfile {
	api := PartnerAPI{BaseURL: baseURL}
	api.applyDefaults()
	return &PartnerProfile{API: api}
}

// LoadConfig loads PartnerConfig from ResolveConfigPath().
func LoadConfig() (*PartnerConfig, error) {
	return LoadConfigFile(ResolveConfigPath())
}

// LoadConfigFile loads and validates PartnerConfig from path.
func LoadConfigFile(path string) (*PartnerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read partner config %s: %w", path, err)
	}
	var cfg PartnerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse partner config %s: %w", path, err)
	}
	for id := range cfg.Partners {
		prof := cfg.Partners[id]
		prof.API.applyDefaults()
		cfg.Partners[id] = prof
	}
	return &cfg, nil
}

// Profile returns the profile with the given id.
func (c *PartnerConfig) Profile(id string) (PartnerProfile, bool) {
	if c == nil || c.Partners == nil {
		return PartnerProfile{}, false
	}
	p, ok := c.Partners[id]
	return p, ok
}

// ActiveProfile returns the active profile or an error when `active` does not resolve.
func (c *PartnerConfig) ActiveProfile() (PartnerProfile, error) {
	if c == nil {
		return PartnerProfile{}, fmt.Errorf("partner config is nil")
	}
	if c.Active == "" {
		return PartnerProfile{}, fmt.Errorf("partner config has no active profile")
	}
	p, ok := c.Partners[c.Active]
	if !ok {
		return PartnerProfile{}, fmt.Errorf("active profile %q not found in partners", c.Active)
	}
	return p, nil
}

// applyDefaults fills in unset connection fields.
func (a *PartnerAPI) applyDefaults() {
	if a.BaseURL == "" {
		if env := os.Getenv("PARTNER_API_URL"); env != "" {
			a.BaseURL = env
		} else {
			a.BaseURL = defaultAPIBaseURL
		}
	}
	a.BaseURL = strings.TrimRight(a.BaseURL, "/")
	if a.Timeout == "" {
		a.Timeout = defaultAPITimeout
	}
	// Upstream paths are fixed, not operator-configurable.
	a.Endpoints.ChannelList = DefaultChannelListEndpoint
	a.Endpoints.ChannelDetail = DefaultChannelDetailEndpoint
	a.Endpoints.Schedule = DefaultScheduleEndpoint
}

// TimeoutDuration returns the parsed timeout, falling back to the default on error.
func (a PartnerAPI) TimeoutDuration() time.Duration {
	d, err := time.ParseDuration(a.Timeout)
	if err != nil || d <= 0 {
		return 10 * time.Second
	}
	return d
}

// ValidCustomFields returns the valid subset of fields plus one error per
// invalid field. Invalid fields are dropped so the base XML stays well-formed
// (FR-016); the caller logs the returned errors for operators.
func ValidCustomFields(fields []XMLField) ([]XMLField, []error) {
	valid := make([]XMLField, 0, len(fields))
	var errs []error
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if err := f.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[f.Name] {
			errs = append(errs, fmt.Errorf("custom field %q is declared more than once", f.Name))
			continue
		}
		seen[f.Name] = true
		valid = append(valid, f)
	}
	return valid, errs
}

// Validate checks a custom field's name, source/value exclusivity, and `when` value.
func (f XMLField) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("custom field has no name")
	}
	if !validXMLName(f.Name) {
		return fmt.Errorf("custom field %q is not a valid XML name", f.Name)
	}
	if reservedFieldNames[f.Name] {
		return fmt.Errorf("custom field %q collides with a reserved base name", f.Name)
	}
	if f.Source != "" && f.Value != "" {
		return fmt.Errorf("custom field %q sets both source and value", f.Name)
	}
	if f.Source == "" && f.Value == "" {
		return fmt.Errorf("custom field %q sets neither source nor value", f.Name)
	}
	switch f.When {
	case "", "always", "metadata":
	default:
		return fmt.Errorf("custom field %q has invalid when %q", f.Name, f.When)
	}
	return nil
}

// validXMLName reports whether s is a valid XML element name.
func validXMLName(s string) bool {
	for i, r := range s {
		switch {
		case r == '_' || r == ':' || r == '-' || r == '.':
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return s != ""
}
