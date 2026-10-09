package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// depModel is a small model of how measures are built:
//
//	10 OEE        = calculation of 11 and 12
//	11 Good       aggregation (sum, hourly) of 13
//	12 Planned    set to measure 13
//	13 Count      a tag
//	14 Broken     Flow answers 500 for its metadata
//	15 Shift sum  aggregation of event scheme 7
//	16, 17        a calculation cycle
//	18 FQ-001.PV  retrieved from Flow tag 13; its own name has a dot
//	19 Target     a calculation of 10, nested under it
//	20 WTD        a calculation of 19, nested under it
//
// 10, 18, 19 and 20 sit in metric 5 "Line"; the rest have no metric, so
// their names stand as Flow gives them.
var depMetadata = map[int][]map[string]any{
	10: {{"type": "calculation", "data": map[string]any{"1970/01/01 00:00:00": "Good / Planned", "2026/01/01 00:00:00": "Good / Planned * 100", "dependents": []int{11, 12}}}},
	11: {{"type": "aggregation", "data": map[string]any{"type": "sum", "interval": "hourly", "measure": 13, "scaling": 1}}},
	12: {{"type": "manual", "data": map[string]any{"type": "SetToMeasure", "id": 13}}},
	13: {{"type": "retrieval", "data": map[string]any{"source": "Sim", "tag": "FQ.PV", "aggregation": "Average"}}},
	15: {{"type": "aggregation", "data": map[string]any{"type": "sum", "interval": "daily", "event": 7}}},
	16: {{"type": "calculation", "data": map[string]any{"dependents": []int{17}}}},
	17: {{"type": "calculation", "data": map[string]any{"dependents": []int{16}}}},
	18: {{"type": "retrieval", "data": map[string]any{"source": "Tag", "id": 13, "aggregation": "Average"}}},
	19: {{"type": "calculation", "data": map[string]any{"dependents": []int{10}}}},
	20: {{"type": "calculation", "data": map[string]any{"dependents": []int{19}}}},
}

var depMetricOf = map[int]int{10: 5, 18: 5, 19: 5, 20: 5}
var depParentOf = map[int]int{19: 10, 20: 19}

// depMetric5Names is metric 5's measure list: whole names, unlike 20's
// by-id name, which skips its grandparent as Flow's does.
var depMetric5Names = map[int]string{10: "Line.OEE", 18: "Line.FQ-001.PV", 19: "Line.OEE.Target", 20: "Line.OEE.Target.WTD"}

// depRetrievals overrides the fake's default retrieval, "calculated".
var depRetrievals = map[int]string{11: "aggregated", 12: "manual", 13: "retrieved", 18: "retrieved"}

var depNamesByID = map[int]string{10: "Line.OEE", 11: "Line.Good", 12: "Line.Planned", 13: "Line.Count", 14: "Line.Broken", 15: "Line.Shift sum", 16: "Loop.A", 17: "Loop.B", 18: "Line.FQ-001.PV", 19: "Line.OEE.Target", 20: "Line.Target.WTD"}

