package plugin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// itemEvent selects a Flow event in an events query; a template event
// stands for every model event created from it.
const itemEvent = "event"

// Event query formats.
const (
	eventFormatTable       = "table"       // one row per event period
	eventFormatTimeline    = "timeline"    // state bands per attribute
	eventFormatAnnotations = "annotations" // regions on other panels
)

// eventPeriodLimit caps event periods per query, across all events.
const eventPeriodLimit = 10000

// eventInfo is an event as the frames name it: by folder path, with its
// attributes in Flow's order.
type eventInfo struct {
	Path       string
	Attributes []string
}

// readEvents reads the selected events' periods in the time range, with
// their names. Periods are sorted by start within each event.
func (d *Datasource) readEvents(ctx context.Context, items []queryItem, tr backend.TimeRange) ([]flow.EventSeries, map[int]eventInfo, []string, error) {
	var ids, templateIDs []int
	for _, it := range items {
		if it.Tree == flow.TreeTemplate {
			templateIDs = appendUnique(templateIDs, it.ID)
		} else {
			ids = appendUnique(ids, it.ID)
		}
	}
	res, err := d.client.EventData(ctx, flow.EventDataRequest{IDs: ids, TemplateIDs: templateIDs, From: tr.From, To: tr.To, Limit: eventPeriodLimit})
	if flow.IsNoData(err) || (err == nil && res == nil) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	notices := append([]string(nil), res.Errors...)
	if res.Continuation != nil {
		notices = append(notices, fmt.Sprintf("Flow returned only the first %d event periods; narrow the time range or select fewer events.", eventPeriodLimit))
	}

	var eventIDs []int
	for _, s := range res.Values {
		eventIDs = appendUnique(eventIDs, s.ID)
		sort.SliceStable(s.Data, func(i, j int) bool { return startOf(s.Data[i]).Before(startOf(s.Data[j])) })
	}
	infos := map[int]eventInfo{}
	events, err := d.client.Events(ctx, flow.TreeModel, eventIDs)
	if err != nil {
		notices = append(notices, "Could not look up event names: "+err.Error())
	}
	paths, err := d.index.eventPaths(ctx, events)
	if err != nil {
		notices = append(notices, "Could not look up event folders: "+err.Error())
	}
	for _, e := range events {
		info := eventInfo{Path: paths[e.ID]}
		if info.Path == "" {
			info.Path = e.Name
		}
		for _, a := range e.Attributes {
			info.Attributes = append(info.Attributes, a.Name)
		}
		infos[e.ID] = info
	}
	// Attributes only seen on periods (or events Flow did not describe)
	// still get a column.
	for _, s := range res.Values {
		info := infos[s.ID]
		if info.Path == "" {
			info.Path = s.Name
		}
		for _, p := range s.Data {
			for _, a := range p.Attributes {
				if !containsString(info.Attributes, a.Name) {
					info.Attributes = append(info.Attributes, a.Name)
				}
			}
		}
		infos[s.ID] = info
	}
	sort.SliceStable(res.Values, func(i, j int) bool { return infos[res.Values[i].ID].Path < infos[res.Values[j].ID].Path })
	return res.Values, infos, notices, nil
}

func (d *Datasource) runEvents(ctx context.Context, qm queryModel, q backend.DataQuery) (backend.DataResponse, error) {
	all, infos, notices, err := d.readEvents(ctx, qm.Items, q.TimeRange)
	if err != nil {
		return backend.DataResponse{}, err
	}
	now := time.Now()
	var frames data.Frames
	switch qm.EventFormat {
	case eventFormatTimeline:
		for _, s := range all {
			frames = append(frames, eventTimelineFrame(s, infos[s.ID], len(all) > 1))
		}
	case eventFormatAnnotations:
		frames = data.Frames{eventAnnotationFrame(all, infos, now)}
	default:
		frames = data.Frames{eventTableFrame(all, infos, now)}
	}
	if len(frames) == 0 {
		frames = data.Frames{data.NewFrame("events")}
	}
	if len(notices) > 0 {
		if frames[0].Meta == nil {
			frames[0].Meta = &data.FrameMeta{}
		}
		frames[0].Meta.Notices = append(frames[0].Meta.Notices, noticesOf(notices)...)
	}
	return backend.DataResponse{Frames: frames}, nil
}

func startOf(p flow.EventPeriod) time.Time {
	if p.Start == nil {
		return time.Time{}
	}
	return p.Start.Time
}

// attributeOf is the period's value of the named attribute.
func attributeOf(p flow.EventPeriod, name string) (flow.AttributeValue, bool) {
	return findAttribute(p.Attributes, name)
}

func findAttribute(attrs []flow.AttributeValue, name string) (flow.AttributeValue, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a, true
		}
	}
	return flow.AttributeValue{}, false
}

