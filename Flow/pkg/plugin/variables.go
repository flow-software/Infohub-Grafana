package plugin

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// queryTypeVariable lists options for a dashboard variable.
const queryTypeVariable = "variable"

// Variable kinds.
const (
	// varAttributeNames: the model attributes the measures carry, e.g. Line
	// and Site, for a dashboard that lets people pick the attribute too.
	varAttributeNames = "attributeNames"
	// varAttributeValues: the values of a model attribute (VariableAttribute)
	// on measures, e.g. every "Line".
	varAttributeValues = "attributeValues"
	// varMeasures: measures as value = id, text = folder path; the instances
	// of picked template measures or metrics, a metric's measures, or with
	// nothing picked every model measure.
	varMeasures = "measures"
	// varEvents: events as value = id, text = folder path.
	varEvents = "events"
	// varEventValues: the values of an event attribute seen recently.
	varEventValues = "eventValues"
	// varEventPeriods: the periods of an event in the time range (batches),
	// newest first, as value = "<event id>:<start ms>:<end ms>" (no end
	// while running), text = the label attribute and the start.
	varEventPeriods = "eventPeriods"
	// varLimits: the names of the limits the picked measures have in the
	// query's calendar (or event), for a dashboard that lets people pick
	// which limits to draw.
	varLimits = "limits"
	// varCharts: the charts the plugin can show (time-based), as value = id,
	// text = information folder path.
	varCharts = "charts"
)

// variableOption is one entry of a variable's list.
type variableOption struct{ text, value string }

// runVariable answers a variable query with a frame of text and value
// fields, the shape Grafana reads variable options from. Every kind can be
// narrowed by AttributeFilter/AttributeValues, so variables can depend on
// each other: the measures where Line = $line.
func (d *Datasource) runVariable(ctx context.Context, qm queryModel, tr backend.TimeRange) (backend.DataResponse, error) {
	var opts []variableOption
	var err error
	switch qm.VariableKind {
	case varAttributeNames:
		var names []string
		names, err = d.attributeNamesResource(ctx, nil)
		for _, n := range names {
			opts = append(opts, variableOption{n, n})
		}
	case varAttributeValues:
		opts, err = d.variableAttributeValues(ctx, qm)
	case varMeasures, "":
		// No kind is the editor's first list, measures.
		opts, err = d.variableMeasures(ctx, qm)
	case varEvents:
		opts, err = d.variableEvents(ctx, qm)
	case varEventValues:
		opts, err = d.variableEventValues(ctx, qm)
	case varEventPeriods:
		opts, err = d.variableEventPeriods(ctx, qm, tr)
	case varCharts:
		opts, err = d.variableCharts(ctx)
	case varLimits:
		var names []string
		names, err = d.limitNames(ctx, qm)
		for _, n := range names {
			opts = append(opts, variableOption{n, n})
		}
	default:
		return backend.DataResponse{}, &badRequest{fmt.Sprintf("unknown variable kind %q", qm.VariableKind)}
	}
	if err != nil {
		return backend.DataResponse{}, err
	}
	texts, values := make([]string, len(opts)), make([]string, len(opts))
	for i, o := range opts {
		texts[i], values[i] = o.text, o.value
	}
	return backend.DataResponse{Frames: data.Frames{data.NewFrame("options",
		data.NewField("text", nil, texts),
		data.NewField("value", nil, values),
	)}}, nil
}

// scopeMeasures is the model measures the query's items stand for: a model
// measure itself, a metric's measures, and the model instances of template
// measures and metrics. No items is every model measure.
func (d *Datasource) scopeMeasures(ctx context.Context, items []queryItem) (map[int]flow.Measure, error) {
	all, err := d.modelMeasures(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return all, nil
	}
	modelIDs := map[int]bool{}
	templateIDs := map[int]bool{}
	for _, it := range items {
		switch {
		case it.Type == itemMeasure && it.Tree == flow.TreeTemplate:
			templateIDs[it.ID] = true
		case it.Type == itemMeasure:
			modelIDs[it.ID] = true
		case it.Type == itemMetric || it.Type == itemFolder:
			var ms []flow.Measure
			var err error
			if it.Type == itemFolder {
				ms, err = d.folderMeasures(ctx, it.Tree, it.ID)
			} else {
				ms, err = d.client.MetricMeasures(ctx, it.Tree, it.ID)
			}
			if err != nil {
				return nil, fmt.Errorf("measures of %s %s %d: %w", it.Tree, it.Type, it.ID, err)
			}
			for _, m := range ms {
				if it.Tree == flow.TreeTemplate {
					templateIDs[m.ID] = true
				} else {
					modelIDs[m.ID] = true
				}
			}
		case it.Type == itemChart:
			// Flow lists a chart's model and template measures together.
			ms, err := d.chartMeasures(ctx, it.ID)
			if err != nil {
				return nil, fmt.Errorf("measures of chart %d: %w", it.ID, err)
			}
			for _, m := range ms {
				if _, ok := all[m.ID]; ok {
					modelIDs[m.ID] = true
				} else {
					templateIDs[m.ID] = true
				}
			}
		}
	}
	out := map[int]flow.Measure{}
	for id, m := range all {
		if modelIDs[id] || (m.Template != nil && templateIDs[m.Template.ID]) {
			out[id] = m
		}
	}
	return out, nil
}

