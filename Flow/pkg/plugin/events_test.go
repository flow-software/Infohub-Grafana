package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func attr(name, value, color string) map[string]any {
	return map[string]any{"name": name, "value": value, "color": color}
}

// eventServer serves event 2 "Run" in folder Line 1 with three periods: two
// back to back (00:00-01:00, 01:00-02:00) and one still running from 03:00.
func eventServer(t *testing.T) (*fakeFlow, *Datasource) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/model/events"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"events": []map[string]any{{
			"id": 2, "name": "Run", "folder": map[string]any{"id": 2},
			"attributes": []map[string]any{{"id": 1, "name": "Product"}, {"id": 2, "name": "Crew"}},
		}}})
	}
	f.handlers["/api/v1/data/events"] = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("attributes") != "true" {
			t.Errorf("data/events without attributes: %s", r.URL.RawQuery)
		}
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 2, "name": "Run", "data": []map[string]any{
			{"start": "2026-10-06T03:00:00Z", "attributes": []any{attr("Product", "Juice", "#F28E2B")}},
			{"start": "2026-10-06T00:00:00Z", "end": "2026-10-06T01:00:00Z", "attributes": []any{attr("Product", "Coke", "#A0CBE8"), attr("Crew", "A", "")}},
			{"start": "2026-10-06T01:00:00Z", "end": "2026-10-06T02:00:00Z", "attributes": []any{attr("Product", "Juice", "#F28E2B"), attr("Crew", "B", "")}},
		}}}})
	}
	return f, testDatasource(srv)
}

func runEventQuery(t *testing.T, d *Datasource, qm queryModel) data.Frames {
	t.Helper()
	qm.QueryType = queryTypeEvents
	b, _ := json.Marshal(qm)
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b, TimeRange: backend.TimeRange{
		From: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC),
	}})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	return resp.Frames
}

var runEvent = []queryItem{{Tree: "model", Type: itemEvent, ID: 2}}

func TestEventsTable(t *testing.T) {
	_, d := eventServer(t)
	frames := runEventQuery(t, d, queryModel{Items: runEvent})
	f := frames[0]
	if got := column(t, f, "Event"); got[0] != "Site/Line 1/Run" {
		t.Errorf("event = %v", got)
	}
	// Newest first; the running period has no end, and a duration to now.
	starts := fieldByName(t, f, "Start")
	if s, _ := starts.ConcreteAt(0); !s.(time.Time).Equal(time.Date(2026, 10, 6, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("first start = %v", s)
	}
	if _, ok := fieldByName(t, f, "End").ConcreteAt(0); ok {
		t.Error("running period has an end")
	}
	if d, ok := fieldByName(t, f, "Duration").ConcreteAt(2); !ok || d.(float64) != 3600 {
		t.Errorf("duration of 00:00-01:00 = %v", d)
	}
	// A column per attribute, in Flow's order, coloured as in Flow.
	if got := column(t, f, "Product"); strings.Join(got, ",") != "Juice,Juice,Coke" {
		t.Errorf("product = %v", got)
	}
	if got := column(t, f, "Crew"); strings.Join(got, ",") != ",B,A" {
		t.Errorf("crew = %v", got)
	}
	product := fieldByName(t, f, "Product")
	if m := product.Config.Mappings[0].(data.ValueMapper); m["Coke"].Color != "#A0CBE8" {
		t.Errorf("mappings = %+v", m)
	}
	// Crew has no colours, so no coloured cells.
	if crew := fieldByName(t, f, "Crew"); crew.Config.Mappings != nil || crew.Config.Custom != nil {
		t.Errorf("crew config = %+v", crew.Config)
	}
}

func TestEventsTimeline(t *testing.T) {
	_, d := eventServer(t)
	frames := runEventQuery(t, d, queryModel{Items: runEvent, EventFormat: eventFormatTimeline})
	f := frames[0]
	times := fieldByName(t, f, "Time")
	var got []string
	for i := 0; i < times.Len(); i++ {
		got = append(got, times.At(i).(time.Time).Format("15:04"))
	}
	// 00:00-01:00 runs straight into 01:00-02:00, so no gap at 01:00; the
	// band closes at 02:00, and the running period stays open.
	if strings.Join(got, ",") != "00:00,01:00,02:00,03:00" {
		t.Errorf("times = %v", got)
	}
	if got := column(t, f, "Product"); strings.Join(got, ",") != "Coke,Juice,,Juice" {
		t.Errorf("product = %v", got)
	}
	// One event: fields are named by attribute alone.
	if got := fieldByName(t, f, "Product").Config.DisplayNameFromDS; got != "Product" {
		t.Errorf("display name = %q", got)
	}
}

func TestEventsAnnotations(t *testing.T) {
	_, d := eventServer(t)
	frames := runEventQuery(t, d, queryModel{Items: runEvent, EventFormat: eventFormatAnnotations})
	f := frames[0]
	if f.Rows() != 3 {
		t.Fatalf("rows = %d", f.Rows())
	}
	if got := column(t, f, "text")[0]; got != "Product: Coke\nCrew: A" {
		t.Errorf("text = %q", got)
	}
	if got := column(t, f, "tags")[0]; got != "Coke,A" {
		t.Errorf("tags = %q", got)
	}
	if got := column(t, f, "color")[0]; got != "#A0CBE8" {
		t.Errorf("color = %q", got)
	}
	if got := column(t, f, "title")[0]; got != "Site/Line 1/Run" {
		t.Errorf("title = %q", got)
	}
}

func TestEventsQueryValidation(t *testing.T) {
	for _, qm := range []queryModel{
		{QueryType: queryTypeEvents, Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 1}}},
		{QueryType: queryTypeEvents, EventFormat: "pie", Items: runEvent},
		{Context: "shift", Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 1}}},
		{Items: runEvent}, // a measures query does not take events
	} {
		if qm.validate() == nil {
			t.Errorf("%+v validated", qm)
		}
	}
}

