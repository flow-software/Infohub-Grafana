package plugin

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
)

// seriesInfo names one model measure for legends and labels. Instances of a
// template usually share their metric and measure names ("Production.Good
// Count" on every line), so the folder path is what tells them apart.
type seriesInfo struct {
	ID          int
	Path        string // Site/Area/Line 1/Production/Good Count
	Folder      string // Site/Area/Line 1
	Metric      string // Production
	Measure     string // Good Count (nested measures keep their dots)
	UOM         string
	Description string
	TemplateID  int
}

// modelIndex caches the model objects needed to name series. Folders are
// loaded whole (one call); metrics and measures are looked up by id as
// series appear. Everything is dropped after ttl so renames show up.
type modelIndex struct {
	client *flow.Client
	ttl    time.Duration
	now    func() time.Time

	mu       sync.Mutex
	loadedAt time.Time
	folders  map[int]flow.Folder
	metrics  map[int]flow.Metric
	measures map[int]flow.Measure
}

func newModelIndex(c *flow.Client, ttl time.Duration) *modelIndex {
	return &modelIndex{client: c, ttl: ttl, now: time.Now}
}

// expireLocked clears the cache once it is older than ttl.
func (x *modelIndex) expireLocked() {
	if x.measures != nil && x.now().Sub(x.loadedAt) < x.ttl {
		return
	}
	x.loadedAt = x.now()
	x.folders, x.metrics, x.measures = nil, map[int]flow.Metric{}, map[int]flow.Measure{}
}

// describe names the given model measure ids. Ids Flow no longer knows are
// named by id alone rather than failing the query.
//
// The answer is built from local maps; the shared cache only saves calls.
// Network calls run without the lock so concurrent panels are not serialised
// behind each other, and a cache reset by another query mid-way cannot lose
// anything this one fetched.
func (x *modelIndex) describe(ctx context.Context, ids []int) (map[int]seriesInfo, error) {
	measures := map[int]flow.Measure{}
	metrics := map[int]flow.Metric{}
	var folders map[int]flow.Folder

	x.mu.Lock()
	x.expireLocked()
	var missing []int
	for _, id := range ids {
		if m, ok := x.measures[id]; ok {
			measures[id] = m
		} else {
			missing = append(missing, id)
		}
	}
	x.mu.Unlock()

	if len(missing) > 0 {
		found, err := x.client.Measures(ctx, flow.TreeModel, missing)
		if err != nil {
			return nil, err
		}
		x.wholeNames(ctx, found)
		x.mu.Lock()
		for _, m := range found {
			measures[m.ID] = m
			x.measures[m.ID] = m
		}
		x.mu.Unlock()
	}

	x.mu.Lock()
	var missingMetrics []int
	for _, m := range measures {
		id := refID(m.Metric)
		if id == 0 {
			continue
		}
		if _, done := metrics[id]; done {
			continue
		}
		if c, ok := x.metrics[id]; ok {
			metrics[id] = c
		} else if !contains(missingMetrics, id) {
			missingMetrics = append(missingMetrics, id)
		}
	}
	folders = x.folders
	x.mu.Unlock()

	if len(missingMetrics) > 0 {
		found, err := x.client.Metrics(ctx, flow.TreeModel, missingMetrics)
		if err != nil {
			return nil, err
		}
		x.mu.Lock()
		for _, m := range found {
			metrics[m.ID] = m
			x.metrics[m.ID] = m
		}
		x.mu.Unlock()
	}

	// Reload folders when there are none yet, or a metric sits in a folder
	// created since the last load.
	needFolders := folders == nil
	for _, m := range metrics {
		if id := refID(m.Folder); id != 0 && folders != nil {
			if _, ok := folders[id]; !ok {
				needFolders = true
			}
		}
	}
	if needFolders && len(metrics) > 0 {
		list, err := x.client.Folders(ctx, flow.TreeModel)
		if err != nil {
			return nil, err
		}
		folders = make(map[int]flow.Folder, len(list))
		for _, f := range list {
			folders[f.ID] = f
		}
		x.mu.Lock()
		x.folders = folders
		x.mu.Unlock()
	}

	out := make(map[int]seriesInfo, len(ids))
	for _, id := range ids {
		out[id] = describeOne(id, measures, metrics, folders)
	}
	return out, nil
}

