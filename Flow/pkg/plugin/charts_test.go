package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// The fixtures are a real Flow chart: "Daily", a time-based table with the
// TRI measure in two sections and a "< 3" limit, over 11 days.
func chartFixtures(t *testing.T) (json.RawMessage, []byte) {
	t.Helper()
	def, err := os.ReadFile("testdata/chart5-definition.json")
	if err != nil {
		t.Fatal(err)
	}
	dat, err := os.ReadFile("testdata/chart5-data.json")
	if err != nil {
		t.Fatal(err)
	}
	return def, dat
}

func TestParseChartDefinition(t *testing.T) {
	raw, _ := chartFixtures(t)
	def, err := parseChartDefinition(raw)
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "Daily" || def.Interval != "daily" || def.Type != "tables-timebased-table" {
		t.Errorf("def = %+v", def)
	}
	if len(def.Rows) != 2 {
		t.Fatalf("rows = %+v, want TRI in Safety and in Section", def.Rows)
	}
	r := def.Rows[0]
	if r.Section != "Safety" || r.Title != "TRI TRI" || r.MeasureID != 2087 || r.UOM != "#" ||
		r.MeasureLimitID != 7 || r.LimitID != 3 || r.Comparator != "<" {
		t.Errorf("row = %+v", r)
	}
	var kinds []string
	for _, h := range def.Headers {
		kinds = append(kinds, h.Kind+"="+h.Title)
	}
	if got := strings.Join(kinds, ","); got != "name=Measure,uom=UOM,lowlimit=Low Limit,limit=Limit,highlimit=High Limit" {
		t.Errorf("headers = %s", got)
	}
	if l := def.Limits[3]; l.ForeHigh != "#B10043" || l.ForeLow != "#E47F00" {
		t.Errorf("limit colours = %+v", l)
	}
}

func chartServer(t *testing.T) (*fakeFlow, *Datasource) {
	def, dat := chartFixtures(t)
	f, srv := newFakeFlow(t)
	f.handlers["/api/definition/charts/5"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(def)
	}
	f.handlers["/api/data/charts/5"] = func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Query().Get("periodStart"), "Z") {
			t.Errorf("periodStart not UTC: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(dat)
	}
	return f, testDatasource(srv)
}

func runChartQuery(t *testing.T, d *Datasource, qm queryModel) *data.Frame {
	t.Helper()
	qm.QueryType = queryTypeChart
	qm.ChartID = 5
	b, _ := json.Marshal(qm)
	resp := d.query(context.Background(), backend.DataQuery{
		RefID: "A", JSON: b,
		TimeRange: backend.TimeRange{From: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)},
	})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if len(resp.Frames) != 1 {
		t.Fatalf("frames = %d", len(resp.Frames))
	}
	return resp.Frames[0]
}

func fieldByName(t *testing.T, f *data.Frame, name string) *data.Field {
	t.Helper()
	field, i := f.FieldByName(name)
	if i < 0 {
		var names []string
		for _, x := range f.Fields {
			names = append(names, x.Name)
		}
		t.Fatalf("no field %q in %v", name, names)
	}
	return field
}

func strAt(f *data.Field, i int) string {
	if v, ok := f.ConcreteAt(i); ok {
		return v.(string)
	}
	return ""
}

func TestChartTableLikeFlow(t *testing.T) {
	f, d := chartServer(t)
	frame := runChartQuery(t, d, queryModel{})

	if frame.Rows() != 2 {
		t.Fatalf("rows = %d, want one per measure row", frame.Rows())
	}
	if got := strAt(fieldByName(t, frame, "Section"), 0); got != "Safety" {
		t.Errorf("section = %q", got)
	}
	if got := strAt(fieldByName(t, frame, "Limit"), 0); got != "< 3,00" {
		t.Errorf("limit = %q, want comparator and Flow's formatted target", got)
	}
	if got := strAt(fieldByName(t, frame, "High Limit"), 0); got != "3,00" {
		t.Errorf("high limit = %q", got)
	}

	// Columns are Flow's own period labels (in the server's locale), oldest first.
	first := fieldByName(t, frame, "Tue 08 Sept")
	if got := strAt(first, 0); got != "4,00" {
		t.Errorf("Tue 08 Sept = %q, want 4,00", got)
	}
	styleName, _ := first.Config.Custom["styleField"].(string)
	style := fieldByName(t, frame, styleName)
	var css map[string]string
	if err := json.Unmarshal([]byte(strAt(style, 0)), &css); err != nil || css["color"] != "#B10043" {
		t.Errorf("style of a high value = %q, want Flow's high colour", strAt(style, 0))
	}
	if hidden, _ := style.Config.Custom["hidden"].(bool); !hidden {
		t.Error("style column is visible")
	}
	// Where Grafana ignores the hide flag (a panel fresh from Suggestions),
	// helper columns show no text: their style comes from __invisible.
	if got, _ := style.Config.Custom["styleField"].(string); got != invisibleField {
		t.Errorf("style column styleField = %q, want %q", got, invisibleField)
	}
	if got := strAt(fieldByName(t, frame, invisibleField), 1); got != `{"color":"transparent"}` {
		t.Errorf("invisible style = %q", got)
	}

	// A value within the limit is green: the limit sets no target colour.
	within := -1
	for i, fld := range frame.Fields {
		if strAt(fld, 0) == "1,00" {
			within = i
		}
	}
	if within < 0 {
		t.Fatal("no 1,00 cell")
	}
	if got := strAt(frame.Fields[within+1], 0); !strings.Contains(got, withinLimitColor) {
		t.Errorf("style within limit = %q, want green", got)
	}

	// The definition is read once, however often the panel refreshes.
	runChartQuery(t, d, queryModel{})
	if n := f.count("/api/definition/charts/5"); n != 1 {
		t.Errorf("definition reads = %d, want 1 (each read records chart usage in Flow)", n)
	}
}

