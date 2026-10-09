package plugin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/gtime"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Item types a query can select. A metric stands for all of its measures,
// resolved when the query runs, so measures added later are picked up.
const (
	itemMetric  = "metric"
	itemMeasure = "measure"
)

// Tag bucket modes for aggregated tag queries.
const (
	bucketAuto     = "auto"     // the panel's interval
	bucketFixed    = "fixed"    // Resolution, e.g. "15m"
	bucketCalendar = "calendar" // calendar periods of TagInterval, e.g. shifts
)

type queryItem struct {
	Tree flow.Tree `json:"tree"`
	Type string    `json:"type"`
	ID   int       `json:"id"`
	// Label is what the query editor shows; the backend does not use it.
	Label string `json:"label"`
}

// Query types. Unset is a measures query, the original kind.
const (
	queryTypeMeasures = "measures"
	queryTypeChart    = "chart"
	queryTypeDeps     = "dependencies"
	queryTypeEvents   = "events"
)

// Measure query contexts: calendar periods (the default) or event periods.
const contextEvent = "event"

// timeAtPeriod draws each event-period value from the period's start to its
// end ("whole period"); the other TimeAt values are "start" and "end".
const timeAtPeriod = "period"

type queryModel struct {
	QueryType string `json:"queryType"`

	// Chart queries: the chart's periods within the panel's time range.
	ChartID int `json:"chartId"`

	// Dependency queries (they also use Items).
	DepDirection string `json:"depDirection"`
	DepDepth     int    `json:"depDepth"`
	// DepValues "none" leaves out each measure's latest value.
	DepValues string `json:"depValues"`
	// DepNames "measure" titles nodes by the measure's own name ("WTD")
	// rather than Flow's full Metric.Parent.Measure name.
	DepNames string `json:"depNames"`

	// Events queries (items are events): table, timeline or annotations.
	EventFormat string `json:"eventFormat"`

	// Context "event" reads period measures per period of EventID instead
	// of per calendar period. EventAttribute with EventValues keeps only
	// periods with one of those values; EventSplit gives each value of
	// EventAttribute its own series.
	Context        string   `json:"context"`
	EventID        int      `json:"eventId"`
	EventAttribute string   `json:"eventAttribute"`
	EventValues    []string `json:"eventValues"`
	EventSplit     bool     `json:"eventSplit"`

	// Measure queries.
	Items []queryItem `json:"items"`
	// CalendarID for period measures; zero uses the datasource default.
	CalendarID int `json:"calendarId"`
	// Aggregation for tags: empty for auto (raw while it fits the panel,
	// else averages), "raw" for every raw value, or a Flow AggregationType.
	Aggregation string `json:"aggregation"`
	Bucket      string `json:"bucket"`
	Resolution  string `json:"resolution"`
	TagInterval string `json:"tagInterval"`
	// TimeAt places each period value at the period's "start" (default) or
	// "end"; in event context "period" draws it from start to end.
	TimeAt           string `json:"timeAt"`
	IncludeQuality   bool   `json:"includeQuality"`
	BadQualityAsNull bool   `json:"badQualityAsNull"`
	// Legend names each series; {{path}}, {{folder}}, {{metric}},
	// {{measure}}, {{id}} and {{uom}} are replaced. Empty uses the full path.
	// Not ${...}, which Grafana would take for dashboard variables.
	Legend string `json:"legend"`
}

// aggregationRaw forces raw tag values however many there are.
const aggregationRaw = "raw"

// aggregations are Flow's AggregationType names accepted by data/tags.
var aggregations = map[string]bool{
	"sum": true, "average": true, "minimum": true, "maximum": true, "counter": true,
	"range": true, "delta": true, "first": true, "last": true, "count": true,
	"timeInState": true, "variance": true, "standardDeviation": true,
}

