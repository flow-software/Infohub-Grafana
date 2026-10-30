package plugin

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Item types a query can select. A metric stands for all of its measures,
// resolved when the query runs, so measures added later are picked up.
const (
	itemMetric  = "metric"
	itemMeasure = "measure"
	// itemChart stands for the measures in a Flow chart, of any type.
	itemChart = "chart"
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
	// ChartMode "measures" reads the chart's measures as a measures query,
	// for any panel, instead of laying the chart out as Flow does.
	ChartMode  string `json:"chartMode"`
	ChartLabel string `json:"chartLabel"`

	// Dependency queries (they also use Items).
	DepDirection string `json:"depDirection"`
	DepDepth     int    `json:"depDepth"`
	// DepValues "none" leaves out each measure's latest value.
	DepValues string `json:"depValues"`
	// DepNames "measure" titles nodes by the measure's own name ("WTD")
	// rather than Flow's full Metric.Parent.Measure name.
	DepNames string `json:"depNames"`
	// DepProcessing adds how far the engine has got with each measure
	// (periods behind, pending, queued), read from the Flow database.
	DepProcessing bool `json:"depProcessing"`
	// DepRing "processing" colours each ring by that state instead of the
	// retrieval type.
	DepRing string `json:"depRing"`

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

	// AttributeFilter with AttributeValues keeps only measures whose model
	// attribute has one of the values, e.g. Line = "Line 1" (dashboard
	// variables are replaced in the frontend). No values keeps all.
	AttributeFilter string   `json:"attributeFilter"`
	AttributeValues []string `json:"attributeValues"`
	// Limits "lines" draws each measure's Flow limits as threshold lines;
	// LimitNames keeps only those limits (no names draws them all).
	Limits     string   `json:"limits"`
	LimitNames []string `json:"limitNames"`
	// Annotate "comments" returns the measures' value comments as
	// annotations instead of their values (annotation queries).
	Annotate string `json:"annotate"`

	// Variable queries: what to list, see variables.go.
	VariableKind      string `json:"variableKind"`
	VariableAttribute string `json:"variableAttribute"`

	// Measure queries.
	Items []queryItem `json:"items"`
	// CalendarID for period measures; zero uses the datasource default.
	CalendarID int `json:"calendarId"`
	// Aggregation "raw" reads every raw tag value, and "average" raw values
	// while they fit the panel, else averages per panel interval. Anything
	// else is Auto: raw values while they fit, else each panel interval's
	// minimum and maximum, so spikes stay visible. (The other aggregations
	// of 1.3.0 and before read as Auto.)
	Aggregation string `json:"aggregation"`
	// TimeAt places each period value at the period's "start" (default) or
	// "end"; in event context "period" draws it from start to end.
	TimeAt           string `json:"timeAt"`
	IncludeQuality   bool   `json:"includeQuality"`
	BadQualityAsNull bool   `json:"badQualityAsNull"`
	// IncludeVersion adds each value's version, its type and who entered it.
	IncludeVersion bool `json:"includeVersion"`
	// Versions reads every version of each period's value: "corrections"
	// draws the preferred values as usual and marks each corrected period
	// with an annotation listing its versions; "history" returns one table
	// of every version, like Flow's History tab.
	Versions string `json:"versions"`
	// Legend names each series; {{path}}, {{folder}}, {{metric}},
	// {{measure}}, {{id}} and {{uom}} are replaced. Empty uses the full path.
	// Not ${...}, which Grafana would take for dashboard variables.
	Legend string `json:"legend"`
}

// aggregationRaw forces raw tag values however many there are;
// aggregationAverage smooths tags that overflow the panel into averages.
const (
	aggregationRaw     = "raw"
	aggregationAverage = "average"
)