func TestChartAlwaysUsesTheDashboardRange(t *testing.T) {
	f, d := chartServer(t)
	_, dat := chartFixtures(t)
	var query string
	f.handlers["/api/data/charts/5"] = func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(dat)
	}
	runChartQuery(t, d, queryModel{})
	if !strings.Contains(query, "periodStart=2026-09-08T00%3A00%3A00.000Z") || !strings.Contains(query, "periodEnd=2026-09-20T00%3A00%3A00.000Z") {
		t.Errorf("query = %q, want the panel's range in UTC", query)
	}
}

func TestEventChartIsRefused(t *testing.T) {
	f, d := chartServer(t)
	f.handlers["/api/definition/charts/5"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"name": "Downtime", "chartType": "tables-eventbased-table"})
	}
	b, _ := json.Marshal(queryModel{QueryType: queryTypeChart, ChartID: 5})
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b})
	if resp.Error == nil || !strings.Contains(resp.Error.Error(), "only time-based charts") {
		t.Errorf("error = %v", resp.Error)
	}
}

func TestPeriodLabelsDisambiguateRepeats(t *testing.T) {
	p := func(label string, day int) flow.ChartPeriod {
		return flow.ChartPeriod{StartLabel: label, Year: 2026, Month: 10, Day: day}
	}
	got := periodLabels([]flow.ChartPeriod{p("06:00", 6), p("07:00", 6), p("06:00", 7)})
	want := []string{"2026-10-06 06:00", "07:00", "2026-10-07 06:00"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

func TestSummaryCountsKinds(t *testing.T) {
	f, srv := newFakeFlow(t)
	_ = f
	d := testDatasource(srv)
	s, err := d.summary(context.Background(), []string{"template:metric:900", "model:measure:200"})
	if err != nil {
		t.Fatal(err)
	}
	if s.PeriodMeasures != 2 || s.Tags != 1 {
		t.Errorf("summary = %+v, want 2 period measures (9000, 200) and 1 tag (9001)", s)
	}
	if _, err := d.summary(context.Background(), []string{"bogus"}); err == nil {
		t.Error("bogus key accepted")
	}
}

// The summary tells the editor which calendars and events the selection's
// period measures can be read in, from their context metadata.
func TestSummaryContext(t *testing.T) {
	f, srv := newFakeFlow(t)
	context200 := map[string]any{"type": "context", "data": map[string]any{"calendars": []int{1, 2}, "events": []int{7}}}
	context9000 := map[string]any{"type": "context", "data": map[string]any{"calendars": []int{1}}}
	withMeta := func(all map[int]map[string]any, meta map[int]map[string]any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var out []map[string]any
			for _, m := range byIDs(all, r) {
				c := map[string]any{}
				for k, v := range m {
					c[k] = v
				}
				if r.URL.Query().Get("metadata") == "true" {
					if md, ok := meta[int(c["id"].(int))]; ok {
						c["metadata"] = []any{md}
					}
				}
				out = append(out, c)
			}
			writeJSON(w, 200, map[string]any{"measures": out})
		}
	}
	f.handlers["/api/v1/model/measures"] = withMeta(modelMeasures, map[int]map[string]any{200: context200})
	f.handlers["/api/v1/template/measures"] = withMeta(templateMeasures, map[int]map[string]any{9000: context9000})
	d := testDatasource(srv)

	s, err := d.summary(context.Background(), []string{"template:measure:9000", "model:measure:200"})
	if err != nil {
		t.Fatal(err)
	}
	c := s.Context
	if c == nil || c.Measures != 2 || fmt.Sprint(c.Calendars) != "[{1 2} {2 1}]" || c.EventMeasures != 1 {
		t.Errorf("context = %+v", c)
	}

	// A measure without context metadata: unknown, so the editor offers all.
	s, err = d.summary(context.Background(), []string{"model:measure:200", "model:measure:300"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Context != nil {
		t.Errorf("context = %+v, want unknown", s.Context)
	}
}

func TestChartCommentsBecomeCellTooltips(t *testing.T) {
	_, d := chartServer(t)
	frame := runChartQuery(t, d, queryModel{})

	commented := fieldByName(t, frame, "Wed 09 Sept")
	tooltip, _ := commented.Config.Custom["tooltip"].(map[string]any)
	name, _ := tooltip["field"].(string)
	if name == "" {
		t.Fatalf("no tooltip on the commented column: %+v", commented.Config.Custom)
	}
	comments := fieldByName(t, frame, name)
	if got := strAt(comments, 0); got != "what is this\n— Charl\\flow, 2026-10-07 10:05 UTC" {
		t.Errorf("comment = %q", got)
	}
	if hidden, _ := comments.Config.Custom["hidden"].(bool); !hidden {
		t.Error("comments column is visible")
	}

	// A column without comments gets no tooltip, so no marker in its cells.
	plain := fieldByName(t, frame, "Thu 10 Sept")
	if _, ok := plain.Config.Custom["tooltip"]; ok {
		t.Error("tooltip on a column without comments")
	}
}

// Grafana's table keys row values by display name, so helper columns with
// the same display name overwrite each other's values.
func TestChartFieldDisplayNamesAreUnique(t *testing.T) {
	_, d := chartServer(t)
	frame := runChartQuery(t, d, queryModel{})
	seen := map[string]string{}
	for _, f := range frame.Fields {
		name := f.Name
		if f.Config != nil && f.Config.DisplayNameFromDS != "" {
			name = f.Config.DisplayNameFromDS
		}
		if other, dup := seen[name]; dup {
			t.Errorf("fields %q and %q share display name %q", other, f.Name, name)
		}
		seen[name] = f.Name
	}
}

// withModelAttributes is the chart 5 definition with model attribute
// columns added next to its UOM column, as the Flow chart editor adds them:
// "Line" (attribute 5) twice under the same title, one whose attribute was
// deleted (no id), and the TRI measure valued for attribute 5 only where
// set is true.
func withModelAttributes(t *testing.T, set bool) json.RawMessage {
	t.Helper()
	raw, _ := chartFixtures(t)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	header := func(title string, id any) map[string]any {
		return map[string]any{"componentType": "tables-timebased-modelattributeheader", "title": title, "modelAttributeID": id}
	}
	added := false
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if comps, ok := v["components"].([]any); ok && !added {
				for _, c := range comps {
					if cm, ok := c.(map[string]any); ok && cm["componentType"] == "tables-timebased-uomheader" {
						v["components"] = append(comps, header("Line", 5), header("Area", 4), header("", nil))
						added = true
						break
					}
				}
			}
			if m, ok := v["measure"].(map[string]any); ok {
				if set {
					m["modelAttributes"] = map[string]any{"4": "", "5": "Line 1"}
				} else {
					delete(m, "modelAttributes")
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(root)
	if !added {
		t.Fatal("no UOM header to add model attribute columns next to")
	}
	b, _ := json.Marshal(root)
	return b
}

func TestChartModelAttributeColumns(t *testing.T) {
	def, err := parseChartDefinition(withModelAttributes(t, true))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, h := range def.Headers {
		kinds = append(kinds, fmt.Sprintf("%s=%s:%d", h.Kind, h.Title, h.AttributeID))
	}
	// After UOM, before the limits; the column without an attribute is left out.
	want := "name=Measure:0,uom=UOM:0,attribute=Line:5,attribute=Area:4,lowlimit=Low Limit:0,limit=Limit:0,highlimit=High Limit:0"
	if got := strings.Join(kinds, ","); got != want {
		t.Errorf("headers\n got %s\nwant %s", got, want)
	}
	j := &joined{}
	frame := j.tableFrame(def)
	line := fieldByName(t, frame, "Line")
	if v, ok := line.ConcreteAt(0); !ok || v.(string) != "Line 1" {
		t.Errorf("Line = %v", v)
	}
	// Attribute 4 is empty on the measure: a blank cell, not an error.
	if _, ok := fieldByName(t, frame, "Area").ConcreteAt(0); ok {
		t.Error("Area has a value")
	}

	// A measure without modelAttributes at all: blank cells too.
	def, err = parseChartDefinition(withModelAttributes(t, false))
	if err != nil {
		t.Fatal(err)
	}
	frame = (&joined{}).tableFrame(def)
	if _, ok := fieldByName(t, frame, "Line").ConcreteAt(0); ok {
		t.Error("Line has a value without modelAttributes")
	}
}

func TestChartColumnNamesStayUnique(t *testing.T) {
	frame := data.NewFrame("", data.NewField("Line", nil, []string{}))
	if got := uniqueName(frame, "Line"); got != "Line (2)" {
		t.Errorf("uniqueName = %q", got)
	}
	if got := uniqueName(frame, "Area"); got != "Area" {
		t.Errorf("uniqueName = %q", got)
	}
}
