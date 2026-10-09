package models

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// PluginSettings is the datasource configuration. TLS options (skip verify,
// CA certificate) use Grafana's standard jsonData/secureJsonData keys and are
// read by the SDK's HTTP client options, so they are not repeated here.
type PluginSettings struct {
	// URL is the Flow Server base, normalised to end without "/api", e.g.
	// http://flow:4501/flow-software/flow/instances/<InstanceID>/server
	URL string `json:"-"`
	// DefaultCalendarID is used by queries that do not pick a calendar. Zero
	// means the first calendar Flow lists.
	DefaultCalendarID int `json:"defaultCalendarId"`

	Secrets *SecretPluginSettings `json:"-"`
}

type SecretPluginSettings struct {
	Token string
}

func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}
	if len(source.JSONData) > 0 {
		if err := json.Unmarshal(source.JSONData, &settings); err != nil {
			return nil, fmt.Errorf("could not unmarshal PluginSettings json: %w", err)
		}
	}
	settings.URL = NormalizeURL(source.URL)

	// People paste the whole header value; the "Bearer " prefix is added on send.
	token := strings.TrimSpace(source.DecryptedSecureJSONData["token"])
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	settings.Secrets = &SecretPluginSettings{Token: token}
	return &settings, nil
}

// NormalizeURL trims whitespace and trailing slashes, and drops a trailing
// "/api" or "/api/v1" so the Swagger page address can be pasted as-is.
func NormalizeURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range []string{"/api/v1", "/api"} {
		if strings.HasSuffix(strings.ToLower(u), suffix) {
			u = strings.TrimRight(u[:len(u)-len(suffix)], "/")
			break
		}
	}
	return u
}

// Validate reports the first missing setting, worded for the config page.
func (s *PluginSettings) Validate() error {
	if s.URL == "" {
		return fmt.Errorf("URL is required, for example http://flow:4501/flow-software/flow/instances/<InstanceID>/server")
	}
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("URL must be an http or https address, for example http://flow:4501/flow-software/flow/instances/<InstanceID>/server")
	}
	if s.Secrets.Token == "" {
		return fmt.Errorf("API token is required: create a server token in Flow Config")
	}
	return nil
}
