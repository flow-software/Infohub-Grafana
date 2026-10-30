package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// fakeFlow serves a small model: two lines created from one template.
//
//	Site (1)
//	  Line 1 (2) -> metric Production (20, template 900) -> Good (200, hourly, template 9000), Speed (201, tag, template 9001)
//	  Line 2 (3) -> metric Production (30, template 900) -> Good (300, hourly, template 9000), Speed (301, tag, template 9001)
type fakeFlow struct {
	t        *testing.T
	mu       sync.Mutex
	requests []string
	// handlers override endpoints by path.
	handlers map[string]func(w http.ResponseWriter, r *http.Request)
}

func newFakeFlow(t *testing.T) (*fakeFlow, *httptest.Server) {
	f := &fakeFlow{t: t, handlers: map[string]func(http.ResponseWriter, *http.Request){}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

var (
	modelFolders = []map[string]any{
		{"id": 1, "name": "Site"},
		{"id": 2, "name": "Line 1", "parent": map[string]any{"id": 1}, "template": map[string]any{"id": 90}},
		{"id": 3, "name": "Line 2", "parent": map[string]any{"id": 1}, "template": map[string]any{"id": 90}},
	}
	modelMetrics = map[int]map[string]any{
		20: {"id": 20, "name": "Production", "folder": map[string]any{"id": 2}, "template": map[string]any{"id": 900}},
		30: {"id": 30, "name": "Production", "folder": map[string]any{"id": 3}, "template": map[string]any{"id": 900}},
	}
	modelMeasures = map[int]map[string]any{
		200: {"id": 200, "name": "Production.Good", "uom": "units", "interval": "hourly", "metric": map[string]any{"id": 20}, "template": map[string]any{"id": 9000}},
		201: {"id": 201, "name": "Production.Speed", "interval": "timestamp", "metric": map[string]any{"id": 20}, "template": map[string]any{"id": 9001}},
		300: {"id": 300, "name": "Production.Good", "uom": "units", "interval": "hourly", "metric": map[string]any{"id": 30}, "template": map[string]any{"id": 9000}},
		301: {"id": 301, "name": "Production.Speed", "interval": "timestamp", "metric": map[string]any{"id": 30}, "template": map[string]any{"id": 9001}},
	}
	templateMeasures = map[int]map[string]any{
		9000: {"id": 9000, "name": "Production.Good", "interval": "hourly", "metric": map[string]any{"id": 900}},
		9001: {"id": 9001, "name": "Production.Speed", "interval": "timestamp", "metric": map[string]any{"id": 900}},
	}
)

func byIDs[T any](all map[int]T, r *http.Request) []T {
	var out []T
	for _, s := range r.URL.Query()["id"] {
		var id int
		_ = json.Unmarshal([]byte(s), &id)
		if v, ok := all[id]; ok {
			out = append(out, v)
		}
	}
	return out
}

func (f *fakeFlow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.URL.Path+"?"+r.URL.RawQuery)
	f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer secret" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"errors": []string{"unauthorized"}})
		return
	}
	if h, ok := f.handlers[r.URL.Path]; ok {
		h(w, r)
		return
	}
	switch r.URL.Path {
	case "/api/token/validate":
		writeJSON(w, 200, map[string]any{})
	case "/api/v1/config/calendars":
		writeJSON(w, 200, map[string]any{"calendars": []map[string]any{{"id": 1, "name": "Default"}}})
	case "/api/v1/model/folders":
		writeJSON(w, 200, map[string]any{"folders": modelFolders})
	case "/api/v1/model/metrics":
		// Without ids, every metric, as Flow lists them.
		if len(r.URL.Query()["id"]) == 0 {
			var all []any
			for _, m := range modelMetrics {
				all = append(all, m)
			}
			writeJSON(w, 200, map[string]any{"metrics": all})
			return
		}
		writeJSON(w, 200, map[string]any{"metrics": byIDs(modelMetrics, r)})
	case "/api/v1/model/measures":
		writeJSON(w, 200, map[string]any{"measures": byIDs(modelMeasures, r)})
	case "/api/v1/template/measures":
		writeJSON(w, 200, map[string]any{"measures": byIDs(templateMeasures, r)})
	case "/api/v1/template/metrics/900/measures":
		writeJSON(w, 200, map[string]any{"measures": []any{templateMeasures[9000], templateMeasures[9001]}})
	case "/api/v1/data/measures":
		q := r.URL.Query()
		if q.Get("templateid") != "9000" || q.Get("calendarid") != "1" || !strings.HasSuffix(q.Get("start"), "Z") {
			f.t.Errorf("unexpected data/measures query %s", r.URL.RawQuery)
		}
		writeJSON(w, 200, map[string]any{
			"errors": []string{},
			"values": []map[string]any{
				{"id": 300, "name": "Production.Good", "data": []map[string]any{
					{"start": "2026-10-06T01:00:00Z", "end": "2026-10-06T02:00:00Z", "value": 7, "detail": map[string]any{"quality": map[string]any{"value": 192}}},
					{"start": "2026-10-06T00:00:00Z", "end": "2026-10-06T01:00:00Z", "value": 5, "detail": map[string]any{"quality": map[string]any{"value": 192}}},
				}},
				{"id": 200, "name": "Production.Good", "data": []map[string]any{
					{"start": "2026-10-06T00:00:00Z", "end": "2026-10-06T01:00:00Z", "value": 3, "detail": map[string]any{"quality": map[string]any{"value": 0}}},
				}},
			},
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"errors": []string{"no route " + r.URL.Path}})
	}
}