func depServer(t *testing.T) (*fakeFlow, *Datasource) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/model/measures"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ids := q["id"]
		if len(ids) == 0 { // all measures
			for id := range depNamesByID {
				ids = append(ids, strconv.Itoa(id))
			}
		}
		var out []map[string]any
		for _, s := range ids {
			id, _ := strconv.Atoi(s)
			if id == 14 && q.Get("metadata") == "true" {
				writeJSON(w, 500, map[string]any{})
				return
			}
			name, ok := depNamesByID[id]
			if !ok {
				continue
			}
			retrieval := depRetrievals[id]
			if retrieval == "" {
				retrieval = "calculated"
			}
			m := map[string]any{"id": id, "name": name, "retrieval": retrieval, "interval": "hourly"}
			if metric := depMetricOf[id]; metric != 0 {
				m["metric"] = map[string]any{"id": metric}
			}
			if parent := depParentOf[id]; parent != 0 {
				m["parent"] = map[string]any{"id": parent}
			}
			if q.Get("metadata") == "true" {
				m["metadata"] = depMetadata[id]
			}
			out = append(out, m)
		}
		writeJSON(w, 200, map[string]any{"measures": out})
	}
	f.handlers["/api/v1/model/events"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"events": []map[string]any{{"id": 7, "name": "Shift"}}})
	}
	// Latest values: 10 has two periods, the later with a value; 11 has none;
	// 12 was entered by hand (users=true sends who) before Flow filled it in.
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		var user map[string]any
		if r.URL.Query().Get("users") == "true" {
			user = map[string]any{"username": "charl", "name": "Charl"}
		}
		writeJSON(w, 200, map[string]any{"values": []map[string]any{
			// 12: entered at 00:00, then filled in by Flow (no user) at 01:00.
			{"id": 12, "data": []map[string]any{
				{"start": "2026-10-06T00:00:00Z", "value": 8, "user": user},
				{"start": "2026-10-06T01:00:00Z", "value": 8},
			}},
			{"id": 10, "data": []map[string]any{
				{"start": "2026-10-06T01:00:00Z", "value": 81.5},
				{"start": "2026-10-06T00:00:00Z", "value": 70},
				{"start": "2026-10-06T02:00:00Z"},
			}},
			{"id": 11, "data": []map[string]any{{"start": "2026-10-06T01:00:00Z"}}},
		}})
	}
	f.handlers["/api/v1/model/metrics/5/measures"] = func(w http.ResponseWriter, r *http.Request) {
		var out []map[string]any
		for id, name := range depMetric5Names {
			m := map[string]any{"id": id, "name": name, "metric": map[string]any{"id": 5}}
			if p := depParentOf[id]; p != 0 {
				m["parent"] = map[string]any{"id": p}
			}
			out = append(out, m)
		}
		writeJSON(w, 200, map[string]any{"measures": out})
	}
	// One metric and no folders.
	f.handlers["/api/v1/model/metrics"] = func(w http.ResponseWriter, r *http.Request) {
		metrics := []any{}
		for _, id := range r.URL.Query()["id"] {
			if id == "5" {
				metrics = append(metrics, map[string]any{"id": 5, "name": "Line"})
			}
		}
		writeJSON(w, 200, map[string]any{"metrics": metrics})
	}
	return f, testDatasource(srv)
}

func runDeps(t *testing.T, d *Datasource, qm queryModel) data.Frames {
	t.Helper()
	qm.QueryType = queryTypeDeps
	b, _ := json.Marshal(qm)
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b, TimeRange: backend.TimeRange{
		From: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 6, 3, 0, 0, 0, time.UTC),
	}})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	return resp.Frames
}

func column(t *testing.T, f *data.Frame, name string) []string {
	t.Helper()
	field := fieldByName(t, f, name)
	out := make([]string, field.Len())
	for i := range out {
		v, ok := field.ConcreteAt(i)
		if ok {
			switch x := v.(type) {
			case string:
				out[i] = x
			case int64:
				out[i] = strconv.FormatInt(x, 10)
			case bool:
				out[i] = strconv.FormatBool(x)
			}
		}
	}
	return out
}

// nodeCol maps each node's id to its value in a nodes-frame column.
func nodeCol(t *testing.T, frames data.Frames, name string) map[string]string {
	t.Helper()
	out := map[string]string{}
	ids := column(t, frames[0], "id")
	for i, v := range column(t, frames[0], name) {
		out[ids[i]] = v
	}
	return out
}

// edgeList is the edges as "source>target", in order.
func edgeList(t *testing.T, frames data.Frames) string {
	t.Helper()
	return strings.Join(column(t, frames[1], "id"), ",")
}

// edgeCol maps each edge's id to its value in an edges-frame column.
func edgeCol(t *testing.T, frames data.Frames, name string) map[string]string {
	t.Helper()
	out := map[string]string{}
	ids := column(t, frames[1], "id")
	for i, v := range column(t, frames[1], name) {
		out[ids[i]] = v
	}
	return out
}

const (
	m10, m11, m12, m13 = "model:measure:10", "model:measure:11", "model:measure:12", "model:measure:13"
)

