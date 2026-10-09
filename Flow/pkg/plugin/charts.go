package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// chartLimit is a limit's colours from the chart definition. Colours that
// name a Flow theme entry ("theme.Accent") are dropped: Grafana cannot
// resolve them.
type chartLimit struct {
	ID                               int
	Name                             string
	ForeHigh, ForeLow, ForeTarget    string
	BackHigh, BackLow, BackTarget    string
	ChartHigh, ChartLow, ChartTarget string
}

// chartRow is one measure row of a time-based chart.
type chartRow struct {
	Section        string
	Title          string
	MeasureID      int
	UOM            string
	MeasureLimitID int
	LimitID        int
	Comparator     string
}

// chartHeader is one of the row header columns the chart shows, in order.
type chartHeader struct {
	Kind  string // name, uom, lowlimit, limit, highlimit
	Title string
}

type chartDefinition struct {
	Name     string
	Type     string
	Interval string
	Limits   map[int]chartLimit
	Rows     []chartRow
	Headers  []chartHeader
}

// The row header components a time-based table can show, and the column
// each becomes. Context, sparkline and link columns have no Grafana
// equivalent in a table and are left out.
var headerKinds = map[string]string{
	"tables-timebased-nameheader":      "name",
	"tables-timebased-uomheader":       "uom",
	"tables-timebased-lowlimitheader":  "lowlimit",
	"tables-timebased-limitheader":     "limit",
	"tables-timebased-highlimitheader": "highlimit",
}

var defaultHeaderTitles = map[string]string{
	"name": "Measure", "uom": "UOM", "lowlimit": "Low Limit", "limit": "Limit", "highlimit": "High Limit",
}

// parseChartDefinition walks the component tree that the Flow web app draws
// a chart from. Only what a table needs is kept: sections, measure rows with
// their limit, the row header columns, and the limit colours.
func parseChartDefinition(raw json.RawMessage) (*chartDefinition, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("could not read the chart definition: %w", err)
	}
	def := &chartDefinition{
		Name:     str(root["name"]),
		Type:     str(root["chartType"]),
		Interval: str(root["intervalType"]),
		Limits:   map[int]chartLimit{},
	}
	for _, l := range list(root["limits"]) {
		m, _ := l.(map[string]any)
		id := num(m["id"])
		def.Limits[id] = chartLimit{
			ID: id, Name: str(m["n"]),
			ForeHigh: color(m["tfch"]), ForeLow: color(m["tfcl"]), ForeTarget: color(m["tfct"]),
			BackHigh: color(m["tbch"]), BackLow: color(m["tbcl"]), BackTarget: color(m["tbct"]),
			ChartHigh: color(m["cch"]), ChartLow: color(m["ccl"]), ChartTarget: color(m["cct"]),
		}
	}

	seenHeader := map[string]bool{}
	var walk func(node map[string]any, section string)
	walk = func(node map[string]any, section string) {
		ct := str(node["componentType"])
		if strings.HasSuffix(ct, "sectioncomponent") {
			section = str(node["title"])
		}
		if kind, ok := headerKinds[ct]; ok && !seenHeader[kind] {
			seenHeader[kind] = true
			title := str(node["title"])
			if title == "" {
				title = defaultHeaderTitles[kind]
			}
			def.Headers = append(def.Headers, chartHeader{Kind: kind, Title: title})
		}
		for _, it := range list(node["items"]) {
			item, _ := it.(map[string]any)
			if row, ok := measureRow(item, section); ok {
				def.Rows = append(def.Rows, row)
				// A measure row's own items (details, latest value, ...) may
				// name other measures; they are not rows.
				continue
			}
			walk(item, section)
		}
		for _, c := range list(node["components"]) {
			if child, ok := c.(map[string]any); ok {
				walk(child, section)
			}
		}
	}
	walk(root, "")

	sort.SliceStable(def.Headers, func(i, j int) bool { return headerOrder(def.Headers[i].Kind) < headerOrder(def.Headers[j].Kind) })
	if len(def.Headers) == 0 {
		def.Headers = []chartHeader{{Kind: "name", Title: "Measure"}, {Kind: "uom", Title: "UOM"}}
	}
	return def, nil
}

func headerOrder(kind string) int {
	return map[string]int{"name": 0, "uom": 1, "lowlimit": 2, "limit": 3, "highlimit": 4}[kind]
}