func (f *fakeFlow) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

func testDatasource(srv *httptest.Server) *Datasource {
	c := &flow.Client{BaseURL: srv.URL, Token: "secret", HTTP: srv.Client()}
	return &Datasource{client: c, index: newModelIndex(c, time.Minute)}
}

func runQuery(t *testing.T, d *Datasource, qm queryModel) backend.DataResponse {
	t.Helper()
	b, _ := json.Marshal(qm)
	return d.query(context.Background(), backend.DataQuery{
		RefID:     "A",
		JSON:      b,
		Interval:  time.Minute,
		TimeRange: backend.TimeRange{From: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC)},
	})
}

func displayNames(frames data.Frames) []string {
	var out []string
	for _, f := range frames {
		out = append(out, f.Fields[1].Config.DisplayNameFromDS)
	}
	return out
}

func TestTemplateMeasureReturnsEveryInstanceNamedByPath(t *testing.T) {
	_, srv := newFakeFlow(t)
	d := testDatasource(srv)

	resp := runQuery(t, d, queryModel{Items: []queryItem{{Tree: flow.TreeTemplate, Type: itemMeasure, ID: 9000}}})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	got := displayNames(resp.Frames)
	want := []string{"Site/Line 1/Production/Good", "Site/Line 2/Production/Good"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("series %v, want %v", got, want)
	}

	line2 := resp.Frames[1]
	if line2.Rows() != 2 {
		t.Fatalf("Line 2 rows = %d, want 2", line2.Rows())
	}
	// Sorted by period start even though Flow returned them out of order.
	if v, _ := line2.Fields[1].ConcreteAt(0); v.(float64) != 5 {
		t.Errorf("first Line 2 value = %v, want 5", v)
	}
	if line2.Fields[1].Config.Unit != "suffix: units" {
		t.Errorf("unit = %q", line2.Fields[1].Config.Unit)
	}
	labels := line2.Fields[1].Labels
	if labels["folder"] != "Site/Line 2" || labels["templateId"] != "9000" || labels["measureId"] != "300" {
		t.Errorf("labels = %v", labels)
	}
	if line2.Meta == nil || line2.Meta.Type != data.FrameTypeTimeSeriesMulti {
		t.Errorf("frame type = %+v, want timeseries-multi for alerting", line2.Meta)
	}
}

func TestBadQualityAsNullAndLegend(t *testing.T) {
	_, srv := newFakeFlow(t)
	d := testDatasource(srv)

	resp := runQuery(t, d, queryModel{
		Items:            []queryItem{{Tree: flow.TreeTemplate, Type: itemMeasure, ID: 9000}},
		BadQualityAsNull: true,
		IncludeQuality:   true,
		Legend:           "{{folder}} {{measure}}",
	})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	line1 := resp.Frames[0]
	if line1.Fields[1].Config.DisplayNameFromDS != "Site/Line 1 Good" {
		t.Errorf("legend = %q", line1.Fields[1].Config.DisplayNameFromDS)
	}
	if _, ok := line1.Fields[1].ConcreteAt(0); ok {
		t.Errorf("quality 0 value kept, want null")
	}
	if q := line1.Fields[2].At(0); q != "Bad" {
		t.Errorf("quality = %v, want Bad", q)
	}
}