// attributeColors maps each attribute value to its colour in Flow, so
// table cells and timeline bands are coloured as Flow colours them.
func attributeColors(series []flow.EventSeries, attr string) data.ValueMappings {
	m := data.ValueMapper{}
	for _, s := range series {
		for _, p := range s.Data {
			if a, ok := attributeOf(p, attr); ok && a.Value != "" && a.Color != "" {
				m[a.Value] = data.ValueMappingResult{Color: a.Color}
			}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return data.ValueMappings{m}
}

// eventAttributes is every attribute of the given events, in order of
// first appearance.
func eventAttributes(series []flow.EventSeries, infos map[int]eventInfo) []string {
	var out []string
	for _, s := range series {
		for _, a := range infos[s.ID].Attributes {
			if !containsString(out, a) {
				out = append(out, a)
			}
		}
	}
	return out
}

// eventTableFrame has one row per event period, newest first: the event,
// start, end, duration and a column per attribute, each attribute cell
// coloured with the value's colour in Flow. A period still running has no
// end; its duration runs to now.
func eventTableFrame(series []flow.EventSeries, infos map[int]eventInfo, now time.Time) *data.Frame {
	type row struct {
		event string
		p     flow.EventPeriod
	}
	var rows []row
	for _, s := range series {
		for _, p := range s.Data {
			rows = append(rows, row{infos[s.ID].Path, p})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return startOf(rows[i].p).After(startOf(rows[j].p)) })

	attrs := eventAttributes(series, infos)
	n := len(rows)
	events, starts, ends, durations := make([]string, n), make([]*time.Time, n), make([]*time.Time, n), make([]*float64, n)
	values := make([][]*string, len(attrs))
	for i := range values {
		values[i] = make([]*string, n)
	}
	for i, r := range rows {
		events[i] = r.event
		if r.p.Start != nil {
			t := r.p.Start.Time
			starts[i] = &t
			end := now
			if r.p.End != nil {
				e := r.p.End.Time
				ends[i], end = &e, e
			}
			secs := end.Sub(t).Seconds()
			durations[i] = &secs
		}
		for j, name := range attrs {
			if a, ok := attributeOf(r.p, name); ok {
				v := a.Value
				values[j][i] = &v
			}
		}
	}
	duration := data.NewField("Duration", nil, durations)
	duration.Config = &data.FieldConfig{Unit: "s", Description: "To now for a period still running"}
	frame := data.NewFrame("events",
		data.NewField("Event", nil, events),
		data.NewField("Start", nil, starts),
		data.NewField("End", nil, ends),
		duration,
	)
	for j, name := range attrs {
		f := data.NewField(name, nil, values[j])
		f.Config = &data.FieldConfig{Mappings: attributeColors(series, name)}
		if f.Config.Mappings != nil {
			f.Config.Custom = map[string]any{"cellOptions": map[string]any{"type": "color-background", "mode": "basic"}}
		}
		frame.Fields = append(frame.Fields, f)
	}
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}
	return frame
}

// eventTimelineFrame lays one event out for Grafana's State timeline: a
// field per attribute holding the value from each period's start, and null
// from its end until the next period starts. An event without attributes
// gets one field that shows when it ran.
func eventTimelineFrame(s flow.EventSeries, info eventInfo, prefix bool) *data.Frame {
	attrs := info.Attributes
	running := len(attrs) == 0
	if running {
		attrs = []string{info.Path}
	}
	var times []time.Time
	values := make([][]*string, len(attrs))
	add := func(t time.Time, vals []*string) {
		times = append(times, t)
		for j := range attrs {
			values[j] = append(values[j], vals[j])
		}
	}
	for i, p := range s.Data {
		if p.Start == nil {
			continue
		}
		vals := make([]*string, len(attrs))
		for j, name := range attrs {
			if running {
				v := "Running"
				vals[j] = &v
			} else if a, ok := attributeOf(p, name); ok {
				v := a.Value
				vals[j] = &v
			}
		}
		add(p.Start.Time, vals)
		// Close the band unless the next period starts as this one ends.
		if p.End != nil && (i+1 >= len(s.Data) || s.Data[i+1].Start == nil || s.Data[i+1].Start.After(p.End.Time)) {
			add(p.End.Time, make([]*string, len(attrs)))
		}
	}
	frame := data.NewFrame(info.Path, data.NewField("Time", nil, times))
	for j, name := range attrs {
		f := data.NewField(name, nil, values[j])
		display := name
		if prefix && !running {
			display = info.Path + " · " + name
		}
		f.Config = &data.FieldConfig{DisplayNameFromDS: display}
		if running {
			f.Config.Mappings = data.ValueMappings{data.ValueMapper{"Running": data.ValueMappingResult{Color: "green"}}}
		} else {
			f.Config.Mappings = attributeColors([]flow.EventSeries{s}, name)
		}
		frame.Fields = append(frame.Fields, f)
	}
	return frame
}

// eventAnnotationFrame is the shape Grafana turns into annotations: time
// and timeEnd make a region, title and text label it, tags list the
// attribute values, and color shades it with the first attribute's colour.
// A period still running ends now.
func eventAnnotationFrame(series []flow.EventSeries, infos map[int]eventInfo, now time.Time) *data.Frame {
	var times, timeEnds []time.Time
	var titles, texts, tags, colors []string
	for _, s := range series {
		info := infos[s.ID]
		for _, p := range s.Data {
			if p.Start == nil {
				continue
			}
			end := now
			if p.End != nil {
				end = p.End.Time
			}
			var parts, vals []string
			color := ""
			for _, name := range info.Attributes {
				if a, ok := attributeOf(p, name); ok && a.Value != "" {
					parts = append(parts, name+": "+a.Value)
					vals = append(vals, a.Value)
					if color == "" {
						color = a.Color
					}
				}
			}
			times, timeEnds = append(times, p.Start.Time), append(timeEnds, end)
			titles, texts = append(titles, info.Path), append(texts, strings.Join(parts, "\n"))
			tags, colors = append(tags, strings.Join(vals, ",")), append(colors, color)
		}
	}
	return data.NewFrame("events",
		data.NewField("time", nil, times),
		data.NewField("timeEnd", nil, timeEnds),
		data.NewField("title", nil, titles),
		data.NewField("text", nil, texts),
		data.NewField("tags", nil, tags),
		data.NewField("color", nil, colors),
	)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
