package models

import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestNormalizeURL(t *testing.T) {
	base := "http://flow:4501/flow-software/flow/instances/abc/server"
	for _, in := range []string{base, base + "/", base + "/api", base + "/api/", base + "/API/v1", "  " + base + "  "} {
		if got := NormalizeURL(in); got != base {
			t.Errorf("NormalizeURL(%q) = %q", in, got)
		}
	}
}

func TestLoadStripsBearerPrefix(t *testing.T) {
	s, err := LoadPluginSettings(backend.DataSourceInstanceSettings{
		URL:                     "http://flow:4501/x/server",
		JSONData:                []byte(`{"defaultCalendarId": 3}`),
		DecryptedSecureJSONData: map[string]string{"token": "Bearer abc.def"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Secrets.Token != "abc.def" || s.DefaultCalendarID != 3 {
		t.Errorf("settings = %+v token %q", s, s.Secrets.Token)
	}
	if err := s.Validate(); err != nil {
		t.Error(err)
	}
}

func TestFlowDatabaseSettings(t *testing.T) {
	s, err := LoadPluginSettings(backend.DataSourceInstanceSettings{
		URL:                     "http://flow:4501/x/server",
		JSONData:                []byte(`{"sqlServer": " sql01 ", "sqlDatabase": "Flow", "sqlUser": "grafana"}`),
		DecryptedSecureJSONData: map[string]string{"token": "t", "sqlPassword": "pw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.SQLConfigured() || s.SQLServer != "sql01" || s.Secrets.SQLPassword != "pw" {
		t.Errorf("settings = %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Error(err)
	}
	s.SQLUser = ""
	if err := s.Validate(); err == nil {
		t.Error("a server without a user accepted")
	}
	s.SQLUser, s.SQLEncrypt = "grafana", "maybe"
	if err := s.Validate(); err == nil {
		t.Error("unknown encryption accepted")
	}
	// Without a server the database is simply not used.
	if (&PluginSettings{}).SQLConfigured() {
		t.Error("empty settings configured")
	}
}

func TestValidate(t *testing.T) {
	for name, s := range map[string]PluginSettings{
		"no url":    {Secrets: &SecretPluginSettings{Token: "t"}},
		"no scheme": {URL: "flow:4501/server", Secrets: &SecretPluginSettings{Token: "t"}},
		"no token":  {URL: "http://flow:4501/server", Secrets: &SecretPluginSettings{}},
	} {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