// Measures read per event period: kept to some values of an attribute and
// split into a series per value.
func TestMeasuresInEventContext(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("eventPeriods") != "true" || q.Get("eventid") != "2" || q.Get("calendarid") != "" || q.Get("id") != "200" {
			t.Errorf("data/measures query %s", r.URL.RawQuery)
		}
		writeJSON(w, 200, map[string]any{"errors": []string{}, "values": []map[string]any{{"id": 200, "data": []map[string]any{
			{"start": "2026-10-06T00:00:00Z", "end": "2026-10-06T01:00:00Z", "value": 5, "attributes": []any{attr("Product", "Coke", "#A0CBE8")}},
			{"start": "2026-10-06T01:00:00Z", "end": "2026-10-06T02:00:00Z", "value": 7, "attributes": []any{attr("Product", "Juice", "")}},
			{"start": "2026-10-06T02:00:00Z", "end": "2026-10-06T03:00:00Z", "value": 9, "attributes": []any{attr("Product", "Coke", "")}},
			{"start": "2026-10-06T03:00:00Z", "end": "2026-10-06T04:00:00Z", "value": 1},
		}}}})
	}
	d := testDatasource(srv)
	good := queryItem{Tree: "model", Type: itemMeasure, ID: 200}

	// Split by Product: a series per value, periods without one as "(none)".
	resp := runQuery(t, d, queryModel{Items: []queryItem{good}, Context: contextEvent, EventID: 2, EventAttribute: "Product", EventSplit: true, TimeAt: "end"})
	var names []string
	for _, fr := range resp.Frames {
		names = append(names, fr.Fields[1].Config.DisplayNameFromDS+"="+strings.Join(floatColumn(fr), ","))
	}
	want := "Site/Line 1/Production/Good · (none)=1,Site/Line 1/Production/Good · Coke=5,9,Site/Line 1/Production/Good · Juice=7"
	if strings.Join(names, ";") != strings.ReplaceAll(want, ",Site", ";Site") {
		t.Errorf("series = %v", names)
	}
	if lbl := resp.Frames[1].Fields[1].Labels["Product"]; lbl != "Coke" {
		t.Errorf("labels = %v", resp.Frames[1].Fields[1].Labels)
	}
	// Each value's series has its colour in Flow; values without one keep Grafana's.
	if c := resp.Frames[1].Fields[1].Config.Color; c == nil || c["fixedColor"] != "#A0CBE8" {
		t.Errorf("coke colour = %v", c)
	}
	if c := resp.Frames[2].Fields[1].Config.Color; c != nil {
		t.Errorf("juice colour = %v", c)
	}
	// Time at end places each value at its period's end.
	if ts := resp.Frames[1].Fields[0].At(0).(time.Time); ts.Hour() != 1 {
		t.Errorf("time at end = %v", ts)
	}

	// Filtered to Coke, not split: one series of the Coke periods.
	resp = runQuery(t, d, queryModel{Items: []queryItem{good}, Context: contextEvent, EventID: 2, EventAttribute: "Product", EventValues: []string{"Coke"}})
	if len(resp.Frames) != 1 || strings.Join(floatColumn(resp.Frames[0]), ",") != "5,9" {
		t.Errorf("filtered = %v", resp.Frames)
	}

	// Template measures cannot be read per event: they are left out, saying so.
	resp = runQuery(t, d, queryModel{Items: []queryItem{good, {Tree: "template", Type: itemMeasure, ID: 9000}}, Context: contextEvent, EventID: 2})
	if n := resp.Frames[0].Meta; n == nil || len(n.Notices) == 0 || !strings.Contains(n.Notices[0].Text, "template measure") {
		t.Errorf("notices = %+v", n)
	}
}

