package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// limitsLines draws each measure's limits as dashed lines that follow the
// limit's dated settings.
const limitsLines = "lines"

// Threshold colours: a bound to stay within, and a target to aim for.
const (
	limitBoundColor  = "red"
	limitTargetColor = "green"
)

// measureLimit is one limit of a measure in one calendar or event, as it
// applies now.
type measureLimit struct {
	Name       string
	Comparator string // >, = or <
	High       *float64
	Target     *float64
	Low        *float64
	// FromMeasure lists the values that come from another measure; those
	// change over time and are not drawn.
	FromMeasure []string
	// Steps are the limit's dated settings (Flow's limit instances), oldest
	// first: each applies from its date until the next one.
	Steps []limitStep
}

// limitStep is one dated setting of a limit, applying from From.
type limitStep struct {
	From  time.Time
	Props limitProps
}

type limitProps struct {
	Comparator string   `json:"comparator"`
	High       *float64 `json:"high"`
	Target     *float64 `json:"target"`
	Low        *float64 `json:"low"`
	HighID     *int     `json:"highid"`
	TargetID   *int     `json:"targetid"`
	LowID      *int     `json:"lowid"`
}

// measureLimits reads a measure's limits for one calendar or event (by
// name) from its metadata: limits belong to a calendar in Flow, and a
// period measure read in one calendar must not show another's. Tags are
// read without a calendar, so for a tag (anyContext) whose limits are all
// in one context, those are used whatever the name. Of a limit's dated
// settings the latest applies.
func measureLimits(m flow.Measure, context string, anyContext bool) []measureLimit {
	byContext := map[string]map[string]map[string]limitProps{}
	for _, md := range m.Metadata {
		if !strings.EqualFold(md.Type, "limit") {
			continue
		}
		for ctxName, raw := range md.Data {
			var limits map[string]map[string]limitProps
			if json.Unmarshal(raw, &limits) != nil {
				continue
			}
			if byContext[ctxName] == nil {
				byContext[ctxName] = map[string]map[string]limitProps{}
			}
			for name, dated := range limits {
				byContext[ctxName][name] = dated
			}
		}
	}
	chosen, ok := byContext[context]
	if !ok {
		for name, limits := range byContext {
			if strings.EqualFold(name, context) {
				chosen, ok = limits, true
			}
		}
	}
	if !ok && anyContext && len(byContext) == 1 {
		for _, limits := range byContext {
			chosen = limits
		}
	}
	var out []measureLimit
	for name, dated := range chosen {
		p, found := latestLimit(dated)
		if !found {
			continue
		}
		l := measureLimit{Name: name, Comparator: p.Comparator, High: p.High, Target: p.Target, Low: p.Low}
		for key, props := range dated {
			l.Steps = append(l.Steps, limitStep{From: parseFlowDate(key), Props: props})
		}
		sort.Slice(l.Steps, func(i, j int) bool { return l.Steps[i].From.Before(l.Steps[j].From) })
		if p.HighID != nil {
			l.FromMeasure = append(l.FromMeasure, fmt.Sprintf("high from measure %d", *p.HighID))
		}
		if p.LowID != nil {
			l.FromMeasure = append(l.FromMeasure, fmt.Sprintf("low from measure %d", *p.LowID))
		}
		if p.TargetID != nil {
			l.FromMeasure = append(l.FromMeasure, fmt.Sprintf("target from measure %d", *p.TargetID))
		}
		if l.High == nil && l.Low == nil && l.Target == nil && len(l.FromMeasure) == 0 {
			continue
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// latestLimit picks the setting that applies now: the one with the latest
// date. Flow writes the date in the server's own format, so a key that
// cannot be read sorts first rather than being dropped.
func latestLimit(dated map[string]limitProps) (limitProps, bool) {
	var best limitProps
	var bestAt time.Time
	found := false
	for key, p := range dated {
		at := parseFlowDate(key)
		if !found || at.After(bestAt) {
			best, bestAt, found = p, at, true
		}
	}
	return best, found
}

var flowDateLayouts = []string{
	"2006/01/02 15:04:05", "2006-01-02 15:04:05", "1/2/2006 3:04:05 PM", "2/1/2006 15:04:05",
	"02/01/2006 15:04:05", "01/02/2006 15:04:05", time.RFC3339,
}

func parseFlowDate(s string) time.Time {
	for _, layout := range flowDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// limitText describes a limit the way Flow's charts do, e.g. "Moonshot < 3".
func limitText(l measureLimit) string {
	num := func(v *float64) string { return strconv.FormatFloat(*v, 'f', -1, 64) }
	var parts []string
	switch {
	case l.Comparator == "<" && l.High != nil:
		parts = append(parts, "< "+num(l.High))
	case l.Comparator == ">" && l.Low != nil:
		parts = append(parts, "> "+num(l.Low))
	default:
		if l.Low != nil {
			parts = append(parts, "low "+num(l.Low))
		}
		if l.High != nil {
			parts = append(parts, "high "+num(l.High))
		}
	}
	if l.Target != nil && !sameValue(l.Target, l.High) && !sameValue(l.Target, l.Low) {
		parts = append(parts, "target "+num(l.Target))
	}
	parts = append(parts, l.FromMeasure...)
	return strings.TrimSpace(l.Name + " " + strings.Join(parts, ", "))
}

func sameValue(a, b *float64) bool { return a != nil && b != nil && *a == *b }

// at is the setting that applies at t, as Flow picks it for a period: the
// latest one dated on or before the period's start. Nil before the first.
func (l measureLimit) at(t time.Time) *limitProps {
	var found *limitProps
	for i := range l.Steps {
		if l.Steps[i].From.After(t) {
			break
		}
		found = &l.Steps[i].Props
	}
	return found
}

// sourceValues are the period values of measures that limits take their
// value from, by measure id and period start (Unix ms).
type sourceValues map[int]map[int64]float64

// limitLineFields draws a limit as dashed lines beside a series: one for
// each of high, target and low that has a value in any of the limit's
// settings. Each value is the setting in effect at the point's period
// start, so a limit changed on 27 October steps there. A value that comes
// from another measure is that measure's value for the same period: Flow
// does not return the limit's worked-out values, so this matches Flow only
// when the limit uses the same period (the field's description says so).
// Bounds are red, the target green.
func limitLineFields(l measureLimit, points []point, value *data.Field, interpolation string, sources sourceValues) []*data.Field {
	parts := []struct {
		name  string
		color string
		get   func(limitProps) (*float64, *int)
	}{
		{"high", limitBoundColor, func(p limitProps) (*float64, *int) { return p.High, p.HighID }},
		{"target", limitTargetColor, func(p limitProps) (*float64, *int) { return p.Target, p.TargetID }},
		{"low", limitBoundColor, func(p limitProps) (*float64, *int) { return p.Low, p.LowID }},
	}
	var out []*data.Field
	for _, part := range parts {
		vals := make([]*float64, len(points))
		drawn := false
		var from []int
		for i, pt := range points {
			at := pt.start
			if at.IsZero() {
				at = pt.t
			}
			props := l.at(at)
			if props == nil {
				continue
			}
			v, id := part.get(*props)
			if v == nil && id != nil {
				from = appendUnique(from, *id)
				if sv, ok := sources[*id][at.UnixMilli()]; ok {
					v = &sv
				}
			}
			if v != nil && !math.IsNaN(*v) {
				vals[i] = v
				drawn = true
			}
		}
		if !drawn {
			continue
		}
		label := l.Name + " " + part.name
		f := data.NewField(label, value.Labels, vals)
		var description string
		if len(from) > 0 {
			ids := make([]string, len(from))
			for i, id := range from {
				ids[i] = strconv.Itoa(id)
			}
			description = fmt.Sprintf("Taken from measure %s, using its value for the same period. Flow does not return this limit's own values, so this differs from what Flow evaluates if the limit is set to use other periods.", strings.Join(ids, ", "))
		}
		f.Config = &data.FieldConfig{
			Description:       description,
			DisplayNameFromDS: strings.TrimSpace(value.Config.DisplayNameFromDS + " · " + label),
			Unit:              value.Config.Unit,
			Color:             map[string]any{"mode": "fixed", "fixedColor": part.color},
			Custom: map[string]any{
				"lineInterpolation": interpolation,
				"lineStyle":         map[string]any{"fill": "dash", "dash": []int{10, 10}},
				"lineWidth":         2,
				"showPoints":        "never",
				"fillOpacity":       0,
			},
		}
		out = append(out, f)
	}
	return out
}

// applyLimits adds each measure's limits to its frame: a dashed line for
// each limit value, following its dated settings, and the limits as they
// apply now in the value's description. frames and series
// are in the same order. It returns notices for what could not be drawn.
func (d *Datasource) applyLimits(ctx context.Context, frames data.Frames, series []series, qm queryModel, tr backend.TimeRange) []string {
	if len(frames) != len(series) {
		return nil
	}
	var ids []int
	for _, s := range series {
		ids = appendUnique(ids, s.id)
	}
	measures, failed, err := d.client.MeasuresWithMetadata(ctx, flow.TreeModel, ids)
	if err != nil {
		return []string{"Could not read limits: " + err.Error()}
	}
	byID := make(map[int]flow.Measure, len(measures))
	for _, m := range measures {
		byID[m.ID] = m
	}
	context, err := d.limitContext(ctx, qm)
	if err != nil {
		return []string{"Could not read limits: " + err.Error()}
	}
	var notices []string
	if len(failed) > 0 {
		notices = append(notices, fmt.Sprintf("Flow could not return the limits of %d measure(s), so they are not drawn.", len(failed)))
	}
	// The measures that limit values come from, read once for all series.
	var sourceIDs []int
	for _, s := range series {
		m := byID[s.id]
		for _, l := range pickLimits(measureLimits(m, context, m.IsTag()), qm.LimitNames) {
			for _, st := range l.Steps {
				for _, id := range []*int{st.Props.HighID, st.Props.TargetID, st.Props.LowID} {
					if id != nil {
						sourceIDs = appendUnique(sourceIDs, *id)
					}
				}
			}
		}
	}
	sources, err := d.limitSources(ctx, sourceIDs, qm, tr)
	if err != nil {
		notices = append(notices, "Could not read the measures some limits take their values from: "+err.Error())
	}

	for i, s := range series {
		if s.superseded {
			// Limits are drawn once, on the measure's preferred values.
			continue
		}
		m := byID[s.id]
		limits := pickLimits(measureLimits(m, context, m.IsTag()), qm.LimitNames)
		if len(limits) == 0 {
			continue
		}
		frame := frames[i]
		value := frame.Fields[1]
		if value.Config == nil {
			value.Config = &data.FieldConfig{}
		}
		// A value at its period start holds until the next one; at the end
		// it held since the one before; whole event periods are drawn as
		// lines of their own.
		interpolation := "stepAfter"
		switch qm.TimeAt {
		case "end":
			interpolation = "stepBefore"
		case "period":
			interpolation = "linear"
		}
		added := false
		for _, l := range limits {
			fields := limitLineFields(l, s.points, value, interpolation, sources)
			frame.Fields = append(frame.Fields, fields...)
			added = added || len(fields) > 0
		}
		if added && frame.Meta != nil {
			// No longer a single value per time.
			frame.Meta.Type, frame.Meta.TypeVersion = "", data.FrameTypeVersion{}
		}
		texts := make([]string, len(limits))
		for j, l := range limits {
			texts[j] = limitText(l)
		}
		value.Config.Description = strings.TrimSpace(value.Config.Description + "\nLimits: " + strings.Join(texts, "; "))
	}
	return notices
}

// limitSources reads the period values of the measures that limits take
// their values from, in the query's calendar or event.
func (d *Datasource) limitSources(ctx context.Context, ids []int, qm queryModel, tr backend.TimeRange) (sourceValues, error) {
	out := sourceValues{}
	if len(ids) == 0 {
		return out, nil
	}
	req := flow.MeasureDataRequest{IDs: ids, From: tr.From, To: tr.To, Limit: periodLimit}
	if qm.Context == contextEvent && qm.EventID > 0 {
		req.EventID = qm.EventID
	} else {
		calendarID, err := d.calendarFor(ctx, qm.CalendarID)
		if err != nil {
			return out, err
		}
		req.CalendarID = calendarID
	}
	res, err := d.client.MeasureData(ctx, req)
	if flow.IsNoData(err) || (err == nil && res == nil) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	for _, s := range res.Values {
		for _, p := range s.Data {
			if p.Start == nil || p.Value == nil {
				continue
			}
			if out[s.ID] == nil {
				out[s.ID] = map[int64]float64{}
			}
			out[s.ID][p.Start.UnixMilli()] = *p.Value
		}
	}
	return out, nil
}

// limitContext is the calendar or event whose limits apply to the query,
// by name, as Flow keys limits.
func (d *Datasource) limitContext(ctx context.Context, qm queryModel) (string, error) {
	if qm.Context == contextEvent && qm.EventID > 0 {
		events, err := d.client.Events(ctx, flow.TreeModel, []int{qm.EventID})
		if err != nil || len(events) == 0 {
			return "", err
		}
		return events[0].Name, nil
	}
	calendarID, err := d.calendarFor(ctx, qm.CalendarID)
	if err != nil {
		return "", err
	}
	list, err := d.listCalendars(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range list {
		if c.ID == calendarID {
			return c.Name, nil
		}
	}
	return "", nil
}

// pickLimits keeps the named limits; no names keeps them all.
func pickLimits(limits []measureLimit, names []string) []measureLimit {
	if len(names) == 0 {
		return limits
	}
	var out []measureLimit
	for _, l := range limits {
		for _, n := range names {
			if strings.EqualFold(l.Name, n) {
				out = append(out, l)
				break
			}
		}
	}
	return out
}

// limitNamesCap bounds how many measures the editor's limit list reads.
const limitNamesCap = 500

// limitNames lists the limits the selected measures have in the query's
// calendar or event, for the editor's limit picker. Template items stand
// for their model instances.
func (d *Datasource) limitNames(ctx context.Context, qm queryModel) ([]string, error) {
	sel, err := d.resolve(ctx, qm.Items)
	if err != nil {
		return nil, err
	}
	ids := append(append([]int(nil), sel.periodIDs...), sel.tagIDs...)
	if len(sel.periodTemplateIDs)+len(sel.tagTemplateIDs) > 0 {
		all, err := d.modelMeasures(ctx)
		if err != nil {
			return nil, err
		}
		templates := map[int]bool{}
		for _, id := range append(append([]int(nil), sel.periodTemplateIDs...), sel.tagTemplateIDs...) {
			templates[id] = true
		}
		for id, m := range all {
			if m.Template != nil && templates[m.Template.ID] {
				ids = appendUnique(ids, id)
			}
		}
	}
	if len(ids) > limitNamesCap {
		ids = ids[:limitNamesCap]
	}
	names := []string{}
	if len(ids) == 0 {
		return names, nil
	}
	measures, _, err := d.client.MeasuresWithMetadata(ctx, flow.TreeModel, ids)
	if err != nil {
		return nil, err
	}
	context, err := d.limitContext(ctx, qm)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, m := range measures {
		for _, l := range measureLimits(m, context, m.IsTag()) {
			if !seen[strings.ToLower(l.Name)] {
				seen[strings.ToLower(l.Name)] = true
				names = append(names, l.Name)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}
