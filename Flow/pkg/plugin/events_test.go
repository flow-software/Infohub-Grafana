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