func floatColumn(f *data.Frame) []string {
	var out []string
	for i := 0; i < f.Fields[1].Len(); i++ {
		if v, ok := f.Fields[1].ConcreteAt(i); ok {
			out = append(out, strconv.FormatFloat(v.(float64), 'f', -1, 64))
		}
	}
	return out
}

// Whole period draws each value from its period's start to its end, with a
// gap before the next period; a running period runs to the range end.
func TestMeasuresInEventContextWholePeriod(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 200, "data": []map[string]any{
			{"start": "2026-10-06T00:00:00Z", "end": "2026-10-06T00:30:00Z", "value": 5},
			{"start": "2026-10-06T00:30:00Z", "end": "2026-10-06T00:50:00Z", "value": 6},
			{"start": "2026-10-06T01:00:00Z", "end": "2026-10-06T01:20:00Z", "value": 7},
			{"start": "2026-10-06T01:30:00Z", "value": 9},
		}}}})
	}
	d := testDatasource(srv)
	resp := runQuery(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 200}}, Context: contextEvent, EventID: 2, TimeAt: timeAtPeriod})
	fr := resp.Frames[0]
	var got []string
	for i := 0; i < fr.Rows(); i++ {
		ts := fr.Fields[0].At(i).(time.Time).Format("15:04:05.000")
		v := "null"
		if x, ok := fr.Fields[1].ConcreteAt(i); ok {
			v = strconv.FormatFloat(x.(float64), 'f', -1, 64)
		}
		got = append(got, ts+"="+v)
	}
	want := []string{
		"00:00:00.000=5", "00:30:00.000=5", // back to back with the next: no gap
		"00:30:00.000=6", "00:50:00.000=6", "00:50:00.001=null",
		"01:00:00.000=7", "01:20:00.000=7", "01:20:00.001=null",
		"01:30:00.000=9", "02:00:00.000=9", // running: up to the range end
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("points\n got %v\nwant %v", got, want)
	}

	// Period end: a running period has no end, so its value stays at the start.
	resp = runQuery(t, d, queryModel{Items: []queryItem{{Tree: "model", Type: itemMeasure, ID: 200}}, Context: contextEvent, EventID: 2, TimeAt: "end"})
	if ts := resp.Frames[0].Fields[0].At(3).(time.Time); ts.Format("15:04") != "01:30" {
		t.Errorf("running period at end = %v", ts)
	}
}

// The events browser leaves out folders with no event anywhere below them.
func TestBrowseEventsOnlyFoldersWithEvents(t *testing.T) {
	f, d := eventServer(t) // event 2 sits in folder 2 "Line 1", under 1 "Site"
	f.handlers["/api/v1/model/folders/1/folders"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"folders": []any{modelFolders[1], modelFolders[2]}})
	}
	f.handlers["/api/v1/model/folders/1/events"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"events": []any{}})
	}
	ctx := context.Background()
	root, err := d.browseEvents(ctx, "model", "root", 0)
	if err != nil || len(root) != 1 || root[0].ID != 1 {
		t.Fatalf("root = %+v, %v", root, err)
	}
	site, err := d.browseEvents(ctx, "model", "folder", 1)
	if err != nil || len(site) != 1 || site[0].Name != "Line 1" {
		t.Errorf("Site = %+v, %v (Line 2 has no events)", site, err)
	}
}