func (d *Datasource) variableAttributeValues(ctx context.Context, qm queryModel) ([]variableOption, error) {
	if qm.VariableAttribute == "" {
		return nil, &badRequest{"pick the model attribute to list"}
	}
	measures, err := d.scopeMeasures(ctx, qm.Items)
	if err != nil {
		return nil, err
	}
	kept := map[int]flow.Measure{}
	for id, m := range measures {
		if attributeMatch(m, qm.AttributeFilter, qm.AttributeValues) {
			kept[id] = m
		}
	}
	var out []variableOption
	for _, v := range attributeValues(kept, qm.VariableAttribute) {
		out = append(out, variableOption{v, v})
	}
	return out, nil
}

func (d *Datasource) variableMeasures(ctx context.Context, qm queryModel) ([]variableOption, error) {
	// No items lists every model measure, for dashboards that work on any model.
	measures, err := d.scopeMeasures(ctx, qm.Items)
	if err != nil {
		return nil, err
	}
	var ids []int
	for id, m := range measures {
		if attributeMatch(m, qm.AttributeFilter, qm.AttributeValues) {
			ids = append(ids, id)
		}
	}
	infos, err := d.index.describe(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]variableOption, 0, len(ids))
	for _, id := range ids {
		text := infos[id].Path
		if text == "" {
			text = fmt.Sprintf("measure %d", id)
		}
		out = append(out, variableOption{text, strconv.Itoa(id)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].text) < strings.ToLower(out[j].text) })
	return out, nil
}

func (d *Datasource) variableEvents(ctx context.Context, qm queryModel) ([]variableOption, error) {
	events, err := d.client.AllEvents(ctx, flow.TreeModel)
	if err != nil {
		return nil, err
	}
	// Picked events narrow the list: a model event to itself, a template
	// event to every event created from it.
	picked, templates := map[int]bool{}, map[int]bool{}
	for _, it := range qm.Items {
		if it.Type != itemEvent {
			continue
		}
		if it.Tree == flow.TreeTemplate {
			templates[it.ID] = true
		} else {
			picked[it.ID] = true
		}
	}
	if len(picked)+len(templates) > 0 {
		kept := events[:0]
		for _, e := range events {
			if picked[e.ID] || (e.Template != nil && templates[e.Template.ID]) {
				kept = append(kept, e)
			}
		}
		events = kept
	}
	paths, err := d.index.eventPaths(ctx, events)
	if err != nil {
		return nil, err
	}
	out := make([]variableOption, 0, len(events))
	for _, e := range events {
		text := paths[e.ID]
		if text == "" {
			text = e.Name
		}
		out = append(out, variableOption{text, strconv.Itoa(e.ID)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].text) < strings.ToLower(out[j].text) })
	return out, nil
}