func (qm *queryModel) validate() error {
	switch qm.QueryType {
	case "", queryTypeMeasures:
	case queryTypeChart:
		return nil
	case queryTypeEvents:
		switch qm.EventFormat {
		case "", eventFormatTable, eventFormatTimeline, eventFormatAnnotations:
		default:
			return fmt.Errorf("unknown event format %q", qm.EventFormat)
		}
		for _, it := range qm.Items {
			if !it.Tree.Valid() || it.Type != itemEvent {
				return fmt.Errorf("item %d: an events query takes events, not %s %s", it.ID, it.Tree, it.Type)
			}
		}
		return nil
	case queryTypeDeps:
		if qm.DepDirection != "" && qm.DepDirection != depInputs && qm.DepDirection != depUsedBy {
			return fmt.Errorf("unknown dependency direction %q", qm.DepDirection)
		}
		if qm.DepNames != "" && qm.DepNames != depNamesMeasure {
			return fmt.Errorf("unknown dependency node names %q", qm.DepNames)
		}
	default:
		return fmt.Errorf("unknown query type %q", qm.QueryType)
	}
	for _, it := range qm.Items {
		if !it.Tree.Valid() {
			return fmt.Errorf("item %d: unknown tree %q", it.ID, it.Tree)
		}
		if it.Type != itemMetric && it.Type != itemMeasure {
			return fmt.Errorf("item %d: unknown type %q", it.ID, it.Type)
		}
	}
	if qm.Context != "" && qm.Context != contextEvent {
		return fmt.Errorf("unknown context %q", qm.Context)
	}
	if qm.Aggregation != "" && qm.Aggregation != aggregationRaw && !aggregations[qm.Aggregation] {
		return fmt.Errorf("unknown aggregation %q", qm.Aggregation)
	}
	if aggregations[qm.Aggregation] && qm.Bucket == bucketFixed {
		if _, err := gtime.ParseDuration(qm.Resolution); err != nil {
			return fmt.Errorf("invalid resolution %q: use a duration such as 30s, 15m or 1h", qm.Resolution)
		}
	}
	return nil
}

// selection is the query's items resolved to measure ids, split by how Flow
// reads them. Template ids expand on the server to all their instances.
type selection struct {
	periodIDs, periodTemplateIDs []int
	tagIDs, tagTemplateIDs       []int
	warnings                     []string
}

func (s *selection) add(tree flow.Tree, m flow.Measure) {
	switch {
	case tree == flow.TreeTemplate && m.IsTag():
		s.tagTemplateIDs = appendUnique(s.tagTemplateIDs, m.ID)
	case tree == flow.TreeTemplate:
		s.periodTemplateIDs = appendUnique(s.periodTemplateIDs, m.ID)
	case m.IsTag():
		s.tagIDs = appendUnique(s.tagIDs, m.ID)
	default:
		s.periodIDs = appendUnique(s.periodIDs, m.ID)
	}
}

func (d *Datasource) resolve(ctx context.Context, items []queryItem) (*selection, error) {
	sel := &selection{}
	measureIDs := map[flow.Tree][]int{}
	for _, it := range items {
		switch it.Type {
		case itemMeasure:
			measureIDs[it.Tree] = appendUnique(measureIDs[it.Tree], it.ID)
		case itemMetric:
			measures, err := d.client.MetricMeasures(ctx, it.Tree, it.ID)
			if err != nil {
				return nil, fmt.Errorf("measures of %s metric %d: %w", it.Tree, it.ID, err)
			}
			if len(measures) == 0 {
				sel.warnings = append(sel.warnings, fmt.Sprintf("%s has no measures you can read", itemName(it)))
			}
			for _, m := range measures {
				sel.add(it.Tree, m)
			}
		}
	}
	for _, tree := range []flow.Tree{flow.TreeModel, flow.TreeTemplate} {
		ids := measureIDs[tree]
		if len(ids) == 0 {
			continue
		}
		// Looked up for their interval, which decides the endpoint.
		found, err := d.client.Measures(ctx, tree, ids)
		if err != nil {
			return nil, fmt.Errorf("looking up %s measures: %w", tree, err)
		}
		known := map[int]bool{}
		for _, m := range found {
			known[m.ID] = true
			sel.add(tree, m)
		}
		for _, id := range ids {
			if !known[id] {
				sel.warnings = append(sel.warnings, fmt.Sprintf("%s measure %d no longer exists or is not accessible", tree, id))
			}
		}
	}
	return sel, nil
}