func (qm *queryModel) validate() error {
	switch qm.QueryType {
	case "", queryTypeMeasures:
	case queryTypeChart:
		return nil
	case queryTypeVariable:
		for _, it := range qm.Items {
			if !it.Tree.Valid() || (it.Type != itemMetric && it.Type != itemMeasure && it.Type != itemEvent && it.Type != itemFolder) {
				return fmt.Errorf("item %d: unknown %s %s", it.ID, it.Tree, it.Type)
			}
		}
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
		if it.Type != itemMetric && it.Type != itemMeasure && it.Type != itemChart && it.Type != itemFolder {
			return fmt.Errorf("item %d: unknown type %q", it.ID, it.Type)
		}
	}
	if qm.Context != "" && qm.Context != contextEvent {
		return fmt.Errorf("unknown context %q", qm.Context)
	}
	if qm.Limits != "" && qm.Limits != limitsLines {
		return fmt.Errorf("unknown limits option %q", qm.Limits)
	}
	if qm.Annotate != "" && qm.Annotate != annotateComments {
		return fmt.Errorf("unknown annotation kind %q", qm.Annotate)
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
	// Measures from charts: Flow lists model and template measures together,
	// so ids not found among the model's are looked up as template ones.
	fromChart := map[int]bool{}
	for _, it := range items {
		switch it.Type {
		case itemFolder:
			measures, err := d.folderMeasures(ctx, it.Tree, it.ID)
			if err != nil {
				return nil, fmt.Errorf("measures of %s folder %d: %w", it.Tree, it.ID, err)
			}
			if len(measures) == 0 {
				sel.warnings = append(sel.warnings, fmt.Sprintf("%s has no measures you can read", itemName(it)))
			}
			for _, m := range measures {
				sel.add(it.Tree, m)
			}
		case itemChart:
			measures, err := d.chartMeasures(ctx, it.ID)
			if err != nil {
				return nil, fmt.Errorf("measures of chart %d: %w", it.ID, err)
			}
			if len(measures) == 0 {
				sel.warnings = append(sel.warnings, fmt.Sprintf("%s has no measures you can read", itemName(it)))
			}
			for _, m := range measures {
				fromChart[m.ID] = true
				measureIDs[flow.TreeModel] = appendUnique(measureIDs[flow.TreeModel], m.ID)
			}
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
			if known[id] {
				continue
			}
			if tree == flow.TreeModel && fromChart[id] {
				measureIDs[flow.TreeTemplate] = appendUnique(measureIDs[flow.TreeTemplate], id)
				continue
			}
			sel.warnings = append(sel.warnings, fmt.Sprintf("%s measure %d no longer exists or is not accessible", tree, id))
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
	// superseded holds a measure's older versions, drawn as markers.
	superseded bool
}

type point struct {
	t       time.Time
	value   *float64
	text    *string
	quality int
	// version, versionType and user describe the value's version, when read;
	// captured is when Flow stored it.
	version     *int
	versionType string
	user        string
	captured    time.Time
}

// Versions modes: read every version of each value.
const (
	versionsCorrections = "corrections"
	versionsHistory     = "history"
)

// allVersions reports a query that reads every version.
func (qm queryModel) allVersions() bool {
	return qm.Versions == versionsCorrections || qm.Versions == versionsHistory
}

// versionTypes name Flow's VersionType values, by number and by the enum
// name the public API sends (in any case).
var versionTypes = map[string]string{
	"1": "Original (updateable)", "originalupdateable": "Original (updateable)",
	"2": "Original", "original": "Original",
	"3": "Updated", "updated": "Updated",
	"4": "Manual", "manual": "Manual",
	"5": "Deleted", "deleted": "Deleted",
	"6": "Manual request", "manualrequest": "Manual request",
}

// withVersion fills a point's version, its type and who entered it.
func withVersion(pt *point, v flow.PeriodValue) {
	if v.Detail != nil {
		pt.version = v.Detail.Version.Value
		if raw := strings.Trim(string(v.Detail.Version.Type), `"`); raw != "" && raw != "null" {
			pt.versionType = versionTypes[strings.ToLower(raw)]
			if pt.versionType == "" {
				pt.versionType = raw
			}
		}
	}
	pt.user = userName(v.User)
	if v.DateTime != nil {
		pt.captured = v.DateTime.Time
	}
}

func (d *Datasource) run(ctx context.Context, qm queryModel, q backend.DataQuery) (backend.DataResponse, error) {
	if qm.Annotate == annotateComments {
		return d.runComments(ctx, qm, q)
	}
	sel, err := d.resolve(ctx, qm.Items)
	if err != nil {
		return backend.DataResponse{}, err
	}
	notices := append([]string(nil), sel.warnings...)
	var all []series

	// Model measures outside the attribute filter are not read at all.
	// Template instances are expanded by Flow, so those are filtered after
	// reading, below.
	if qm.AttributeFilter != "" && len(qm.AttributeValues) > 0 {
		measures, err := d.modelMeasures(ctx)
		if err != nil {
			return backend.DataResponse{}, fmt.Errorf("reading model attributes: %w", err)
		}
		keep := func(ids []int) []int {
			var out []int
			for _, id := range ids {
				if m, ok := measures[id]; ok && attributeMatch(m, qm.AttributeFilter, qm.AttributeValues) {
					out = append(out, id)
				}
			}
			return out
		}
		sel.periodIDs, sel.tagIDs = keep(sel.periodIDs), keep(sel.tagIDs)
	}

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
		got, warn, err := d.readPeriods(ctx, sel, calendarID, q.TimeRange, qm)
		if err != nil && qm.CalendarID == 0 && flow.IsCalendarMismatch(err) {
			// No calendar picked and the default is not one of these
			// measures': read them in the calendar most of them have.
			if alt, name := d.contextCalendar(ctx, sel); alt != 0 && alt != calendarID {
				got, warn, err = d.readPeriods(ctx, sel, alt, q.TimeRange, qm)
				notices = append(notices, fmt.Sprintf("Read in the %s calendar: the data source's default calendar is not a calendar of these measures in Flow. Pick a calendar to read them in another.", name))
			}
		}
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

	if qm.AttributeFilter != "" && len(qm.AttributeValues) > 0 {
		var warn string
		all, warn, err = d.filterByAttribute(ctx, all, qm.AttributeFilter, qm.AttributeValues)
		if err != nil {
			return backend.DataResponse{}, err
		}
		if warn != "" {
			notices = append(notices, warn)
		}
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
		if all[i].value != all[j].value {
			return all[i].value < all[j].value
		}
		return !all[i].superseded && all[j].superseded
	})
	infoOf := func(id int) seriesInfo {
		if info, ok := infos[id]; ok {
			return info
		}
		return seriesInfo{ID: id, Path: fmt.Sprintf("measure %d", id)}
	}
	// Superseded versions are not series of their own: they mark corrected
	// periods, or join the preferred values in the history table.
	var superseded []series
	kept := all[:0]
	for _, s := range all {
		if s.superseded {
			superseded = append(superseded, s)
		} else {
			kept = append(kept, s)
		}
	}
	all = kept
	var resp backend.DataResponse
	if qm.Versions == versionsHistory {
		resp.Frames = data.Frames{historyFrame(all, superseded, infoOf, qm)}
		if hasTags(all) {
			notices = append(notices, "Tags have no versions, so they are left out of the history table.")
		}
		all = nil
	}
	for _, s := range all {
		resp.Frames = append(resp.Frames, buildFrame(s, infoOf(s.id), qm))
	}
	if qm.Versions == versionsCorrections {
		if f := correctionsFrame(all, superseded, infoOf, qm); f != nil {
			resp.Frames = append(resp.Frames, f)
		}
	}
	if qm.Limits == limitsLines && len(all) > 0 {
		notices = append(notices, d.applyLimits(ctx, resp.Frames, all, qm)...)
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

func (d *Datasource) readPeriods(ctx context.Context, sel *selection, calendarID int, tr backend.TimeRange, qm queryModel) ([]series, []string, error) {
	atEnd := qm.TimeAt == "end"
	all := qm.allVersions()
	res, err := d.client.MeasureData(ctx, flow.MeasureDataRequest{
		IDs: sel.periodIDs, TemplateIDs: sel.periodTemplateIDs,
		CalendarID: calendarID, From: tr.From, To: tr.To, Limit: periodLimit,
		Users: qm.IncludeVersion || all, AllVersions: all,
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
		old := series{id: v.ID, superseded: true}
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
			withVersion(&pt, p)
			if all && p.Detail != nil && !p.Detail.Preferred {
				old.points = append(old.points, pt)
				continue
			}
			s.points = append(s.points, pt)
		}
		sort.SliceStable(s.points, func(i, j int) bool { return s.points[i].t.Before(s.points[j].t) })
		out = append(out, s)
		if len(old.points) > 0 {
			sort.SliceStable(old.points, func(i, j int) bool { return old.points[i].t.Before(old.points[j].t) })
			out = append(out, old)
		}
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

	if qm.Aggregation == aggregationRaw {
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

	bucketed := func(agg string) ([]series, []string, error) {
		return d.readTags(ctx, flow.TagDataRequest{
			IDs: full, From: q.TimeRange.From, To: q.TimeRange.To,
			Aggregation: agg, ResolutionMs: interval, Limit: tagPageLimit,
		}, tagMaxPages)
	}
	var reduced []series
	var how string
	if qm.Aggregation == aggregationAverage {
		avg, w, err := bucketed("average")
		if err != nil {
			return nil, nil, err
		}
		reduced, how = avg, "averaged"
		warnings = append(warnings, w...)
	} else {
		mins, w, err := bucketed("minimum")
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, w...)
		maxs, w, err := bucketed("maximum")
		if err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, w...)
		reduced, how = minMaxSeries(mins, maxs, time.Duration(interval)*time.Millisecond), "shown as their minimum and maximum"
	}
	byID := make(map[int]series, len(reduced))
	for _, s := range reduced {
		byID[s.id] = s
	}
	for i, s := range raw {
		if a, ok := byID[s.id]; ok {
			raw[i] = a
		}
	}
	warnings = append(warnings, fmt.Sprintf("%d tag(s) have more raw values in this range than the panel can show, so they are %s per %s. Pick Raw under Tags to get every value.",
		len(full), how, shortDuration(time.Duration(interval)*time.Millisecond)))
	return raw, warnings, nil
}

// minMaxSeries draws each interval's minimum and maximum as two points, so
// a line through them still reaches every spike and dip. Flow gives both at
// the interval's start without saying which came first, so the one nearer
// the line's previous point goes first (at the start), the other halfway
// through the interval: the line does not double back on itself.
func minMaxSeries(mins, maxs []series, interval time.Duration) []series {
	type key struct {
		id int
		t  int64
	}
	maxAt := map[key]point{}
	for _, s := range maxs {
		for _, p := range s.points {
			maxAt[key{s.id, p.t.UnixNano()}] = p
		}
	}
	out := make([]series, 0, len(mins))
	for _, s := range mins {
		merged := series{id: s.id, tag: s.tag}
		var prev *float64
		for _, lo := range s.points {
			hi, ok := maxAt[key{s.id, lo.t.UnixNano()}]
			if !ok || lo.value == nil || hi.value == nil || *lo.value == *hi.value {
				merged.points = append(merged.points, lo)
				if lo.value != nil {
					prev = lo.value
				}
				continue
			}
			first, second := lo, hi
			if prev != nil && math.Abs(*hi.value-*prev) < math.Abs(*lo.value-*prev) {
				first, second = hi, lo
			}
			second.t = first.t.Add(interval / 2)
			merged.points = append(merged.points, first, second)
			prev = second.value
		}
		out = append(out, merged)
	}
	return out
}

// readTags follows Flow's continuation to page through raw values. Flow sets
// one continuation for the whole answer (the earliest cut-off of any tag), so
// a later page repeats values of tags that were not cut off; those are
// dropped by keeping only values after each tag's last one.
// readTags reads tag values. Flow fails the whole call when it cannot read
// one of the tags (a Latch transform without a latch source, for one), so a
// failing call over several tags is retried a tag at a time: the others
// still show, and each tag Flow rejects becomes a warning.
func (d *Datasource) readTags(ctx context.Context, req flow.TagDataRequest, maxPages int) ([]series, []string, error) {
	out, warnings, err := d.readTagPages(ctx, req, maxPages)
	var apiErr *flow.APIError
	if err == nil || len(req.IDs)+len(req.TemplateIDs) < 2 || !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		return out, warnings, err
	}
	firstErr := err
	out, warnings = nil, nil
	seen := map[string]bool{}
	var failed []string
	read := func(one flow.TagDataRequest, label string) error {
		s, w, err := d.readTagPages(ctx, one, maxPages)
		var apiErr *flow.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
			failed = append(failed, fmt.Sprintf("Flow could not read %s: %s", label, strings.Join(apiErr.Messages, "; ")))
			return nil
		}
		if err != nil {
			return err
		}
		out = append(out, s...)
		for _, x := range w {
			if !seen[x] {
				seen[x] = true
				warnings = append(warnings, x)
			}
		}
		return nil
	}
	for _, id := range req.IDs {
		one := req
		one.IDs, one.TemplateIDs = []int{id}, nil
		if err := read(one, fmt.Sprintf("tag measure %d", id)); err != nil {
			return nil, nil, err
		}
	}
	for _, id := range req.TemplateIDs {
		one := req
		one.IDs, one.TemplateIDs = nil, []int{id}
		if err := read(one, fmt.Sprintf("template tag measure %d", id)); err != nil {
			return nil, nil, err
		}
	}
	if len(failed) == len(req.IDs)+len(req.TemplateIDs) {
		return nil, nil, firstErr
	}
	return out, append(warnings, failed...), nil
}

// readTagPages reads tag values in one call per page.
func (d *Datasource) readTagPages(ctx context.Context, req flow.TagDataRequest, maxPages int) ([]series, []string, error) {
	byID := map[int]*series{}
	var order []int
	var warnings []string
	seenWarning := map[string]bool{}

	for page := 0; ; page++ {
		res, err := d.client.TagData(ctx, req)
		if flow.IsNoData(err) {
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