func TestTemplateMetricSplitsPeriodMeasuresAndTags(t *testing.T) {
	f, srv := newFakeFlow(t)
	var tagQuery string
	f.handlers["/api/v1/data/tags"] = func(w http.ResponseWriter, r *http.Request) {
		tagQuery = r.URL.RawQuery
		writeJSON(w, 200, map[string]any{"values": []map[string]any{
			{"id": 201, "data": []map[string]any{{"start": "2026-10-06T00:10:00Z", "value": 1.5, "quality": map[string]any{"value": 192}}}},
		}})
	}
	d := testDatasource(srv)

	resp := runQuery(t, d, queryModel{
		Items: []queryItem{{Tree: flow.TreeTemplate, Type: itemMetric, ID: 900}},
	})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if len(resp.Frames) != 3 {
		t.Fatalf("frames = %v, want two period series and one tag", displayNames(resp.Frames))
	}
	// Auto: one value fits the panel, so it is read raw.
	if !strings.Contains(tagQuery, "templateid=9001") || strings.Contains(tagQuery, "aggregation") {
		t.Errorf("tag query = %q", tagQuery)
	}
	if strings.Contains(tagQuery, "9000") {
		t.Errorf("period measure sent to data/tags: %s", tagQuery)
	}
}

func TestTagPagingDropsRepeatedValues(t *testing.T) {
	f, srv := newFakeFlow(t)
	calls := 0
	f.handlers["/api/v1/data/tags"] = func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			// Tag 201 was cut off at 00:02; 301 was complete.
			writeJSON(w, 200, map[string]any{"continuation": "2026-10-06T00:02:00Z", "values": []map[string]any{
				{"id": 201, "data": []map[string]any{{"start": "2026-10-06T00:01:00Z", "value": 1}, {"start": "2026-10-06T00:02:00Z", "value": 2}}},
				{"id": 301, "data": []map[string]any{{"start": "2026-10-06T00:01:30Z", "value": 10}}},
			}})
			return
		}
		if got := r.URL.Query().Get("start"); got != "2026-10-06T00:02:00.000Z" {
			t.Errorf("second page start = %s", got)
		}
		writeJSON(w, 200, map[string]any{"values": []map[string]any{
			{"id": 201, "data": []map[string]any{{"start": "2026-10-06T00:02:00Z", "value": 2}, {"start": "2026-10-06T00:03:00Z", "value": 3}}},
			{"id": 301, "data": []map[string]any{{"start": "2026-10-06T00:01:30Z", "value": 10}}},
		}})
	}
	d := testDatasource(srv)

	resp := runQuery(t, d, queryModel{Items: []queryItem{{Tree: flow.TreeTemplate, Type: itemMeasure, ID: 9001}}, Aggregation: aggregationRaw})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	rows := map[string]int{}
	for _, fr := range resp.Frames {
		rows[fr.Fields[1].Labels["measureId"]] = fr.Rows()
	}
	if rows["201"] != 3 || rows["301"] != 1 {
		t.Errorf("rows = %v, want 201:3 301:1", rows)
	}
}

