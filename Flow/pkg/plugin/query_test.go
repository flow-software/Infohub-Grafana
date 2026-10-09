package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		Items:       []queryItem{{Tree: flow.TreeTemplate, Type: itemMetric, ID: 900}},
		Aggregation: "average",
		Bucket:      bucketAuto,
	})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if len(resp.Frames) != 3 {
		t.Fatalf("frames = %v, want two period series and one tag", displayNames(resp.Frames))
	}
	for _, want := range []string{"templateid=9001", "aggregation=average", "resolution=60000"} {
		if !strings.Contains(tagQuery, want) {
			t.Errorf("tag query %q lacks %q", tagQuery, want)
		}
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

func TestAutoAveragesOnlyTagsThatOverflowThePanel(t *testing.T) {
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

	b, _ := json.Marshal(queryModel{Items: []queryItem{{Tree: flow.TreeTemplate, Type: itemMeasure, ID: 9001}}})
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

func TestCalendarBucketWithoutPeriodsWarns(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/tags"] = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 400, map[string]any{"errors": []string{"No valid periods for the specified date range. Specify a valid date range or interval parameter"}})
	}
	d := testDatasource(srv)

	resp := runQuery(t, d, queryModel{
		Items:       []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 201}},
		Aggregation: "maximum", Bucket: bucketCalendar, TagInterval: "shiftly",
	})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if len(resp.Frames) != 1 || !strings.Contains(resp.Frames[0].Meta.Notices[0].Text, "no shiftly periods") {
		t.Errorf("frames = %+v", resp.Frames)
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

func TestValidateRejectsUnknownAggregation(t *testing.T) {
	qm := queryModel{Aggregation: "median"}
	if err := qm.validate(); err == nil {
		t.Error("median accepted")
	}
	qm = queryModel{Aggregation: "average", Bucket: bucketFixed, Resolution: "soon"}
	if err := qm.validate(); err == nil {
		t.Error("bad resolution accepted")
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