// measureRow recognises a chart item that is a measure row: it names a
// measure and is not itself a limit or detail entry.
func measureRow(item map[string]any, section string) (chartRow, bool) {
	t := str(item["itemType"])
	if !strings.HasSuffix(t, "measureitem") {
		return chartRow{}, false
	}
	m, _ := item["measure"].(map[string]any)
	id := num(m["measureID"])
	if id == 0 {
		return chartRow{}, false
	}
	row := chartRow{Section: section, Title: str(item["title"]), MeasureID: id, UOM: str(m["uom"])}
	if row.Title == "" {
		row.Title = str(m["hierarchicalName"])
	}
	for _, it := range list(item["items"]) {
		child, _ := it.(map[string]any)
		if ml, ok := child["measureLimit"].(map[string]any); ok && num(ml["measureLimitID"]) != 0 {
			row.MeasureLimitID = num(ml["measureLimitID"])
			row.LimitID = num(ml["limitID"])
			row.Comparator = str(ml["comparatorType"])
		}
	}
	return row, true
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func num(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

func color(v any) string {
	s := strings.TrimSpace(str(v))
	if s == "" || strings.HasPrefix(s, "theme.") {
		return ""
	}
	return s
}

// chartCache keeps chart definitions, because Flow records a chart usage on
// every definition read and a refreshing dashboard would add one per panel
// per refresh.
type chartCache struct {
	mu      sync.Mutex
	entries map[int]chartCacheEntry
}

type chartCacheEntry struct {
	def *chartDefinition
	at  time.Time
}

func (d *Datasource) chartDefinition(ctx context.Context, chartID int) (*chartDefinition, error) {
	d.charts.mu.Lock()
	if e, ok := d.charts.entries[chartID]; ok && time.Since(e.at) < cacheTTL {
		d.charts.mu.Unlock()
		return e.def, nil
	}
	d.charts.mu.Unlock()

	raw, err := d.client.ChartDefinition(ctx, chartID)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("chart %d does not exist", chartID)
	}
	def, err := parseChartDefinition(raw)
	if err != nil {
		return nil, err
	}
	d.charts.mu.Lock()
	if d.charts.entries == nil {
		d.charts.entries = map[int]chartCacheEntry{}
	}
	d.charts.entries[chartID] = chartCacheEntry{def: def, at: time.Now()}
	d.charts.mu.Unlock()
	return def, nil
}

func (d *Datasource) runChart(ctx context.Context, qm queryModel, q backend.DataQuery) (backend.DataResponse, error) {
	def, err := d.chartDefinition(ctx, qm.ChartID)
	if err != nil {
		return backend.DataResponse{}, err
	}
	if !strings.Contains(def.Type, "timebased") {
		return backend.DataResponse{}, &badRequest{fmt.Sprintf("chart %q is a %s; only time-based charts can be shown as a table so far", def.Name, def.Type)}
	}
	if len(def.Rows) == 0 {
		return backend.DataResponse{}, &badRequest{fmt.Sprintf("chart %q has no measures", def.Name)}
	}

	cd, err := d.client.ChartData(ctx, qm.ChartID, &q.TimeRange.From, &q.TimeRange.To)
	if err != nil {
		return backend.DataResponse{}, err
	}
	if cd == nil {
		cd = &flow.ChartData{}
	}

	j := joinChart(def, cd)
	frame := j.tableFrame(def)
	frame.Name = def.Name
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}
	if j.attributeValuesSkipped {
		frame.Meta.Notices = append(frame.Meta.Notices, data.Notice{
			Severity: data.NoticeSeverityInfo,
			Text:     "Rows split by event context are not shown yet; only the measures' overall values are.",
		})
	}
	return backend.DataResponse{Frames: data.Frames{frame}}, nil
}

// joined is a chart's definition and data matched up.
type joined struct {
	periods []flow.ChartPeriod
	labels  []string
	// values[measureID][periodID]
	values map[int]map[int64]flow.ChartValue
	// limit is the latest limit value per measure limit, as Flow's table
	// headers show it.
	limit                  map[int]flow.ChartLimitValue
	users                  map[int]string
	attributeValuesSkipped bool
}

func joinChart(def *chartDefinition, cd *flow.ChartData) *joined {
	j := &joined{values: map[int]map[int64]flow.ChartValue{}, limit: map[int]flow.ChartLimitValue{}, users: map[int]string{}}
	for _, u := range cd.Users {
		j.users[u.ID] = u.Name
	}

	// Columns: the chart's base interval. Flow may add other periods (to-date
	// columns) that the table draws separately.
	for _, p := range cd.Periods {
		if def.Interval == "" || strings.EqualFold(p.Interval, def.Interval) {
			j.periods = append(j.periods, p)
		}
	}
	sort.SliceStable(j.periods, func(a, b int) bool { return j.periods[a].Start.Before(j.periods[b].Start.Time) })
	j.labels = periodLabels(j.periods)

	for _, v := range cd.Values {
		if v.AttributeValueID != nil {
			j.attributeValuesSkipped = true
			continue
		}
		byPeriod, ok := j.values[v.MeasureID]
		if !ok {
			byPeriod = map[int64]flow.ChartValue{}
			j.values[v.MeasureID] = byPeriod
		}
		if _, dup := byPeriod[v.PeriodID]; !dup {
			byPeriod[v.PeriodID] = v
		}
	}

	for _, lv := range cd.LimitValues {
		if lv.AttributeValue != nil && *lv.AttributeValue != "" {
			continue
		}
		cur, ok := j.limit[lv.MeasureLimitID]
		if !ok || (lv.TimeStamp != nil && (cur.TimeStamp == nil || !lv.TimeStamp.Before(cur.TimeStamp.Time))) {
			j.limit[lv.MeasureLimitID] = lv
		}
	}
	return j
}