func TestAverageAveragesOnlyTagsThatOverflowThePanel(t *testing.T) {
	f, srv := newFakeFlow(t)
	var queries []string
	f.handlers["/api/v1/data/tags"] = func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		q := r.URL.Query()
		if q.Get("aggregation") == "" {
			if q.Get("limit") != "2" {
				t.Errorf("raw read limit = %s, want the panel's 2 points", q.Get("limit"))
			}
			// 201 fills the limit (more values exist); 301 fits.
			writeJSON(w, 200, map[string]any{"continuation": "2026-10-06T00:02:00Z", "values": []map[string]any{
				{"id": 201, "data": []map[string]any{{"start": "2026-10-06T00:01:00Z", "value": 1}, {"start": "2026-10-06T00:02:00Z", "value": 2}}},
				{"id": 301, "data": []map[string]any{{"start": "2026-10-06T00:01:30Z", "value": 10}}},
			}})
			return
		}
		if q.Get("tagid") != "201" || q.Get("templateid") != "" || q.Get("aggregation") != "average" || q.Get("resolution") != "60000" {
			t.Errorf("average re-read = %s", r.URL.RawQuery)
		}
		writeJSON(w, 200, map[string]any{"values": []map[string]any{
			{"id": 201, "data": []map[string]any{{"start": "2026-10-06T00:00:00Z", "value": 1.5}}},
		}})
	}
	d := testDatasource(srv)

	b, _ := json.Marshal(queryModel{Items: []queryItem{{Tree: flow.TreeTemplate, Type: itemMeasure, ID: 9001}}, Aggregation: aggregationAverage})
	resp := d.query(context.Background(), backend.DataQuery{
		RefID: "A", JSON: b, Interval: time.Minute, MaxDataPoints: 2,
		TimeRange: backend.TimeRange{From: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC)},
	})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if len(queries) != 2 {
		t.Fatalf("calls = %d, want raw then average", len(queries))
	}
	rows := map[string]int{}
	for _, fr := range resp.Frames {
		rows[fr.Fields[1].Labels["measureId"]] = fr.Rows()
	}
	if rows["201"] != 1 || rows["301"] != 1 {
		t.Errorf("rows = %v, want 201 averaged to 1 bucket and 301 raw", rows)
	}
	notices := resp.Frames[0].Meta.Notices
	if len(notices) != 1 || !strings.Contains(notices[0].Text, "averaged per 1m") {
		t.Errorf("notices = %+v", notices)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{time.Hour: "1h", time.Minute: "1m", 90 * time.Minute: "1h30m", 5 * time.Second: "5s"} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestEmptyRangeIsNoDataNotAnError(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 400, map[string]any{"errors": []string{"Sequence contains no elements"}})
	}
	d := testDatasource(srv)

	resp := runQuery(t, d, queryModel{Items: []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 200}}})
	if resp.Error != nil {
		t.Fatalf("error = %v, want no data", resp.Error)
	}
	if len(resp.Frames) != 0 {
		t.Errorf("frames = %d, want 0", len(resp.Frames))
	}
}

func TestMissingMeasureIsAWarning(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 200, "data": []any{}}}})
	}
	d := testDatasource(srv)

	resp := runQuery(t, d, queryModel{Items: []queryItem{
		{Tree: flow.TreeModel, Type: itemMeasure, ID: 200},
		{Tree: flow.TreeModel, Type: itemMeasure, ID: 999},
	}})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	notices := resp.Frames[0].Meta.Notices
	if len(notices) != 1 || !strings.Contains(notices[0].Text, "999") {
		t.Errorf("notices = %+v", notices)
	}
}

func TestIndexCachesLookups(t *testing.T) {
	f, srv := newFakeFlow(t)
	d := testDatasource(srv)
	for i := 0; i < 2; i++ {
		if _, err := d.index.describe(context.Background(), []int{200, 300}); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count("/api/v1/model/folders"); n != 1 {
		t.Errorf("folder loads = %d, want 1", n)
	}
	if n := f.count("/api/v1/model/measures"); n != 1 {
		t.Errorf("measure lookups = %d, want 1", n)
	}
}

func TestCheckHealth(t *testing.T) {
	_, srv := newFakeFlow(t)
	d := testDatasource(srv)
	res, _ := d.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if res.Status != backend.HealthStatusOk {
		t.Fatalf("health = %s", res.Message)
	}

	d.client.Token = "wrong"
	res, _ = d.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if res.Status != backend.HealthStatusError || !strings.Contains(res.Message, "401") {
		t.Errorf("bad token health = %s", res.Message)
	}
}

// Flow fails a whole tag read when it cannot read one tag (a Latch transform
// without a latch source); the other tags still show, with a warning.
func TestOneBadTagDoesNotFailTheRest(t *testing.T) {
	f, srv := newFakeFlow(t)
	calls := 0
	f.handlers["/api/v1/data/tags"] = func(w http.ResponseWriter, r *http.Request) {
		calls++
		ids := r.URL.Query()["tagid"]
		if slices.Contains(ids, "301") {
			writeJSON(w, 400, map[string]any{"errors": []string{"Could not determine a latch source"}})
			return
		}
		var values []map[string]any
		for _, id := range ids {
			n, _ := strconv.Atoi(id)
			values = append(values, map[string]any{"id": n, "data": []map[string]any{{"start": "2026-10-06T00:10:00Z", "value": 1.5}}})
		}
		writeJSON(w, 200, map[string]any{"values": values})
	}
	d := testDatasource(srv)
	resp := runQuery(t, d, queryModel{Items: []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 201}, {Tree: flow.TreeModel, Type: itemMeasure, ID: 301}}})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if len(resp.Frames) != 1 {
		t.Fatalf("frames = %d, want the readable tag only", len(resp.Frames))
	}
	var notices []string
	for _, n := range resp.Frames[0].Meta.Notices {
		notices = append(notices, n.Text)
	}
	if !slices.Contains(notices, "Flow could not read tag measure 301: Could not determine a latch source") {
		t.Errorf("notices = %q", notices)
	}

	// Only the bad tag selected: the panel shows Flow's error, as before.
	resp = runQuery(t, d, queryModel{Items: []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 301}}})
	if resp.Error == nil || !strings.Contains(resp.Error.Error(), "latch source") {
		t.Errorf("error = %v", resp.Error)
	}
}