func itemName(it queryItem) string {
	if it.Label != "" {
		return it.Label
	}
	return fmt.Sprintf("%s %s %d", it.Tree, it.Type, it.ID)
}

// Caps on how much one query pulls from Flow.
const (
	// Period values across all measures in one call. Flow does not order
	// them, so the plugin asks for plenty and warns when the cap is hit.
	periodLimit = 100000
	// Tag values per tag per call, and pages followed per query.
	tagPageLimit = 10000
	tagMaxPages  = 20
)

// series is one measure's values in a common shape for framing.
type series struct {
	id     int
	points []point
	tag    bool
	// attribute and value name a series split by an event attribute, and
	// color is the value's colour in Flow.
	attribute, value, color string
}

type point struct {
	t       time.Time
	value   *float64
	text    *string
	quality int
}

func (d *Datasource) run(ctx context.Context, qm queryModel, q backend.DataQuery) (backend.DataResponse, error) {
	sel, err := d.resolve(ctx, qm.Items)
	if err != nil {
		return backend.DataResponse{}, err
	}
	notices := append([]string(nil), sel.warnings...)
	var all []series

	if qm.Context == contextEvent && len(sel.periodIDs)+len(sel.periodTemplateIDs) > 0 {
		got, warn, err := d.readEventPeriods(ctx, sel, qm, q.TimeRange)
		if err != nil {
			return backend.DataResponse{}, err
		}
		all = append(all, got...)
		notices = append(notices, warn...)
	} else if len(sel.periodIDs)+len(sel.periodTemplateIDs) > 0 {
		calendarID, err := d.calendarFor(ctx, qm.CalendarID)
		if err != nil {
			return backend.DataResponse{}, err
		}
		got, warn, err := d.readPeriods(ctx, sel, calendarID, q.TimeRange, qm.TimeAt == "end")
		if err != nil {
			return backend.DataResponse{}, err
		}
		all = append(all, got...)
		notices = append(notices, warn...)
	}

	if len(sel.tagIDs)+len(sel.tagTemplateIDs) > 0 {
		got, warn, err := d.readTagSelection(ctx, sel, qm, q)
		if err != nil {
			return backend.DataResponse{}, err
		}
		all = append(all, got...)
		notices = append(notices, warn...)
	}

	ids := make([]int, len(all))
	for i, s := range all {
		ids[i] = s.id
	}
	infos, err := d.index.describe(ctx, ids)
	if err != nil {
		// Data without good names beats no data.
		notices = append(notices, "Could not look up series names: "+err.Error())
		infos = map[int]seriesInfo{}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if a, b := pathOf(infos, all[i].id), pathOf(infos, all[j].id); a != b {
			return a < b
		}
		return all[i].value < all[j].value
	})
	var resp backend.DataResponse
	for _, s := range all {
		info, ok := infos[s.id]
		if !ok {
			info = seriesInfo{ID: s.id, Path: fmt.Sprintf("measure %d", s.id)}
		}
		resp.Frames = append(resp.Frames, buildFrame(s, info, qm))
	}
	if len(notices) > 0 {
		if len(resp.Frames) == 0 {
			resp.Frames = append(resp.Frames, data.NewFrame("notices"))
		}
		first := resp.Frames[0]
		if first.Meta == nil {
			first.Meta = &data.FrameMeta{}
		}
		for _, n := range notices {
			first.Meta.Notices = append(first.Meta.Notices, data.Notice{Severity: data.NoticeSeverityWarning, Text: n})
		}
	}
	return resp, nil
}

