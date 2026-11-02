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
