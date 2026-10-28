package plugin

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
)

// itemFolder stands for every measure in a folder and the folders below it.
const itemFolder = "folder"

// treeCache keeps each tree's folders and metrics (and, for templates, its
// measures) for cacheTTL, so a folder resolves in a few calls however many
// metrics it holds.
type treeCache struct {
	mu      sync.Mutex
	entries map[flow.Tree]treeLists
}

type treeLists struct {
	folders  []flow.Folder
	metrics  []flow.Metric
	measures []flow.Measure // template tree only; model measures are cached elsewhere
	at       time.Time
}

func (d *Datasource) treeLists(ctx context.Context, tree flow.Tree) (treeLists, error) {
	d.trees.mu.Lock()
	if e, ok := d.trees.entries[tree]; ok && time.Since(e.at) < cacheTTL {
		d.trees.mu.Unlock()
		return e, nil
	}
	d.trees.mu.Unlock()
	folders, err := d.client.Folders(ctx, tree)
	if err != nil {
		return treeLists{}, err
	}
	metrics, err := d.client.AllMetrics(ctx, tree)
	if err != nil {
		return treeLists{}, err
	}
	l := treeLists{folders: folders, metrics: metrics, at: time.Now()}
	if tree == flow.TreeTemplate {
		if l.measures, err = d.client.AllMeasuresWithFields(ctx, tree); err != nil {
			return treeLists{}, err
		}
	}
	d.trees.mu.Lock()
	if d.trees.entries == nil {
		d.trees.entries = map[flow.Tree]treeLists{}
	}
	d.trees.entries[tree] = l
	d.trees.mu.Unlock()
	return l, nil
}

// folderMeasures lists the measures of every metric in a folder and the
// folders below it, by id.
func (d *Datasource) folderMeasures(ctx context.Context, tree flow.Tree, folderID int) ([]flow.Measure, error) {
	l, err := d.treeLists(ctx, tree)
	if err != nil {
		return nil, err
	}
	below := map[int]bool{folderID: true}
	for grew := true; grew; {
		grew = false
		for _, f := range l.folders {
			if !below[f.ID] && f.Parent != nil && below[f.Parent.ID] {
				below[f.ID], grew = true, true
			}
		}
	}
	metrics := map[int]bool{}
	for _, m := range l.metrics {
		if m.Folder != nil && below[m.Folder.ID] {
			metrics[m.ID] = true
		}
	}
	all := l.measures
	if tree == flow.TreeModel {
		byID, err := d.modelMeasures(ctx)
		if err != nil {
			return nil, err
		}
		all = make([]flow.Measure, 0, len(byID))
		for _, m := range byID {
			all = append(all, m)
		}
	}
	var out []flow.Measure
	for _, m := range all {
		if m.Metric != nil && metrics[m.Metric.ID] {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// folderPaths names folders by their path, e.g. "Site/Line 1".
func folderPaths(folders []flow.Folder) map[int]string {
	byID := make(map[int]flow.Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}
	out := make(map[int]string, len(folders))
	for _, f := range folders {
		out[f.ID] = folderPath(byID, f.ID)
	}
	return out
}
