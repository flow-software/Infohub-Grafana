package flow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateAcceptsEmptyUntypedBody(t *testing.T) {
	// Flow's token/validate answers 200 with no body and no Content-Type.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	if err := c.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHTMLAnswerNamesTheURLProblem(t *testing.T) {
	// Bootstrap serves the web app's page for a wrong instance path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
	_, err := c.Calendars(context.Background())
	if err == nil || !strings.Contains(err.Error(), "instances/<InstanceID>/server") {
		t.Fatalf("err = %v", err)
	}
}