// eventPaths names events by folder path, "Site/Line 1/Trigger2", so
// instances of one template event stay apart.
func (x *modelIndex) eventPaths(ctx context.Context, events []flow.Event) (map[int]string, error) {
	x.mu.Lock()
	x.expireLocked()
	folders := x.folders
	x.mu.Unlock()
	if folders == nil {
		list, err := x.client.Folders(ctx, flow.TreeModel)
		if err != nil {
			return nil, err
		}
		folders = make(map[int]flow.Folder, len(list))
		for _, f := range list {
			folders[f.ID] = f
		}
		x.mu.Lock()
		x.folders = folders
		x.mu.Unlock()
	}
	out := make(map[int]string, len(events))
	for _, e := range events {
		out[e.ID] = joinPath(folderPath(folders, refID(e.Folder)), e.Name)
	}
	return out, nil
}

// wholeNames fixes the names of nested measures. Flow builds a measure's
// name by walking its parents, but only as far as the parents it loaded for
// that request, so a measure fetched by id two levels down comes back as
// "Metric.Parent.WTD" rather than "Metric.Grandparent.Parent.WTD". Its
// metric's measure list loads every measure of the metric together, so the
// names there are whole. If that list cannot be read, the names stand.
func (x *modelIndex) wholeNames(ctx context.Context, measures []flow.Measure) {
	var metricIDs []int
	for _, m := range measures {
		if m.Parent != nil && refID(m.Metric) != 0 && !contains(metricIDs, refID(m.Metric)) {
			metricIDs = append(metricIDs, refID(m.Metric))
		}
	}
	whole := map[int]string{}
	for _, id := range metricIDs {
		list, err := x.client.MetricMeasures(ctx, flow.TreeModel, id)
		if err != nil {
			continue
		}
		for _, m := range list {
			whole[m.ID] = m.Name
		}
	}
	for i, m := range measures {
		if name, ok := whole[m.ID]; ok && m.Parent != nil {
			measures[i].Name = name
		}
	}
}

func describeOne(id int, measures map[int]flow.Measure, metrics map[int]flow.Metric, folders map[int]flow.Folder) seriesInfo {
	m, ok := measures[id]
	if !ok {
		name := "measure " + strconv.Itoa(id)
		return seriesInfo{ID: id, Path: name, Measure: name}
	}
	info := seriesInfo{ID: id, UOM: m.UOM, Description: m.Description, Measure: m.Name, Path: m.Name}
	if m.Template != nil {
		info.TemplateID = m.Template.ID
	}
	metric, ok := metrics[refID(m.Metric)]
	if !ok {
		return info
	}
	info.Metric = metric.Name
	info.Measure = strings.TrimPrefix(m.Name, metric.Name+".")
	info.Folder = folderPath(folders, refID(metric.Folder))
	info.Path = joinPath(info.Folder, info.Metric, info.Measure)
	return info
}

// folderPath walks parent links up to the root. A loop in the data is cut
// rather than followed forever.
func folderPath(folders map[int]flow.Folder, id int) string {
	var names []string
	seen := map[int]bool{}
	for id != 0 && !seen[id] {
		seen[id] = true
		f, ok := folders[id]
		if !ok {
			break
		}
		names = append(names, f.Name)
		id = refID(f.Parent)
	}
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	return strings.Join(names, "/")
}

func contains(ids []int, id int) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func refID(r *flow.Ref) int {
	if r == nil {
		return 0
	}
	return r.ID
}

func joinPath(parts ...string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "/")
}