func pathOf(infos map[int]seriesInfo, id int) string {
	if info, ok := infos[id]; ok {
		return info.Path
	}
	return ""
}

func (d *Datasource) readPeriods(ctx context.Context, sel *selection, calendarID int, tr backend.TimeRange, atEnd bool) ([]series, []string, error) {
	res, err := d.client.MeasureData(ctx, flow.MeasureDataRequest{
		IDs: sel.periodIDs, TemplateIDs: sel.periodTemplateIDs,
		CalendarID: calendarID, From: tr.From, To: tr.To, Limit: periodLimit,
	})
	if flow.IsNoData(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if res == nil {
		// 204: none of the ids exist any more.
		return nil, []string{"Flow found none of the selected period measures"}, nil
	}
	warnings := append([]string(nil), res.Errors...)
	if res.Continuation != nil {
		warnings = append(warnings, fmt.Sprintf("Flow returned only the first %d periods; some values are missing. Narrow the time range or select fewer measures.", periodLimit))
	}
	out := make([]series, 0, len(res.Values))
	for _, v := range res.Values {
		s := series{id: v.ID, points: make([]point, 0, len(v.Data))}
		for _, p := range v.Data {
			t := p.Start
			if atEnd && p.End != nil {
				t = p.End
			}
			if t == nil {
				continue
			}
			pt := point{t: t.Time, value: p.Value, quality: 192}
			if p.Detail != nil {
				pt.quality = p.Detail.Quality.Value
			}
			s.points = append(s.points, pt)
		}
		sort.SliceStable(s.points, func(i, j int) bool { return s.points[i].t.Before(s.points[j].t) })
		out = append(out, s)
	}
	return out, warnings, nil
}

// readTagSelection reads the selected tags as the query's aggregation asks.
//
// Auto (no aggregation) reads raw values up to the panel's point count. Tags
// with more values than that come back cut off; those are read again as
// averages per panel interval, which costs one call instead of paging through
// what can be hundreds of thousands of values, most of which the panel could
// not draw anyway.
func (d *Datasource) readTagSelection(ctx context.Context, sel *selection, qm queryModel, q backend.DataQuery) ([]series, []string, error) {
	req := flow.TagDataRequest{
		IDs: sel.tagIDs, TemplateIDs: sel.tagTemplateIDs,
		From: q.TimeRange.From, To: q.TimeRange.To,
		Limit: tagPageLimit,
	}
	interval := max(q.Interval.Milliseconds(), 1000)

	switch {
	case qm.Aggregation == aggregationRaw:
		return d.readTags(ctx, req, tagMaxPages)

	case qm.Aggregation != "":
		req.Aggregation = qm.Aggregation
		switch qm.Bucket {
		case bucketCalendar:
			calendarID, err := d.calendarFor(ctx, qm.CalendarID)
			if err != nil {
				return nil, nil, err
			}
			req.CalendarID, req.Interval = calendarID, qm.TagInterval
			if req.Interval == "" {
				req.Interval = "hourly"
			}
		case bucketFixed:
			res, _ := gtime.ParseDuration(qm.Resolution)
			req.ResolutionMs = max(res.Milliseconds(), 1)
		default:
			req.ResolutionMs = interval
		}
		return d.readTags(ctx, req, tagMaxPages)
	}

	// Auto.
	points := int(q.MaxDataPoints)
	if points <= 0 {
		points = 1000
	}
	req.Limit = points
	raw, warnings, err := d.readTags(ctx, req, 1)
	if err != nil {
		return nil, nil, err
	}
	var full []int
	for _, s := range raw {
		if len(s.points) >= points {
			full = append(full, s.id)
		}
	}
	if len(full) == 0 {
		return raw, warnings, nil
	}

	avg, avgWarnings, err := d.readTags(ctx, flow.TagDataRequest{
		IDs: full, From: q.TimeRange.From, To: q.TimeRange.To,
		Aggregation: "average", ResolutionMs: interval, Limit: tagPageLimit,
	}, tagMaxPages)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[int]series, len(avg))
	for _, s := range avg {
		byID[s.id] = s
	}
	for i, s := range raw {
		if a, ok := byID[s.id]; ok {
			raw[i] = a
		}
	}
	warnings = append(warnings, avgWarnings...)
	warnings = append(warnings, fmt.Sprintf("%d tag(s) have more raw values in this range than the panel can show, so they are averaged per %s. Pick Raw values under Tags to get every value.",
		len(full), shortDuration(time.Duration(interval)*time.Millisecond)))
	return raw, warnings, nil
}