func TestDependenciesInputs(t *testing.T) {
	_, d := depServer(t)
	frames := runDeps(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 10}}})

	// 13 feeds both 11 and 12 but is one node.
	if got := strings.Join(column(t, frames[0], "id"), ","); got != m10+","+m11+","+m13+","+m12 {
		t.Fatalf("nodes = %s", got)
	}
	want := m11 + ">" + m10 + "," + m13 + ">" + m11 + "," + m12 + ">" + m10 + "," + m13 + ">" + m12
	if got := edgeList(t, frames); got != want {
		t.Fatalf("edges = %s, want %s", got, want)
	}
	relations := edgeCol(t, frames, "mainstat")
	if relations[m11+">"+m10] != "calculation" || relations[m13+">"+m12] != "set to measure" {
		t.Errorf("relations = %v", relations)
	}
	details := edgeCol(t, frames, "detail__formula")
	if got := details[m11+">"+m10]; got != "Good / Planned * 100" {
		t.Errorf("formula = %q, want the latest one", got)
	}
	if got := details[m13+">"+m11]; got != "sum, hourly" {
		t.Errorf("aggregation detail = %q", got)
	}
	if got := nodeCol(t, frames, "title"); got[m10] != "Line.OEE" || got[m13] != "Line.Count" {
		t.Errorf("titles = %v", got)
	}
	// Each measure carries its latest value in the range, where it has one.
	if got := nodeCol(t, frames, "mainstat"); got[m10] != "81.50" || got[m11] != "" {
		t.Errorf("mainstat = %v, want 10's latest value and none for 11", got)
	}
}

func TestDependenciesUsedBy(t *testing.T) {
	_, d := depServer(t)
	frames := runDeps(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 13}}, DepDirection: depUsedBy})
	// Edges still point the way values flow: from 13 to what is built from it.
	want := m13 + ">" + m11 + "," + m11 + ">" + m10 + "," + m10 + ">model:measure:19,model:measure:19>model:measure:20," + m13 + ">" + m12 + "," + m12 + ">" + m10 + "," + m13 + ">model:measure:18"
	if got := edgeList(t, frames); got != want {
		t.Fatalf("edges = %s, want %s", got, want)
	}
	// Measure 14's metadata failed; the rest of the model still loaded.
	notices := frames[0].Meta.Notices
	if len(notices) != 1 || !strings.Contains(notices[0].Text, "measure 14") {
		t.Errorf("notices = %+v", notices)
	}
}

func TestDependenciesEventsCyclesAndDepth(t *testing.T) {
	_, d := depServer(t)
	frames := runDeps(t, d, queryModel{Items: []queryItem{
		{Tree: "model", Type: itemMeasure, ID: 15},
		{Tree: "model", Type: itemMeasure, ID: 16},
	}})
	// The cycle is two edges between 16 and 17, not an endless chain.
	if got := edgeList(t, frames); got != "model:event:7>model:measure:15,model:measure:17>model:measure:16,model:measure:16>model:measure:17" {
		t.Fatalf("edges = %s", got)
	}
	if got := nodeCol(t, frames, "title")["model:event:7"]; got != "Event: Shift" {
		t.Errorf("event title = %q", got)
	}
	if got := nodeCol(t, frames, "subtitle")["model:event:7"]; got != "event" {
		t.Errorf("event subtitle = %q", got)
	}

	frames = runDeps(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 10}}, DepDepth: 1})
	if got := strings.Join(column(t, frames[0], "id"), ","); got != m10+","+m11+","+m12 {
		t.Errorf("depth 1 nodes = %s", got)
	}
	// The depth limit is noted on the nodes it cut short.
	notes := nodeCol(t, frames, "detail__note")
	if !strings.Contains(notes[m11], "more inputs") || notes[m10] != "" {
		t.Errorf("notes = %v", notes)
	}
}

