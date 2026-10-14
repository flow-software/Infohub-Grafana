package plugin

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
func (d *Datasource) runVariable(ctx context.Context, qm queryModel) (backend.DataResponse, error) {
	var opts []variableOption
	var err error
	switch qm.VariableKind {
	case varAttributeNames:
		var names []string
		names, err = d.attributeNamesResource(ctx)
		for _, n := range names {
			opts = append(opts, variableOption{n, n})
		}
	case varAttributeValues:
		opts, err = d.variableAttributeValues(ctx, qm)
	case varMeasures:
		opts, err = d.variableMeasures(ctx, qm)
	case varEvents:
		opts, err = d.variableEvents(ctx, qm)
	case varEventValues:
		opts, err = d.variableEventValues(ctx, qm)
	case varCharts:
		opts, err = d.variableCharts(ctx)
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
		case it.Type == itemMetric:
			ms, err := d.client.MetricMeasures(ctx, it.Tree, it.ID)
			if err != nil {
				return nil, fmt.Errorf("measures of %s metric %d: %w", it.Tree, it.ID, err)
			}
			for _, m := range ms {
				if it.Tree == flow.TreeTemplate {
					templateIDs[m.ID] = true
				} else {
					modelIDs[m.ID] = true
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
	// Picked template events narrow the list to their model instances.
	templates := map[int]bool{}
	for _, it := range qm.Items {
		if it.Type == itemEvent && it.Tree == flow.TreeTemplate {
			templates[it.ID] = true
		}
	}
	if len(templates) > 0 {
		kept := events[:0]
		for _, e := range events {
			if e.Template != nil && templates[e.Template.ID] {
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
	if qm.EventID == 0 || qm.EventAttribute == "" {
		return nil, &badRequest{"pick the event and the attribute whose values to list"}
	}
	values, err := d.eventValues(ctx, qm.EventID, qm.EventAttribute)
	if err != nil {
		return nil, err
	}
	out := make([]variableOption, len(values))
	for i, v := range values {
		out[i] = variableOption{v.Value, v.Value}
	}
	return out, nil
}

// attributeNamesResource lists the model attribute names measures carry,
// for the editors' attribute pickers.
func (d *Datasource) attributeNamesResource(ctx context.Context) ([]string, error) {
	measures, err := d.modelMeasures(ctx)
	if err != nil {
		return nil, err
	}
	return attributeNames(measures), nil
}

// attributeValuesResource lists the values of one model attribute, for the
// editors' value pickers.
func (d *Datasource) attributeValuesResource(ctx context.Context, name string) ([]string, error) {
	if name == "" {
		return nil, &badRequest{"attribute is required"}
	}
	measures, err := d.modelMeasures(ctx)
	if err != nil {
		return nil, err
	}
	values := attributeValues(measures, name)
	if values == nil {
		values = []string{}
	}
	return values, nil
}
