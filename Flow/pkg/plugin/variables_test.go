package plugin

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func field(name, value string) map[string]any { return map[string]any{"name": name, "value": value} }

// attributeServer is the query test model (template 9000 "Good" with
// instances 200 on Line 1 and 300 on Line 2) with model attributes, as
// model/measures?fields=true answers.
func attributeServer(t *testing.T) (*fakeFlow, *Datasource) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/model/measures"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		all := map[int]map[string]any{}
		for id, m := range modelMeasures {
			c := map[string]any{}
			for k, v := range m {
				c[k] = v
			}
			all[id] = c
		}
		all[200]["fields"] = []any{field("Line", "Line 1"), field("Area", "Packing")}
		all[201]["fields"] = []any{field("Line", "Line 1")}
		all[300]["fields"] = []any{field("Line", "Line 2"), field("Area", "Packing")}
		all[301]["fields"] = []any{field("line", "Line 2")} // names match without case
		var out []any
		if ids := q["id"]; len(ids) > 0 {
			for _, m := range byIDs(all, r) {
				out = append(out, m)
			}
		} else {
			if q.Get("fields") != "true" {
				t.Errorf("all measures without fields: %s", r.URL.RawQuery)
			}
			for _, m := range all {
				out = append(out, m)
			}
		}
		writeJSON(w, 200, map[string]any{"measures": out})
	}
	return f, testDatasource(srv)
}

func runVar(t *testing.T, d *Datasource, qm queryModel) []string {
	t.Helper()
	qm.QueryType = queryTypeVariable
	b, _ := json.Marshal(qm)
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	f := resp.Frames[0]
	var out []string
	for i := 0; i < f.Rows(); i++ {
		out = append(out, f.Fields[0].At(i).(string)+"="+f.Fields[1].At(i).(string))
	}
	return out
}

func TestVariableCharts(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/information/folders"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"folders": []any{
			map[string]any{"id": 1, "name": "Reports"},
			map[string]any{"id": 2, "name": "Daily", "parent": map[string]any{"id": 1}},
		}})
	}
	f.handlers["/api/v1/information/folders/1/charts"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"charts": []any{map[string]any{"id": 6, "name": "Trend", "type": "Line Chart"}}})
	}
	f.handlers["/api/v1/information/folders/2/charts"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"charts": []any{map[string]any{"id": 5, "name": "OEE table", "type": "Time Based Table"}}})
	}
	// Only time-based charts, which a chart query can show, by folder path.
	if got := strings.Join(runVar(t, testDatasource(srv), queryModel{VariableKind: varCharts}), ","); got != "Reports/Daily/OEE table=5" {
		t.Errorf("charts = %s", got)
	}
}

func TestVariableAttributeNames(t *testing.T) {
	_, d := attributeServer(t)
	if got := strings.Join(runVar(t, d, queryModel{VariableKind: varAttributeNames}), ","); got != "Area=Area,Line=Line" {
		t.Errorf("names = %s", got)
	}
}

func TestVariableAttributeValues(t *testing.T) {
	_, d := attributeServer(t)
	if got := strings.Join(runVar(t, d, queryModel{VariableKind: varAttributeValues, VariableAttribute: "Line"}), ","); got != "Line 1=Line 1,Line 2=Line 2" {
		t.Errorf("values = %s", got)
	}
	// Narrowed by another attribute: chained variables.
	got := runVar(t, d, queryModel{VariableKind: varAttributeValues, VariableAttribute: "Area", AttributeFilter: "Line", AttributeValues: []string{"Line 2"}})
	if strings.Join(got, ",") != "Packing=Packing" {
		t.Errorf("chained = %v", got)
	}
}

func TestVariableMeasuresOfTemplate(t *testing.T) {
	_, d := attributeServer(t)
	// The instances of template measure 9000, by path, valued by id.
	got := runVar(t, d, queryModel{VariableKind: varMeasures, Items: []queryItem{{Tree: "template", Type: itemMeasure, ID: 9000}}})
	if strings.Join(got, ",") != "Site/Line 1/Production/Good=200,Site/Line 2/Production/Good=300" {
		t.Errorf("instances = %v", got)
	}
	got = runVar(t, d, queryModel{VariableKind: varMeasures, Items: []queryItem{{Tree: "template", Type: itemMeasure, ID: 9000}}, AttributeFilter: "Line", AttributeValues: []string{"Line 2"}})
	if strings.Join(got, ",") != "Site/Line 2/Production/Good=300" {
		t.Errorf("instances on Line 2 = %v", got)
	}
}

