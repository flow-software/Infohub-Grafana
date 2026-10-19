package plugin

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
)

// chartNode is one row of the query editor's chart browser.
type chartNode struct {
	// Type is folder or chart.
	Type      string `json:"type"`
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ChartType string `json:"chartType,omitempty"`
	// Supported marks charts the plugin can turn into a table.
	Supported bool `json:"supported,omitempty"`
	order     int
}

// browseCharts lists the root chart folders, or a folder's folders and charts.
func (d *Datasource) browseCharts(ctx context.Context, folderID int) ([]chartNode, error) {
	nodes := []chartNode{}
	if folderID == 0 {
		folders, err := d.client.InfoFolders(ctx)
		if err != nil {
			return nil, err
		}
		for _, f := range folders {
			if f.Parent == nil {
				nodes = append(nodes, infoFolderNode(f))
			}
		}
	} else {
		folders, err := d.client.InfoChildFolders(ctx, folderID)
		if err != nil {
			return nil, err
		}
		charts, err := d.client.FolderCharts(ctx, folderID)
		if err != nil {
			return nil, err
		}
		for _, f := range folders {
			nodes = append(nodes, infoFolderNode(f))
		}
		for _, c := range charts {
			nodes = append(nodes, chartNode{
				Type: "chart", ID: c.ID, Name: c.Name, ChartType: c.Type,
				// The public API names types for people ("Time Based Table");
				// the definition's own type is checked again at query time.
				Supported: strings.HasPrefix(strings.ToLower(c.Type), "time based"),
			})
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.Type != b.Type {
			return a.Type == "folder"
		}
		if a.order != b.order {
			return a.order < b.order
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return nodes, nil
}

func infoFolderNode(f flow.InfoFolder) chartNode {
	n := chartNode{Type: "folder", ID: f.ID, Name: f.Name}
	if f.Order != nil {
		n.order = *f.Order
	}
	return n
}

// selectionSummary tells the query editor which option groups apply.
type selectionSummary struct {
	// PeriodMeasures and Tags count measures, with a template measure
	// counted once however many instances it has.
	PeriodMeasures int      `json:"periodMeasures"`
	Tags           int      `json:"tags"`
	Warnings       []string `json:"warnings,omitempty"`
	// Context is which calendars and events the period measures can be read
	// in, for the editor to offer only those; nil when it is not known (the
	// metadata could not be read, or a measure has none).
	Context *selectionContext `json:"context,omitempty"`
}

// selectionContext is the context of a selection's period measures.
type selectionContext struct {
	// Measures is how many period measures were looked at.
	Measures int `json:"measures"`
	// Calendars the measures have as context, with how many have each.
	Calendars []contextCount `json:"calendars"`
	// EventMeasures is how many model measures have an event as context;
	// template measures are not read in event context, so not counted.
	EventMeasures int `json:"eventMeasures"`
}

type contextCount struct {
	ID       int `json:"id"`
	Measures int `json:"measures"`
}

// contextCap bounds the measures whose context the summary reads: a metric
// can hold many, and the first ones tell the editor enough.
const contextCap = 200

// summary resolves items given as "<tree>:<type>:<id>", the editor's keys.
func (d *Datasource) summary(ctx context.Context, keys []string) (*selectionSummary, error) {
	items, err := parseItems(keys)
	if err != nil {
		return nil, err
	}
	sel, err := d.resolve(ctx, items)
	if err != nil {
		return nil, err
	}
	return &selectionSummary{
		PeriodMeasures: len(sel.periodIDs) + len(sel.periodTemplateIDs),
		Tags:           len(sel.tagIDs) + len(sel.tagTemplateIDs),
		Warnings:       sel.warnings,
		Context:        d.selectionContext(ctx, sel),
	}, nil
}

// selectionContext reads which calendars and events the period measures
// have as context. Anything it cannot tell for certain makes it return nil,
// so the editor offers everything rather than hide a choice that works.
func (d *Datasource) selectionContext(ctx context.Context, sel *selection) *selectionContext {
	out := &selectionContext{Calendars: []contextCount{}}
	calendars := map[int]int{}
	var order []int
	for _, part := range []struct {
		tree flow.Tree
		ids  []int
	}{{flow.TreeModel, sel.periodIDs}, {flow.TreeTemplate, sel.periodTemplateIDs}} {
		ids := part.ids
		if len(ids) == 0 {
			continue
		}
		if len(ids) > contextCap {
			ids = ids[:contextCap]
		}
		measures, failed, err := d.client.MeasuresWithMetadata(ctx, part.tree, ids)
		if err != nil || len(failed) > 0 {
			return nil
		}
		for _, m := range measures {
			cals, events, ok := measureContext(m)
			if !ok {
				return nil
			}
			out.Measures++
			for _, id := range cals {
				if calendars[id] == 0 {
					order = append(order, id)
				}
				calendars[id]++
			}
			if len(events) > 0 && part.tree == flow.TreeModel {
				out.EventMeasures++
			}
		}
	}
	for _, id := range order {
		out.Calendars = append(out.Calendars, contextCount{ID: id, Measures: calendars[id]})
	}
	return out
}

// measureContext reads a measure's context metadata: the calendars and
// events it can be read in. ok is false when it has none to read.
func measureContext(m flow.Measure) (calendars, events []int, ok bool) {
	for _, md := range m.Metadata {
		if strings.EqualFold(md.Type, "context") {
			_ = json.Unmarshal(md.Data["calendars"], &calendars)
			_ = json.Unmarshal(md.Data["events"], &events)
			return calendars, events, true
		}
	}
	return nil, nil, false
}