// periodLabels uses Flow's own formatted period start. When labels repeat
// (hourly columns over several days) the date is added to keep columns apart.
func periodLabels(periods []flow.ChartPeriod) []string {
	count := map[string]int{}
	for _, p := range periods {
		count[p.StartLabel]++
	}
	out := make([]string, len(periods))
	for i, p := range periods {
		label := p.StartLabel
		if label == "" {
			label = p.Start.Format("2006-01-02 15:04")
		} else if count[label] > 1 && p.Year > 0 {
			label = fmt.Sprintf("%04d-%02d-%02d %s", p.Year, p.Month, p.Day, label)
		}
		out[i] = label
	}
	return out
}

// cellStatus is how a value compares with its row's limit, from Flow's own
// exception flags.
func cellStatus(v flow.ChartValue, limitID int) string {
	if v.Value == nil {
		return ""
	}
	for _, ex := range v.Exceptions {
		if ex.LimitID != limitID {
			continue
		}
		switch {
		case ex.High:
			return "High"
		case ex.Low:
			return "Low"
		}
	}
	if limitID == 0 {
		return ""
	}
	return "OK"
}

// withinLimitColor marks a value within its limit when the limit sets no
// target colour of its own; readable on Grafana's light and dark themes.
const withinLimitColor = "#3D9A3A"

// cellStyle is the CSS Grafana's table applies to a cell, mirroring Flow's
// limit colours: high and low use the limit's high/low colours, a value
// within the limit its target colours, or green when it has none. Flow falls
// back from text colours to chart colours, and so does this.
func cellStyle(status string, l chartLimit) string {
	var fore, back string
	switch status {
	case "High":
		fore, back = l.ForeHigh, l.BackHigh
		if fore == "" && back == "" {
			fore = l.ChartHigh
		}
	case "Low":
		fore, back = l.ForeLow, l.BackLow
		if fore == "" && back == "" {
			fore = l.ChartLow
		}
	case "OK":
		fore, back = l.ForeTarget, l.BackTarget
		if fore == "" && back == "" {
			fore = l.ChartTarget
		}
		if fore == "" && back == "" {
			fore = withinLimitColor
		}
	}
	style := map[string]string{}
	if fore != "" {
		style["color"] = fore
	}
	if back != "" {
		style["background"] = back
	}
	if fore != "" && back == "" {
		style["fontWeight"] = "500"
	}
	b, _ := json.Marshal(style)
	return string(b)
}

// commentText joins a value's comments, oldest first, each with who wrote
// it and when, for the cell's tooltip. Nil when there are none, so Grafana
// shows no tooltip marker on the cell.
func (j *joined) commentText(v flow.ChartValue, ok bool) *string {
	if !ok || len(v.Comments) == 0 {
		return nil
	}
	comments := append([]flow.ChartComment(nil), v.Comments...)
	sort.SliceStable(comments, func(a, b int) bool {
		ta, tb := comments[a].Time, comments[b].Time
		return ta != nil && tb != nil && ta.Before(tb.Time)
	})
	lines := make([]string, 0, len(comments))
	for _, c := range comments {
		var by []string
		if c.UserID != nil && j.users[*c.UserID] != "" {
			by = append(by, j.users[*c.UserID])
		}
		if c.Time != nil {
			by = append(by, c.Time.UTC().Format("2006-01-02 15:04 UTC"))
		}
		line := c.Text
		if len(by) > 0 {
			line += "\n— " + strings.Join(by, ", ")
		}
		lines = append(lines, line)
	}
	s := strings.Join(lines, "\n\n")
	return &s
}

func (j *joined) formatted(v flow.ChartValue, ok bool) *string {
	if !ok || v.Value == nil {
		return nil
	}
	s := v.Formatted
	if s == "" {
		s = fmt.Sprint(*v.Value)
	}
	return &s
}

func (j *joined) limitText(row chartRow, kind string) *string {
	lv, ok := j.limit[row.MeasureLimitID]
	if !ok || row.MeasureLimitID == 0 {
		return nil
	}
	var s string
	switch kind {
	case "lowlimit":
		s = lv.FormattedLow
	case "highlimit":
		s = lv.FormattedHigh
	case "limit":
		s = lv.FormattedTgt
		if s != "" && row.Comparator != "" {
			s = row.Comparator + " " + s
		}
	}
	if s == "" {
		return nil
	}
	return &s
}

