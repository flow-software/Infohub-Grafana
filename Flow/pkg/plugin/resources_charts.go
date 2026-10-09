package plugin

import (
	"context"
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
}

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
	}, nil
}
