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
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// limitsLines draws each measure's limits as dashed threshold lines.
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

// limitThresholds turns limits into Grafana thresholds: a dashed line at
// every constant bound (red) and target (green). A value that is both a
// bound and a target is drawn as a bound.
func limitThresholds(limits []measureLimit) *data.ThresholdsConfig {
	colors := map[float64]string{}
	set := func(v *float64, color string) {
		if v == nil || math.IsNaN(*v) {
			return
		}
		if colors[*v] != limitBoundColor {
			colors[*v] = color
		}
	}
	for _, l := range limits {
		set(l.Target, limitTargetColor)
		set(l.High, limitBoundColor)
		set(l.Low, limitBoundColor)
	}
	if len(colors) == 0 {
		return nil
	}
	values := make([]float64, 0, len(colors))
	for v := range colors {
		values = append(values, v)
	}
	sort.Float64s(values)
	// No base step: JSON cannot carry its -Infinity, so it would arrive as
	// null, and Grafana puts its own base in front and draws a null step as
	// a line at 0. Grafana's base is not drawn.
	steps := make([]data.Threshold, 0, len(values))
	for _, v := range values {
		steps = append(steps, data.Threshold{Value: data.ConfFloat64(v), Color: colors[v]})
	}
	return &data.ThresholdsConfig{Mode: data.ThresholdsModeAbsolute, Steps: steps}
}

// applyLimits adds each measure's limits to its frame: threshold lines on
// the value field, and the limits in its description. frames and series
// are in the same order. It returns notices for what could not be drawn.
func (d *Datasource) applyLimits(ctx context.Context, frames data.Frames, series []series, qm queryModel) []string {
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
	for i, s := range series {
		m := byID[s.id]
		limits := pickLimits(measureLimits(m, context, m.IsTag()), qm.LimitNames)
		if len(limits) == 0 {
			continue
		}
		value := frames[i].Fields[1]
		if value.Config == nil {
			value.Config = &data.FieldConfig{}
		}
		if t := limitThresholds(limits); t != nil {
			value.Config.Thresholds = t
			if value.Config.Custom == nil {
				value.Config.Custom = map[string]any{}
			}
			value.Config.Custom["thresholdsStyle"] = map[string]any{"mode": "dashed"}
		}
		texts := make([]string, len(limits))
		for j, l := range limits {
			texts[j] = limitText(l)
		}
		value.Config.Description = strings.TrimSpace(value.Config.Description + "\nLimits: " + strings.Join(texts, "; "))
	}
	return notices
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