// hiddenField is a helper column the table reads but does not show.
//
// Grafana hides it through hideFrom.viz. A panel built from the panel
// editor's Suggestions ignores that until the dashboard is saved and
// reloaded, so the column is also made as narrow as Grafana allows, with a
// blank header and its text made transparent through the shared
// invisibleField style, to keep it out of sight there.
//
// The blank header is slot zero-width spaces: Grafana's table keys row
// values by display name, so every helper needs a different one.
func hiddenField[T any](name string, slot int, values []T) *data.Field {
	f := data.NewField(name, nil, values)
	f.Config = &data.FieldConfig{
		DisplayNameFromDS: strings.Repeat(zeroWidthSpace, slot),
		Custom: map[string]any{
			"hidden":     true,
			"hideFrom":   map[string]bool{"viz": true, "legend": true, "tooltip": true},
			"width":      1,
			"minWidth":   1,
			"inspect":    false,
			"filterable": false,
			"styleField": invisibleField,
		},
	}
	return f
}

// zeroWidthSpace builds blank, distinct helper column headers.
var zeroWidthSpace = string(rune(0x200B))

// invisibleField holds, on every row, a style that makes text invisible.
// Helper columns point their styleField at it (see hiddenField).
const invisibleField = "__invisible"

func invisibleStyleField(rows int) *data.Field {
	styles := make([]string, rows)
	for i := range styles {
		styles[i] = `{"color":"transparent"}`
	}
	return hiddenField(invisibleField, 1, styles)
}

func hasSections(def *chartDefinition) bool {
	for _, r := range def.Rows {
		if r.Section != "" {
			return true
		}
	}
	return false
}

// tableFrame lays the chart out as Flow's time-based table does: one row per
// measure, the chart's header columns, then one column per period. Each
// period column takes its cell colours from a hidden style column through
// Grafana's table "styleField".
func (j *joined) tableFrame(def *chartDefinition) *data.Frame {
	n := len(def.Rows)
	frame := data.NewFrame("")
	if hasSections(def) {
		sections := make([]string, n)
		for i, r := range def.Rows {
			sections[i] = r.Section
		}
		frame.Fields = append(frame.Fields, data.NewField("Section", nil, sections))
	}
	for _, h := range def.Headers {
		vals := make([]*string, n)
		for i, r := range def.Rows {
			switch h.Kind {
			case "name":
				t := r.Title
				vals[i] = &t
			case "uom":
				if r.UOM != "" {
					u := r.UOM
					vals[i] = &u
				}
			default:
				vals[i] = j.limitText(r, h.Kind)
			}
		}
		f := data.NewField(h.Title, nil, vals)
		frame.Fields = append(frame.Fields, f)
	}

	for pi, p := range j.periods {
		vals := make([]*string, n)
		styles := make([]string, n)
		comments := make([]*string, n)
		hasComments := false
		for i, r := range def.Rows {
			v, ok := j.values[r.MeasureID][p.ID]
			vals[i] = j.formatted(v, ok)
			styles[i] = cellStyle(cellStatus(v, r.LimitID), def.Limits[r.LimitID])
			comments[i] = j.commentText(v, ok)
			hasComments = hasComments || comments[i] != nil
		}
		styleName := fmt.Sprintf("__style_%d", pi)
		custom := map[string]any{"styleField": styleName, "align": "right"}
		value := data.NewField(j.labels[pi], nil, vals)
		style := hiddenField(styleName, 2*pi+2, styles)
		frame.Fields = append(frame.Fields, value, style)
		if hasComments {
			// Grafana's table shows a marker in the corner of a cell whose
			// tooltip field has a value, and the comments on hover, much as
			// Flow marks commented cells.
			commentName := fmt.Sprintf("__comments_%d", pi)
			//
			// Grafana sizes the tooltip like a cell of the comments column:
			// the period column's width, and one row high unless that column
			// has dynamic height. With wrapped text and dynamic height the
			// box grows to fit the comments.
			custom["tooltip"] = map[string]any{"field": commentName, "placement": "bottom"}
			c := hiddenField(commentName, 2*pi+3, comments)
			c.Config.Custom["wrapText"] = true
			c.Config.Custom["cellOptions"] = map[string]any{"type": "auto", "wrapText": true, "dynamicHeight": true}
			frame.Fields = append(frame.Fields, c)
		}
		value.Config = &data.FieldConfig{Custom: custom}
	}
	frame.Fields = append(frame.Fields, invisibleStyleField(n))
	return frame
}
