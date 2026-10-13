package plugin

import (
	"context"
	"net/http"
	"testing"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
)

func TestParseMeasureID(t *testing.T) {
	for text, want := range map[string]int{"4304": 4304, " #4304 ": 4304, "id:4304": 4304, "ID 4304": 4304, "id: 12": 12} {
		if got, ok := parseMeasureID(text); !ok || got != want {
			t.Errorf("parseMeasureID(%q) = %d, %v", text, got, ok)
		}
	}
	for _, text := range []string{"Good", "4304a", "0", "-3", "#", "idle"} {
		if got, ok := parseMeasureID(text); ok {
			t.Errorf("parseMeasureID(%q) = %d, want no id", text, got)
		}
	}
}

// A number finds the measure with that id first, then names containing it.
func TestSearchByMeasureID(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/search"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"objects": []map[string]any{
			{"id": 20, "name": "Production 300", "type": "Metric"},
			{"id": 300, "name": "Good", "type": "Measure"}, // the id match again
		}})
	}
	d := testDatasource(srv)
	nodes, err := d.search(context.Background(), flow.TreeModel, "300")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %+v, want the measure once and the metric", nodes)
	}
	if n := nodes[0]; n.Type != itemMeasure || n.ID != 300 || n.Name != "Site/Line 2/Production/Good" || n.Interval != "hourly" {
		t.Errorf("first = %+v, want measure 300 by its path", n)
	}
	if nodes[1].Type != itemMetric || nodes[1].ID != 20 {
		t.Errorf("second = %+v", nodes[1])
	}

	// A measure found by name gets its interval from its metadata, for its icon.
	f.handlers["/api/v1/search"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"objects": []map[string]any{{"id": 201, "name": "Speed", "type": "Measure"}}})
	}
	nodes, err = d.search(context.Background(), flow.TreeModel, "Speed")
	if err != nil || len(nodes) != 1 || nodes[0].Interval != "timestamp" {
		t.Errorf("by name = %+v, %v", nodes, err)
	}

	// Flow cannot name-search "#300"; the id match still answers.
	f.handlers["/api/v1/search"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 400, map[string]any{}) }
	nodes, err = d.search(context.Background(), flow.TreeModel, "#300")
	if err != nil || len(nodes) != 1 || nodes[0].ID != 300 {
		t.Errorf("nodes = %+v, err %v", nodes, err)
	}

	// An unknown id is not an error: name matches still show.
	f.handlers["/api/v1/search"] = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"objects": []any{}}) }
	if nodes, err = d.search(context.Background(), flow.TreeModel, "777"); err != nil || len(nodes) != 0 {
		t.Errorf("unknown id: nodes = %+v, err %v", nodes, err)
	}
}

// Listed measures carry their retrieval type, for Flow's retrieval marks.
func TestBrowseCarriesRetrieval(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/model/metrics/20/measures"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"measures": []map[string]any{
			{"id": 200, "name": "Production.Good", "interval": "hourly", "retrieval": "calculated"},
			{"id": 201, "name": "Production.Speed", "interval": "timestamp", "retrieval": "retrieved"},
		}})
	}
	d := testDatasource(srv)
	nodes, err := d.browse(context.Background(), flow.TreeModel, itemMetric, 20)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]string{}
	for _, n := range nodes {
		got[n.ID] = n.Retrieval
	}
	if got[200] != "calculated" || got[201] != "retrieved" {
		t.Errorf("retrieval = %v", got)
	}
}