func TestVariableAllMeasures(t *testing.T) {
	_, d := attributeServer(t)
	// Nothing picked: every model measure, here narrowed to Line 1.
	got := runVar(t, d, queryModel{VariableKind: varMeasures, AttributeFilter: "Line", AttributeValues: []string{"Line 1"}})
	if strings.Join(got, ",") != "Site/Line 1/Production/Good=200,Site/Line 1/Production/Speed=201" {
		t.Errorf("measures = %v", got)
	}
}

func TestAttributeFilterOnMeasuresQuery(t *testing.T) {
	f, d := attributeServer(t)
	// Template 9000 reads instances 200 and 300 (the fake's data/measures);
	// the filter keeps Line 2's.
	resp := runQuery(t, d, queryModel{Items: []queryItem{{Tree: "template", Type: itemMeasure, ID: 9000}}, AttributeFilter: "Line", AttributeValues: []string{"Line 2"}})
	if len(resp.Frames) != 1 || resp.Frames[0].Name != "Site/Line 2/Production/Good" {
		var names []string
		for _, fr := range resp.Frames {
			names = append(names, fr.Name)
		}
		t.Errorf("frames = %v", names)
	}
	// Model measures outside the filter are not even read: measure 201 is a
	// tag on Line 1, so no data/tags call is made for Line 2.
	before := f.count("/api/v1/data/tags")
	runQuery(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 201}}, AttributeFilter: "Line", AttributeValues: []string{"Line 2"}})
	if after := f.count("/api/v1/data/tags"); after != before {
		t.Errorf("tag read %d time(s) for a measure the filter drops", after-before)
	}

	resp = runQuery(t, d, queryModel{Items: []queryItem{{Tree: "template", Type: itemMeasure, ID: 9000}}, AttributeFilter: "Line", AttributeValues: []string{"Line 9"}})
	notices := []string{}
	for _, fr := range resp.Frames {
		if fr.Meta != nil {
			for _, n := range fr.Meta.Notices {
				notices = append(notices, n.Text)
			}
		}
	}
	if !strings.Contains(strings.Join(notices, ";"), "None of the selected measures has Line = Line 9") {
		t.Errorf("notices = %v", notices)
	}
}

// limitMeta is limit metadata as Flow sends it, for calendar "Default".
func limitMeta(name, comparator string, high, target, low any) map[string]any {
	return map[string]any{"type": "Limit", "data": map[string]any{"Default": map[string]any{name: map[string]any{
		"9/8/2025 4:00:00 AM": map[string]any{"comparator": comparator, "high": high, "target": target, "low": low},
	}}}}
}

func TestMeasureLimitsAsThresholds(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/model/measures"] = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("metadata") == "true" {
			writeJSON(w, 200, map[string]any{"measures": []map[string]any{{"id": 300, "metadata": []any{
				limitMeta("Moonshot", "<", 3, 3, nil),
				limitMeta("Site", "<", 5, 4, nil),
				map[string]any{"type": "Limit", "data": map[string]any{"Other calendar": map[string]any{"Shift": map[string]any{
					"1/1/2026 12:00:00 AM": map[string]any{"comparator": "<", "high": 99},
				}}}},
				limitMeta("Spec", "=", nil, nil, 1),
			}}}})
			return
		}
		writeJSON(w, 200, map[string]any{"measures": byIDs(modelMeasures, r)})
	}
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{{"start": "2026-10-06T00:00:00Z", "value": 2}}}}})
	}
	d := testDatasource(srv)
	resp := runQuery(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 300}}, Limits: limitsLines})
	value := resp.Frames[0].Fields[1]
	th := value.Config.Thresholds
	if th == nil {
		t.Fatal("no thresholds")
	}
	var steps []string
	for _, s := range th.Steps {
		v := float64(s.Value)
		if math.IsInf(v, -1) {
			steps = append(steps, "base="+s.Color)
			continue
		}
		steps = append(steps, strconv.FormatFloat(v, 'f', -1, 64)+"="+s.Color)
	}
	// Bounds red, the Site target green, the other calendar's limit left
	// out, and no base step of our own (see limitThresholds).
	if got := strings.Join(steps, ","); got != "1=red,3=red,4=green,5=red" {
		t.Errorf("steps = %s", got)
	}
	if mode := value.Config.Custom["thresholdsStyle"].(map[string]any)["mode"]; mode != "dashed" {
		t.Errorf("style = %v", mode)
	}
	if !strings.Contains(value.Config.Description, "Limits: Moonshot < 3; Site < 5, target 4; Spec low 1") {
		t.Errorf("description = %q", value.Config.Description)
	}

	// Picking limits draws only those.
	resp = runQuery(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 300}}, Limits: limitsLines, LimitNames: []string{"site"}})
	value = resp.Frames[0].Fields[1]
	var picked []string
	for _, s := range value.Config.Thresholds.Steps {
		picked = append(picked, strconv.FormatFloat(float64(s.Value), 'f', -1, 64)+"="+s.Color)
	}
	if got := strings.Join(picked, ","); got != "4=green,5=red" || !strings.HasSuffix(value.Config.Description, "Limits: Site < 5, target 4") {
		t.Errorf("Site only: steps %s, description %q", got, value.Config.Description)
	}

	// The editor's list: the limit names in the query's calendar.
	names, err := d.limitNames(context.Background(), queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 300}}})
	if err != nil || strings.Join(names, ",") != "Moonshot,Site,Spec" {
		t.Errorf("limit names = %v, %v", names, err)
	}
	// The same names as a Limits variable.
	got := runVar(t, d, queryModel{VariableKind: varLimits, Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 300}}})
	if strings.Join(got, ",") != "Moonshot=Moonshot,Site=Site,Spec=Spec" {
		t.Errorf("limits variable = %v", got)
	}

	// Without the option, nothing is drawn.
	resp = runQuery(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 300}}})
	if resp.Frames[0].Fields[1].Config.Thresholds != nil {
		t.Error("thresholds without the limits option")
	}
}