// Every version of a value: "corrections" keeps the preferred values as the
// series and marks corrected periods with an annotation; "history" returns
// one table of every version, newest first.
func TestValueVersions(t *testing.T) {
	f, srv := newFakeFlow(t)
	var query string
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		v := func(start string, value float64, preferred bool, version int, typ, captured string, user any) map[string]any {
			return map[string]any{"start": start, "value": value, "user": user, "datetime": captured,
				"detail": map[string]any{"preferred": preferred, "quality": map[string]any{"value": 192}, "version": map[string]any{"value": version, "type": typ}}}
		}
		charl := map[string]any{"username": "charl", "name": "Charl"}
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 200, "data": []any{
			v("2026-10-06T00:00:00Z", 5, true, 1, "original", "2026-10-06T00:05:00Z", nil),
			v("2026-10-06T01:00:00Z", 6, true, 3, "manual", "2026-10-07T09:00:00Z", charl),
			v("2026-10-06T01:00:00Z", 5, false, 1, "original", "2026-10-06T01:05:00Z", nil),
			v("2026-10-06T01:00:00Z", 9, false, 2, "manual", "2026-10-07T08:00:00Z", charl),
		}}}})
	}
	d := testDatasource(srv)
	item := []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 200}}

	resp := runQuery(t, d, queryModel{Items: item, Versions: versionsCorrections})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if !strings.Contains(query, "preferred=false") || !strings.Contains(query, "users=true") {
		t.Errorf("query = %s", query)
	}
	if len(resp.Frames) != 2 || resp.Frames[0].Rows() != 2 {
		t.Fatalf("frames = %d, want the preferred series and the corrections", len(resp.Frames))
	}
	anno := resp.Frames[1]
	if anno.Meta == nil || anno.Meta.DataTopic != data.DataTopicAnnotations || anno.Rows() != 1 {
		t.Fatalf("corrections frame = %+v", anno.Meta)
	}
	title := strAt(fieldByName(t, anno, "title"), 0)
	text := strAt(fieldByName(t, anno, "text"), 0)
	at := func(day, hour, min int) string { return timeText(time.Date(2026, 10, day, hour, min, 0, 0, time.UTC)) }
	if !strings.Contains(title, ": 3 versions · latest by Charl, "+at(7, 9, 0)) ||
		text != "v3 · 6 · Manual · Charl "+at(7, 9, 0)+"<br>v2 · 9 · Manual · Charl "+at(7, 8, 0)+"<br>v1 · 5 · Original · "+at(6, 1, 5) {
		t.Errorf("title %q, text %q", title, text)
	}

	resp = runQuery(t, d, queryModel{Items: item, Versions: versionsHistory})
	if resp.Error != nil || len(resp.Frames) != 1 {
		t.Fatalf("history: %v, %d frames", resp.Error, len(resp.Frames))
	}
	h := resp.Frames[0]
	var got []string
	for i := 0; i < h.Rows(); i++ {
		v, _ := fieldByName(t, h, "Version").ConcreteAt(i)
		p, _ := fieldByName(t, h, "Preferred").ConcreteAt(i)
		got = append(got, fmt.Sprintf("%v/%v", v, p))
	}
	// Newest period first; the latest version first within it.
	if strings.Join(got, " ") != "3/Yes 2/ 1/ 1/Yes" {
		t.Errorf("history rows = %v", got)
	}

	// Without the options: the preferred values only, as before.
	resp = runQuery(t, d, queryModel{Items: item})
	if strings.Contains(query, "preferred=false") || len(resp.Frames) != 1 || len(resp.Frames[0].Fields) != 2 {
		t.Errorf("plain query %s gave %d frames", query, len(resp.Frames))
	}
}