// readTags follows Flow's continuation to page through raw values. Flow sets
// one continuation for the whole answer (the earliest cut-off of any tag), so
// a later page repeats values of tags that were not cut off; those are
// dropped by keeping only values after each tag's last one.
func (d *Datasource) readTags(ctx context.Context, req flow.TagDataRequest, maxPages int) ([]series, []string, error) {
	byID := map[int]*series{}
	var order []int
	var warnings []string
	seenWarning := map[string]bool{}

	for page := 0; ; page++ {
		res, err := d.client.TagData(ctx, req)
		if flow.IsNoData(err) {
			if req.CalendarID > 0 {
				// Not always an empty range: Flow keeps no shift periods
				// unless a measure uses them, for example.
				warnings = append(warnings, fmt.Sprintf("Flow has no %s periods in this calendar for the time range, so there is nothing to aggregate into.", req.Interval))
			}
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if res == nil {
			if page == 0 {
				warnings = append(warnings, "Flow found none of the selected tags")
			}
			break
		}
		for _, w := range res.Errors {
			if !seenWarning[w] {
				seenWarning[w] = true
				warnings = append(warnings, w)
			}
		}
		for _, v := range res.Values {
			s, ok := byID[v.ID]
			if !ok {
				s = &series{id: v.ID, tag: true}
				byID[v.ID] = s
				order = append(order, v.ID)
			}
			for _, r := range v.Data {
				if r.Start == nil {
					continue
				}
				if n := len(s.points); n > 0 && !r.Start.After(s.points[n-1].t) {
					continue
				}
				pt := point{t: r.Start.Time, value: r.Value, quality: r.Quality.Value}
				if r.Value == nil && r.StringValue != nil {
					pt.text = r.StringValue
				}
				s.points = append(s.points, pt)
			}
		}
		if res.Continuation == nil || !res.Continuation.After(req.From) || !res.Continuation.Before(req.To) {
			break
		}
		if page+1 >= maxPages {
			if maxPages > 1 {
				warnings = append(warnings, fmt.Sprintf("Stopped after %d raw values per tag, so the end of the range is missing. Narrow the time range or use an aggregation.", req.Limit*maxPages))
			}
			break
		}
		req.From = res.Continuation.Time
	}

	out := make([]series, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, warnings, nil
}

// shortDuration drops zero units: "1h" rather than "1h0m0s".
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func appendUnique(ids []int, id int) []int {
	for _, v := range ids {
		if v == id {
			return ids
		}
	}
	return append(ids, id)
}

// legendName expands the query's legend pattern for one series.
func legendName(pattern string, info seriesInfo) string {
	if strings.TrimSpace(pattern) == "" {
		return info.Path
	}
	name := strings.NewReplacer(
		"{{path}}", info.Path,
		"{{folder}}", info.Folder,
		"{{metric}}", info.Metric,
		"{{measure}}", info.Measure,
		"{{id}}", fmt.Sprint(info.ID),
		"{{uom}}", info.UOM,
	).Replace(pattern)
	// A measure without a unit, or a measure outside any folder, leaves
	// "()" or a dangling separator from the editor's presets.
	name = strings.TrimSpace(strings.ReplaceAll(name, "()", ""))
	return strings.Trim(name, " ·.")
}