// Measures listed under a metric name the events they have as context.
func TestBrowseMarksEventContext(t *testing.T) {
	f, d := eventServer(t) // event 2 is "Run"
	f.handlers["/api/v1/model/metrics/20/measures"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"measures": []any{modelMeasures[200], modelMeasures[201]}})
	}
	f.handlers["/api/v1/model/measures"] = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("metadata") != "true" {
			t.Errorf("measures without metadata: %s", r.URL.RawQuery)
		}
		writeJSON(w, 200, map[string]any{"measures": []map[string]any{
			{"id": 200, "metadata": []map[string]any{{"type": "context", "data": map[string]any{"calendars": []int{1}, "events": []int{2}}}}},
			{"id": 201, "metadata": []map[string]any{{"type": "context", "data": map[string]any{"calendars": []int{1}}}}},
		}})
	}
	nodes, err := d.browse(context.Background(), "model", itemMetric, 20)
	if err != nil {
		t.Fatal(err)
	}
	events := map[int]string{}
	for _, n := range nodes {
		events[n.ID] = strings.Join(n.Events, ",")
	}
	if events[200] != "Run" || events[201] != "" {
		t.Errorf("events = %v", events)
	}

}

// Attributes picked as numbers come back as numbers; values that are not
// numbers are empty, and the other attributes stay text.
func TestEventNumbers(t *testing.T) {
	v := func(s string) *string { return &s }
	got := numericValues([]*string{v("1.5"), v(" -3 "), v("1,25"), v("N/A"), nil, v("2e3"), v("1,234.5")})
	var parts []string
	for _, f := range got {
		if f == nil {
			parts = append(parts, "-")
		} else {
			parts = append(parts, strconv.FormatFloat(*f, 'f', -1, 64))
		}
	}
	if strings.Join(parts, ",") != "1.5,-3,1.25,-,-,2000,-" {
		t.Errorf("numbers = %s", strings.Join(parts, ","))
	}

	series := []flow.EventSeries{{ID: 2, Data: []flow.EventPeriod{{
		Start:      &flow.Time{Time: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)},
		Attributes: []flow.AttributeValue{{Name: "FLow", Value: "1.69"}, {Name: "Product", Value: "Juice"}},
	}}}}
	fr := eventTableFrame(series, map[int]eventInfo{2: {Path: "Trigger2", Attributes: []string{"FLow", "Product"}}}, time.Now(), []string{"flow"})
	flowField, _ := fr.FieldByName("FLow")
	product, _ := fr.FieldByName("Product")
	if flowField.Type() != data.FieldTypeNullableFloat64 || *flowField.At(0).(*float64) != 1.69 {
		t.Errorf("FLow = %v %v", flowField.Type(), flowField.At(0))
	}
	if product.Type() != data.FieldTypeNullableString {
		t.Errorf("Product type = %v, want text", product.Type())
	}
}

// An event periods variable lists the batches newest first, labelled by an
// attribute and valued by event, start and end; a filter keeps only the
// periods with one of the values.
func TestEventPeriodsVariable(t *testing.T) {
	_, d := eventServer(t)
	run := func(qm queryModel) []string {
		qm.QueryType, qm.VariableKind = queryTypeVariable, varEventPeriods
		b, _ := json.Marshal(qm)
		resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b, TimeRange: backend.TimeRange{
			From: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC),
		}})
		if resp.Error != nil {
			t.Fatal(resp.Error)
		}
		var out []string
		f := resp.Frames[0]
		for i := 0; i < f.Rows(); i++ {
			out = append(out, f.Fields[0].At(i).(string)+"="+f.Fields[1].At(i).(string))
		}
		return out
	}
	at := func(h int) string { return timeText(time.Date(2026, 10, 6, h, 0, 0, 0, time.UTC)) }
	got := run(queryModel{Items: runEvent, PeriodLabel: "Product"})
	want := []string{
		"Juice · " + at(3) + " (running)=2:1791255600000:",
		"Juice · " + at(1) + "=2:1791248400000:1791252000000",
		"Coke · " + at(0) + "=2:1791244800000:1791248400000",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("periods =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := run(queryModel{EventID: 2, EventAttribute: "Product", EventValues: []string{"coke"}}); len(got) != 1 || !strings.HasPrefix(got[0], at(0)+"=2:") {
		t.Errorf("Coke only = %v", got)
	}
}