// Auto reads a tag that overflows the panel as each interval's minimum and
// maximum, ordered so the line runs on from its previous point.
func TestAutoKeepsEachIntervalsMinimumAndMaximum(t *testing.T) {
	f, srv := newFakeFlow(t)
	var aggs []string
	f.handlers["/api/v1/data/tags"] = func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		agg := q.Get("aggregation")
		aggs = append(aggs, agg)
		switch agg {
		case "":
			writeJSON(w, 200, map[string]any{"continuation": "2026-10-06T00:02:00Z", "values": []map[string]any{
				{"id": 201, "data": []map[string]any{{"start": "2026-10-06T00:01:00Z", "value": 1}, {"start": "2026-10-06T00:02:00Z", "value": 2}}},
			}})
		case "minimum":
			writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 201, "data": []map[string]any{
				{"start": "2026-10-06T00:00:00Z", "value": 1}, {"start": "2026-10-06T00:01:00Z", "value": 2}, {"start": "2026-10-06T00:02:00Z", "value": 4},
			}}}})
		case "maximum":
			writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 201, "data": []map[string]any{
				{"start": "2026-10-06T00:00:00Z", "value": 9}, {"start": "2026-10-06T00:01:00Z", "value": 3}, {"start": "2026-10-06T00:02:00Z", "value": 4},
			}}}})
		default:
			t.Errorf("aggregation = %q", agg)
		}
	}
	d := testDatasource(srv)
	b, _ := json.Marshal(queryModel{Items: []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 201}}})
	resp := d.query(context.Background(), backend.DataQuery{
		RefID: "A", JSON: b, Interval: time.Minute, MaxDataPoints: 2,
		TimeRange: backend.TimeRange{From: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC)},
	})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if strings.Join(aggs, ",") != ",minimum,maximum" {
		t.Fatalf("reads = %v, want raw, minimum, maximum", aggs)
	}
	fr := resp.Frames[0]
	var got []string
	for i := 0; i < fr.Rows(); i++ {
		ts := fr.Fields[0].At(i).(time.Time)
		v := fr.Fields[1].At(i).(*float64)
		got = append(got, fmt.Sprintf("%s=%g", ts.Format("04:05"), *v))
	}
	// 00:00 starts low (no previous point); 00:01 starts with 3, nearer the
	// previous 9; 00:02 has one value, so one point.
	want := "00:00=1,00:30=9,01:00=3,01:30=2,02:00=4"
	if strings.Join(got, ",") != want {
		t.Errorf("points = %s, want %s", strings.Join(got, ","), want)
	}
}

// Panels saved with a tag aggregation from before 1.3.1 read as Auto
// rather than failing.
func TestOldTagAggregationsReadAsAuto(t *testing.T) {
	f, srv := newFakeFlow(t)
	var tagQuery string
	f.handlers["/api/v1/data/tags"] = func(w http.ResponseWriter, r *http.Request) {
		tagQuery = r.URL.RawQuery
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 201, "data": []map[string]any{{"start": "2026-10-06T00:10:00Z", "value": 1.5}}}}})
	}
	d := testDatasource(srv)
	saved := []byte(`{"items":[{"tree":"model","type":"measure","id":201}],"aggregation":"maximum","bucket":"calendar","tagInterval":"shiftly","resolution":"soon"}`)
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: saved, TimeRange: backend.TimeRange{
		From: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC),
	}})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if strings.Contains(tagQuery, "aggregation") || strings.Contains(tagQuery, "calendarid") {
		t.Errorf("tag query = %q, want a raw (Auto) read", tagQuery)
	}
}

