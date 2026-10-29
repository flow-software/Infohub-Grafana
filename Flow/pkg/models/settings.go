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

	// The Flow database, optional: dependency queries read how far the
	// engine has got with each measure from it (the public API does not
	// expose that). SQLServer is host, host:port or host\instance.
	SQLServer                 string `json:"sqlServer"`
	SQLDatabase               string `json:"sqlDatabase"`
	SQLUser                   string `json:"sqlUser"`
	SQLEncrypt                string `json:"sqlEncrypt"`
	SQLTrustServerCertificate bool   `json:"sqlTrustServerCertificate"`

	Secrets *SecretPluginSettings `json:"-"`
}

type SecretPluginSettings struct {
	Token       string
	SQLPassword string
}

// SQLConfigured reports whether the optional Flow database connection is set.
func (s *PluginSettings) SQLConfigured() bool { return strings.TrimSpace(s.SQLServer) != "" }

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
	settings.Secrets = &SecretPluginSettings{Token: token, SQLPassword: source.DecryptedSecureJSONData["sqlPassword"]}
	settings.SQLServer = strings.TrimSpace(settings.SQLServer)
	settings.SQLDatabase = strings.TrimSpace(settings.SQLDatabase)
	settings.SQLUser = strings.TrimSpace(settings.SQLUser)
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
		return fmt.Errorf("the Flow host and instance ID are required: enter them under Connection")
	}
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("URL must be an http or https address, for example http://flow:4501/flow-software/flow/instances/<InstanceID>/server")
	}
	if s.Secrets.Token == "" {
		return fmt.Errorf("API token is required: create a server token in Flow Config")
	}
	if s.SQLConfigured() && (s.SQLDatabase == "" || s.SQLUser == "") {
		return fmt.Errorf("the Flow database needs a database name and a user, or leave its server empty")
	}
	switch s.SQLEncrypt {
	case "", "false", "true", "strict", "disable":
	default:
		return fmt.Errorf("unknown Flow database encryption %q", s.SQLEncrypt)
	}
	return nil
}