func TestDependenciesNodeGraph(t *testing.T) {
	_, d := depServer(t)
	frames := runDeps(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 10}}})
	if len(frames) != 2 || frames[0].Name != "nodes" || frames[1].Name != "edges" {
		t.Fatalf("frames = %v", frames)
	}
	if n := frames[0].Rows(); n != 4 {
		t.Errorf("nodes = %d, want 4 (13 once)", n)
	}
	src := column(t, frames[1], "source")
	dst := column(t, frames[1], "target")
	// Values flow from input to the measure built from it.
	if src[0] != "model:measure:11" || dst[0] != "model:measure:10" {
		t.Errorf("first edge = %s -> %s", src[0], dst[0])
	}
	// The latest value is the node's main stat; how it is built, its subtitle.
	if got := column(t, frames[0], "mainstat"); got[0] != "81.50" {
		t.Errorf("mainstat = %v", got)
	}
	if got := column(t, frames[0], "subtitle"); got[0] != "hourly · calculated" {
		t.Errorf("subtitle = %v", got)
	}
	if f, _ := frames[0].FieldByName("secondarystat"); f != nil {
		t.Error("secondarystat should be gone: the subtitle carries it")
	}
	// The rest is in the node's click menu.
	if got := nodeCol(t, frames, "detail__calculation")["model:measure:10"]; got != "Good / Planned * 100" {
		t.Errorf("calculation = %q", got)
	}
	if got := nodeCol(t, frames, "detail__aggregation")["model:measure:11"]; got != "sum, hourly" {
		t.Errorf("aggregation = %q", got)
	}
	if got := nodeCol(t, frames, "detail__enteredBy")["model:measure:12"]; got != "no user on the latest value; last entered by Charl for 2026-10-06 00:00 UTC" {
		t.Errorf("entered by = %q", got)
	}
	if got := nodeCol(t, frames, "detail__source")["model:measure:13"]; got != "Sim · FQ.PV · Average" {
		t.Errorf("source = %q", got)
	}
	if f, _ := frames[0].FieldByName("detail__calculation"); f.Config == nil || f.Config.DisplayName != "Calculation" {
		t.Errorf("calculation label = %+v", f.Config)
	}
	if frames[0].Meta.PreferredVisualization != data.VisTypeNodeGraph {
		t.Errorf("meta = %+v", frames[0].Meta)
	}
}

// A measure retrieved from a Flow tag reads that tag's measure: the tag is
// its input (TestDependenciesUsedBy covers the other way), and the source
// names it.
func TestDependenciesRetrievedFromTag(t *testing.T) {
	_, d := depServer(t)
	frames := runDeps(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 18}}})
	m18 := "model:measure:18"
	if got := edgeList(t, frames); got != m13+">"+m18 {
		t.Fatalf("edges = %s", got)
	}
	if got := edgeCol(t, frames, "mainstat")[m13+">"+m18]; got != "retrieval" {
		t.Errorf("relation = %q", got)
	}
	sources := nodeCol(t, frames, "detail__source")
	if sources[m18] != "Flow tag Line.Count · average" || sources[m13] != "Sim · FQ.PV · Average" {
		t.Errorf("sources = %q", sources)
	}
}

func TestDepValueEnteredBy(t *testing.T) {
	at := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		v    depValue
		want string
	}{
		{depValue{User: "Charl"}, "Charl"},
		{depValue{LastEntry: &depEntry{User: "Charl", Time: at}}, "no user on the latest value; last entered by Charl for 2026-10-06 00:00 UTC"},
		{depValue{}, "no user on any value in the time range"},
	} {
		if got := c.v.enteredBy(); got != c.want {
			t.Errorf("enteredBy = %q, want %q", got, c.want)
		}
	}
}

func TestDependenciesOwnNames(t *testing.T) {
	_, d := depServer(t)
	// By default the whole name, from the metric's list: 20 by id would
	// skip its grandparent.
	frames := runDeps(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 20}}})
	if got := nodeCol(t, frames, "title")["model:measure:20"]; got != "Line.OEE.Target.WTD" {
		t.Errorf("full name by default = %q", got)
	}

	frames = runDeps(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 20}, {Tree: "model", Type: itemMeasure, ID: 18}}, DepNames: depNamesMeasure})
	titles := nodeCol(t, frames, "title")
	for id, want := range map[string]string{
		"model:measure:20": "WTD",       // two levels down
		"model:measure:19": "Target",    // nested: after its parent's full name
		"model:measure:10": "OEE",       // top level: after its metric's name
		"model:measure:18": "FQ-001.PV", // its own dot is kept
		m11:                "Line.Good", // no metric known: the full name stands
	} {
		if titles[id] != want {
			t.Errorf("title of %s = %q, want %q", id, titles[id], want)
		}
	}
	// The click menu still has the id, whatever the title.
	if got := nodeCol(t, frames, "detail__measureId")["model:measure:19"]; got != "19" {
		t.Errorf("measure id = %q", got)
	}
}