// The editor's legend presets read cleanly when a part is empty.
func TestLegendNamePresets(t *testing.T) {
	info := seriesInfo{ID: 7, Path: "Site/Line 1/Production/Good", Folder: "Site/Line 1", Metric: "Production", Measure: "Good", UOM: "kg"}
	noUnit := info
	noUnit.UOM = ""
	noFolder := info
	noFolder.Folder = ""
	for _, c := range []struct {
		pattern string
		info    seriesInfo
		want    string
	}{
		{"", info, "Site/Line 1/Production/Good"},
		{"{{path}} ({{uom}})", info, "Site/Line 1/Production/Good (kg)"},
		{"{{path}} ({{uom}})", noUnit, "Site/Line 1/Production/Good"},
		{"{{measure}} ({{uom}})", noUnit, "Good"},
		{"{{folder}} · {{measure}}", info, "Site/Line 1 · Good"},
		{"{{folder}} · {{measure}}", noFolder, "Good"},
		{"{{metric}}.{{measure}}", info, "Production.Good"},
		{"{{id}}", info, "7"},
	} {
		if got := legendName(c.pattern, c.info); got != c.want {
			t.Errorf("legendName(%q) = %q, want %q", c.pattern, got, c.want)
		}
	}
}

// A measure nested two levels down is named in full, from its metric's
// list, though Flow's by-id answer skips its grandparent.
func TestDescribeNestedMeasureWholeName(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/model/measures"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"measures": []map[string]any{
			{"id": 210, "name": "Production.Rate.WTD", "metric": map[string]any{"id": 20}, "parent": map[string]any{"id": 209}},
		}})
	}
	f.handlers["/api/v1/model/metrics/20/measures"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"measures": []map[string]any{
			{"id": 208, "name": "Production.Speed", "metric": map[string]any{"id": 20}},
			{"id": 209, "name": "Production.Speed.Rate", "metric": map[string]any{"id": 20}, "parent": map[string]any{"id": 208}},
			{"id": 210, "name": "Production.Speed.Rate.WTD", "metric": map[string]any{"id": 20}, "parent": map[string]any{"id": 209}},
		}})
	}
	d := testDatasource(srv)
	infos, err := d.index.describe(context.Background(), []int{210})
	if err != nil {
		t.Fatal(err)
	}
	if got := infos[210]; got.Measure != "Speed.Rate.WTD" || got.Path != "Site/Line 1/Production/Speed.Rate.WTD" {
		t.Errorf("info = %+v", got)
	}
}

// Limit status comes from Flow's exceptions on each value, not from
// comparing values with the limits here.
func TestLimitStatusFollowsFlowsExceptions(t *testing.T) {
	f, srv := newFakeFlow(t)
	var exceptions string
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		exceptions = r.URL.Query().Get("exceptions")
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{
			// 50 is within although above "high" 40: Flow did not flag it.
			{"start": "2026-10-06T00:00:00Z", "end": "2026-10-06T01:00:00Z", "value": 50},
			{"start": "2026-10-06T01:00:00Z", "end": "2026-10-06T02:00:00Z", "value": 5, "limits": []map[string]any{{"values": map[string]any{"low": 10}, "exception": map[string]any{"low": true}}}},
			{"start": "2026-10-06T02:00:00Z", "end": "2026-10-06T03:00:00Z", "value": 99, "limits": []map[string]any{
				{"values": map[string]any{"low": 10}, "exception": map[string]any{"low": true}},
				{"values": map[string]any{"high": 90}, "exception": map[string]any{"high": true}},
			}},
			{"start": "2026-10-06T03:00:00Z", "end": "2026-10-06T04:00:00Z"},
		}}}})
	}
	d := testDatasource(srv)
	b, _ := json.Marshal(queryModel{Items: []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 300}}, LimitStatus: true})
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b, TimeRange: backend.TimeRange{
		From: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC),
	}})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if exceptions != "true" {
		t.Errorf("exceptions = %q, want true", exceptions)
	}
	fr := resp.Frames[0]
	field, _ := fr.FieldByName("Limits")
	if field == nil {
		t.Fatalf("no Limits field in %v", fr.Fields)
	}
	var got []string
	for i := 0; i < field.Len(); i++ {
		if v := field.At(i).(*string); v != nil {
			got = append(got, *v)
		} else {
			got = append(got, "-")
		}
	}
	want := "Within limits,Below low limit,Above high limit,-"
	if strings.Join(got, ",") != want {
		t.Errorf("statuses = %s, want %s", strings.Join(got, ","), want)
	}
}
