package flowdb

import (
	"net/url"
	"testing"
)

func TestConnString(t *testing.T) {
	c := Config{Server: `sql01\FLOW`, Database: "Flow", User: "grafana", Password: "p@ss;word", TrustServerCertificate: true}
	u, err := url.Parse(c.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	pw, _ := u.User.Password()
	if u.Scheme != "sqlserver" || u.Host != "sql01" || u.Path != "/FLOW" || u.User.Username() != "grafana" || pw != "p@ss;word" {
		t.Errorf("url = %s", u)
	}
	// Login-only encryption by default, as SQL Server clients do.
	if q.Get("database") != "Flow" || q.Get("encrypt") != "false" || q.Get("TrustServerCertificate") != "true" {
		t.Errorf("query = %v", q)
	}

	c = Config{Server: "sql01:1433", Database: "Flow", User: "u", Encrypt: "strict"}
	u, _ = url.Parse(c.ConnString())
	if u.Host != "sql01:1433" || u.Path != "" || u.Query().Get("encrypt") != "strict" || u.Query().Has("TrustServerCertificate") {
		t.Errorf("url = %s", u)
	}
}

func TestUpToDate(t *testing.T) {
	if !(Processing{}).UpToDate() || (Processing{Queued: 1}).UpToDate() || (Processing{Inactive: true}).UpToDate() {
		t.Error("UpToDate")
	}
}