// variableCharts lists every time-based chart in Flow's information
// folders, the charts a chart query can show.
func (d *Datasource) variableCharts(ctx context.Context) ([]variableOption, error) {
	folders, err := d.client.InfoFolders(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]flow.InfoFolder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}
	path := func(id int) string {
		var parts []string
		for seen := map[int]bool{}; id != 0 && !seen[id]; {
			seen[id] = true
			f, ok := byID[id]
			if !ok {
				break
			}
			parts = append([]string{f.Name}, parts...)
			id = refID(f.Parent)
		}
		return strings.Join(parts, "/")
	}
	var out []variableOption
	for _, f := range folders {
		charts, err := d.client.FolderCharts(ctx, f.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range charts {
			if strings.HasPrefix(strings.ToLower(c.Type), "time based") {
				out = append(out, variableOption{strings.Trim(path(f.ID)+"/"+c.Name, "/"), strconv.Itoa(c.ID)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].text) < strings.ToLower(out[j].text) })
	return out, nil
}

func (d *Datasource) variableEventValues(ctx context.Context, qm queryModel) ([]variableOption, error) {
	// The picked event, which may be a template event (then the values of
	// every event created from it); older variables only carry its id.
	tree, id := flow.TreeModel, qm.EventID
	for _, it := range qm.Items {
		if it.Type == itemEvent {
			tree, id = it.Tree, it.ID
			break
		}
	}
	if id == 0 || qm.EventAttribute == "" {
		return nil, &badRequest{"pick the event and the attribute whose values to list"}
	}
	values, err := d.eventValues(ctx, tree, id, qm.EventAttribute)
	if err != nil {
		return nil, err
	}
	out := make([]variableOption, len(values))
	for i, v := range values {
		out[i] = variableOption{v.Value, v.Value}
	}
	return out, nil
}

// variableEventPeriods lists the periods of the picked events (or the
// event variable's event) in the time range, newest first: the batches a
// dashboard can pick from. EventAttribute with EventValues keeps only
// periods with one of those values, e.g. Product = $product.
func (d *Datasource) variableEventPeriods(ctx context.Context, qm queryModel, tr backend.TimeRange) ([]variableOption, error) {
	if qm.PeriodDays > 0 {
		now := time.Now()
		tr = backend.TimeRange{From: now.AddDate(0, 0, -qm.PeriodDays), To: now}
	}
	req := flow.EventDataRequest{From: tr.From, To: tr.To, Limit: eventPeriodLimit}
	for _, it := range qm.Items {
		if it.Type != itemEvent {
			continue
		}
		if it.Tree == flow.TreeTemplate {
			req.TemplateIDs = appendUnique(req.TemplateIDs, it.ID)
		} else {
			req.IDs = appendUnique(req.IDs, it.ID)
		}
	}
	if len(req.IDs)+len(req.TemplateIDs) == 0 && qm.EventID > 0 {
		req.IDs = []int{qm.EventID}
	}
	if len(req.IDs)+len(req.TemplateIDs) == 0 {
		return nil, &badRequest{"pick the event whose periods to list"}
	}
	res, err := d.client.EventData(ctx, req)
	if flow.IsNoData(err) || (err == nil && res == nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names map[int]string
	if len(res.Values) > 1 {
		// Several events: name each period's event.
		var ids []int
		for _, s := range res.Values {
			ids = append(ids, s.ID)
		}
		events, err := d.client.Events(ctx, flow.TreeModel, ids)
		if err == nil {
			paths, _ := d.index.eventPaths(ctx, events)
			names = map[int]string{}
			for _, e := range events {
				names[e.ID] = paths[e.ID]
				if names[e.ID] == "" {
					names[e.ID] = e.Name
				}
			}
		}
	}
	type period struct {
		start time.Time
		opt   variableOption
	}
	var periods []period
	for _, s := range res.Values {
		for _, p := range s.Data {
			if p.Start == nil {
				continue
			}
			if qm.EventAttribute != "" && len(qm.EventValues) > 0 {
				a, ok := attributeOf(p, qm.EventAttribute)
				if !ok || !containsFold(qm.EventValues, a.Value) {
					continue
				}
			}
			value := fmt.Sprintf("%d:%d:", s.ID, p.Start.UnixMilli())
			when := timeText(p.Start.Time)
			if p.End != nil {
				value += strconv.FormatInt(p.End.UnixMilli(), 10)
			} else {
				when += " (running)"
			}
			var parts []string
			if names != nil {
				parts = append(parts, names[s.ID])
			}
			if qm.PeriodLabel != "" {
				if a, ok := attributeOf(p, qm.PeriodLabel); ok && a.Value != "" {
					parts = append(parts, a.Value)
				}
			}
			parts = append(parts, when)
			periods = append(periods, period{p.Start.Time, variableOption{strings.Join(parts, " · "), value}})
		}
	}
	sort.SliceStable(periods, func(i, j int) bool { return periods[i].start.After(periods[j].start) })
	out := make([]variableOption, len(periods))
	for i, p := range periods {
		out[i] = p.opt
	}
	return out, nil
}

// attributeNamesResource lists the model attribute names measures carry,
// for the editors' attribute pickers.
func (d *Datasource) attributeNamesResource(ctx context.Context, items []queryItem) ([]string, error) {
	// Only the attributes the selected measures carry; all with none picked.
	measures, err := d.scopeMeasures(ctx, items)
	if err != nil {
		return nil, err
	}
	return attributeNames(measures), nil
}

// attributeValuesResource lists the values of one model attribute, for the
// editors' value pickers.
func (d *Datasource) attributeValuesResource(ctx context.Context, name string, items []queryItem) ([]string, error) {
	if name == "" {
		return nil, &badRequest{"attribute is required"}
	}
	// Only the values among the selected measures; all with none picked.
	measures, err := d.scopeMeasures(ctx, items)
	if err != nil {
		return nil, err
	}
	values := attributeValues(measures, name)
	if values == nil {
		values = []string{}
	}
	return values, nil
}