func TestLatestLimitWins(t *testing.T) {
	p, ok := latestLimit(map[string]limitProps{
		"9/8/2025 4:00:00 AM":   {Comparator: "<", High: ptr(5)},
		"9/8/2026 4:00:00 AM":   {Comparator: "<", High: ptr(6)},
		"not a date in any way": {Comparator: "<", High: ptr(1)},
	})
	if !ok || *p.High != 6 {
		t.Errorf("latest = %+v", p)
	}
}

func ptr(v float64) *float64 { return &v }

func TestCommentsAsAnnotations(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("comments") != "true" || q.Get("users") != "true" {
			t.Errorf("data/measures without comments: %s", r.URL.RawQuery)
		}
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 200, "data": []map[string]any{
			{"start": "2026-10-06T01:00:00Z", "value": 7, "comments": []any{
				map[string]any{"comment": "Pump tripped", "datetime": "2026-10-06T01:20:00Z", "user": map[string]any{"name": "Charl"}},
				map[string]any{"comment": "  "},
			}},
			{"start": "2026-10-06T00:00:00Z", "value": 5},
		}}}})
	}
	d := testDatasource(srv)
	resp := runQuery(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 200}}, Annotate: annotateComments})
	fr := resp.Frames[0]
	if fr.Rows() != 1 {
		t.Fatalf("rows = %d, want the one non-blank comment", fr.Rows())
	}
	if got := fr.Fields[0].At(0).(time.Time).Format("15:04"); got != "01:00" {
		t.Errorf("time = %s, want the value's period start", got)
	}
	if got := column(t, fr, "text")[0]; got != "Pump tripped\n— Charl, "+timeText(time.Date(2026, 10, 6, 1, 20, 0, 0, time.UTC)) {
		t.Errorf("text = %q", got)
	}
	if got := column(t, fr, "title")[0]; got != "Site/Line 1/Production/Good" {
		t.Errorf("title = %q", got)
	}
	_ = data.Frames{}
}

// A period measure read in one calendar shows only that calendar's limits,
// even when it has limits in no other; a tag, read without a calendar,
// falls back to the only limits it has.
func TestLimitsBelongToTheirCalendar(t *testing.T) {
	m := flow.Measure{Interval: "hourly", Metadata: []flow.MeasureMetadata{{Type: "Limit", Data: map[string]json.RawMessage{
		"Production": json.RawMessage(`{"Site":{"9/8/2025 4:00:00 AM":{"comparator":"<","high":5}}}`),
	}}}}
	if got := measureLimits(m, "finance", m.IsTag()); len(got) != 0 {
		t.Errorf("finance limits = %+v, want none", got)
	}
	if got := measureLimits(m, "Production", m.IsTag()); len(got) != 1 {
		t.Errorf("Production limits = %+v", got)
	}
	m.Interval = flow.IntervalTimestamp
	if got := measureLimits(m, "finance", m.IsTag()); len(got) != 1 {
		t.Errorf("tag limits = %+v, want the only set", got)
	}
}
