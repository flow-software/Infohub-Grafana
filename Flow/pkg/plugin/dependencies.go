package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Dependency query directions.
const (
	depInputs = "inputs" // what a measure is built from (Flow's "dependents")
	depUsedBy = "usedBy" // what is built from a measure
)

// depNamesMeasure titles nodes by the measure's own name.
const depNamesMeasure = "measure"

const (
	depDefaultDepth = 10
	// depMaxRows caps tree rows: a measure used in many places appears once
	// per place, so trees can grow faster than the model.
	depMaxRows = 5000
)

// depLink is one input of a measure, as its metadata describes it.
type depLink struct {
	Kind       string // calculation, aggregation, set to measure, retrieval
	SourceType string // measure or event
	SourceID   int
	// Detail is the formula, or the aggregation, e.g. "sum, weekly".
	Detail string
}

// depLinks reads a measure's inputs from its metadata.
func depLinks(m flow.Measure) []depLink {
	var out []depLink
	for _, md := range m.Metadata {
		switch strings.ToLower(md.Type) {
		case "calculation":
			var ids []int
			_ = json.Unmarshal(md.Data["dependents"], &ids)
			formula := latestFormula(md.Data)
			for _, id := range ids {
				out = append(out, depLink{Kind: "calculation", SourceType: "measure", SourceID: id, Detail: formula})
			}
		case "aggregation":
			var aggType, interval string
			_ = json.Unmarshal(md.Data["type"], &aggType)
			_ = json.Unmarshal(md.Data["interval"], &interval)
			detail := strings.Trim(aggType+", "+interval, ", ")
			var id int
			if json.Unmarshal(md.Data["measure"], &id) == nil && id != 0 {
				out = append(out, depLink{Kind: "aggregation", SourceType: "measure", SourceID: id, Detail: detail})
			} else if json.Unmarshal(md.Data["event"], &id) == nil && id != 0 {
				out = append(out, depLink{Kind: "aggregation", SourceType: "event", SourceID: id, Detail: detail})
			}
		case "manual":
			var t string
			var id int
			_ = json.Unmarshal(md.Data["type"], &t)
			if strings.EqualFold(t, "SetToMeasure") && json.Unmarshal(md.Data["id"], &id) == nil && id != 0 {
				out = append(out, depLink{Kind: "set to measure", SourceType: "measure", SourceID: id})
			}
		case "retrieval":
			// A measure retrieved from a Flow tag reads another measure:
			// the tag (a timestamp measure) holds the connection.
			var source string
			var id int
			_ = json.Unmarshal(md.Data["source"], &source)
			if strings.EqualFold(source, "Tag") && json.Unmarshal(md.Data["id"], &id) == nil && id != 0 {
				out = append(out, depLink{Kind: "retrieval", SourceType: "measure", SourceID: id, Detail: metaString(md.Data, "aggregation")})
			}
		}
	}
	return out
}

// depInfo is how a measure gets its values, as its node's details show it.
type depInfo struct {
	Calculation string // the formula in effect now
	Aggregation string // e.g. "sum, weekly"
	// Source is the connection a retrieved measure reads, e.g.
	// "Flow Data Simulator · 110-FQ-001.PV · average".
	Source string
	// TagID is the tag measure a measure retrieved from a Flow tag reads,
	// and TagAggregation how it reads it; the tag's node shows its source.
	TagID          int
	TagAggregation string
}

func depInfoOf(m flow.Measure) depInfo {
	var info depInfo
	var sources []string
	for _, md := range m.Metadata {
		switch strings.ToLower(md.Type) {
		case "calculation":
			info.Calculation = latestFormula(md.Data)
		case "aggregation":
			info.Aggregation = strings.Trim(metaString(md.Data, "type")+", "+metaString(md.Data, "interval"), ", ")
		case "retrieval":
			source := metaString(md.Data, "source")
			if strings.EqualFold(source, "Tag") {
				_ = json.Unmarshal(md.Data["id"], &info.TagID)
				info.TagAggregation = metaString(md.Data, "aggregation")
				continue
			}
			parts := []string{source}
			for _, k := range []string{"tag", "aggregation"} {
				if v := metaString(md.Data, k); v != "" {
					parts = append(parts, v)
				}
			}
			sources = append(sources, strings.Join(parts, " · "))
		}
	}
	info.Source = strings.Join(sources, "; ")
	return info
}

// metaString reads a metadata value that Flow may send as a string or a number.
func metaString(d map[string]json.RawMessage, key string) string {
	raw, ok := d[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	if string(raw) == "null" {
		return ""
	}
	return string(raw)
}

// latestFormula picks the formula in effect now: calculation metadata keys
// each formula by the date it applies from ("1970/01/01 00:00:00").
func latestFormula(d map[string]json.RawMessage) string {
	var best, formula string
	for k, v := range d {
		if k == "dependents" {
			continue
		}
		var f string
		if json.Unmarshal(v, &f) != nil {
			continue
		}
		if t, err := time.Parse("2006/01/02 15:04:05", k); err == nil {
			key := t.Format(time.RFC3339)
			if key > best {
				best, formula = key, f
			}
		} else if best == "" {
			formula = f
		}
	}
	return formula
}

// depGraph is the part of the model a dependency query walked.
type depGraph struct {
	tree     flow.Tree
	measures map[int]flow.Measure
	// failed are measures Flow could not return metadata for.
	failed map[int]bool
	events map[int]string
	// usedBy[source] lists the measures that read from source.
	usedBy map[int][]depEdge
}

type depEdge struct {
	measureID int
	link      depLink
}

// depIndex caches the whole model's links per tree for "used by" queries,
// which need every measure's metadata to look backwards.
type depIndex struct {
	mu      sync.Mutex
	entries map[flow.Tree]depIndexEntry
}

type depIndexEntry struct {
	graph *depGraph
	at    time.Time
}

// loadMeasures fetches measures with metadata into g, skipping known ones.
func (d *Datasource) loadMeasures(ctx context.Context, g *depGraph, ids []int) error {
	var need []int
	for _, id := range ids {
		if _, ok := g.measures[id]; !ok && !g.failed[id] {
			need = append(need, id)
		}
	}
	if len(need) == 0 {
		return nil
	}
	found, failed, err := d.client.MeasuresWithMetadata(ctx, g.tree, need)
	if err != nil {
		return err
	}
	for _, m := range found {
		g.measures[m.ID] = m
	}
	for _, id := range failed {
		g.failed[id] = true
	}
	return nil
}

func newDepGraph(tree flow.Tree) *depGraph {
	return &depGraph{tree: tree, measures: map[int]flow.Measure{}, failed: map[int]bool{}, events: map[int]string{}}
}

// fullGraph loads every measure's links in the tree, cached for cacheTTL.
func (d *Datasource) fullGraph(ctx context.Context, tree flow.Tree) (*depGraph, error) {
	d.deps.mu.Lock()
	if e, ok := d.deps.entries[tree]; ok && time.Since(e.at) < cacheTTL {
		d.deps.mu.Unlock()
		return e.graph, nil
	}
	d.deps.mu.Unlock()

	ids, err := d.client.AllMeasureIDs(ctx, tree)
	if err != nil {
		return nil, err
	}
	g := newDepGraph(tree)
	if err := d.loadMeasures(ctx, g, ids); err != nil {
		return nil, err
	}
	g.usedBy = map[int][]depEdge{}
	for _, m := range g.measures {
		for _, l := range depLinks(m) {
			if l.SourceType == "measure" {
				g.usedBy[l.SourceID] = append(g.usedBy[l.SourceID], depEdge{measureID: m.ID, link: l})
			}
		}
	}
	for _, edges := range g.usedBy {
		sort.Slice(edges, func(i, j int) bool { return edges[i].measureID < edges[j].measureID })
	}

	d.deps.mu.Lock()
	if d.deps.entries == nil {
		d.deps.entries = map[flow.Tree]depIndexEntry{}
	}
	d.deps.entries[tree] = depIndexEntry{graph: g, at: time.Now()}
	d.deps.mu.Unlock()
	return g, nil
}

// depRow is one node of the dependency tree, in the order a depth-first
// walk meets it. A measure reached by two paths appears twice, as a tree
// view needs.
type depRow struct {
	ID        string // the path of measure ids from the root, e.g. "4282/4281"
	ParentID  string
	Depth     int
	Type      string // measure or event
	MeasureID int
	Relation  string // how this node relates to its parent
	Detail    string
	// Cycle marks a measure already on the path; its branch stops here.
	Cycle bool
	// Truncated marks a node whose children were cut by the depth limit.
	Truncated bool
}

func (d *Datasource) runDependencies(ctx context.Context, qm queryModel, q backend.DataQuery) (backend.DataResponse, error) {
	direction := qm.DepDirection
	if direction == "" {
		direction = depInputs
	}
	depth := qm.DepDepth
	if depth <= 0 {
		depth = depDefaultDepth
	}

	// Roots per tree: measures, and every measure of a selected metric.
	roots := map[flow.Tree][]int{}
	var notices []string
	for _, it := range qm.Items {
		switch it.Type {
		case itemMeasure:
			roots[it.Tree] = appendUnique(roots[it.Tree], it.ID)
		case itemMetric:
			ms, err := d.client.MetricMeasures(ctx, it.Tree, it.ID)
			if err != nil {
				return backend.DataResponse{}, fmt.Errorf("measures of %s metric %d: %w", it.Tree, it.ID, err)
			}
			for _, m := range ms {
				roots[it.Tree] = appendUnique(roots[it.Tree], m.ID)
			}
		}
	}

	var rows []depRow
	graphs := map[flow.Tree]*depGraph{}
	treeOf := map[string]flow.Tree{}
	for _, tree := range []flow.Tree{flow.TreeModel, flow.TreeTemplate} {
		ids := roots[tree]
		if len(ids) == 0 {
			continue
		}
		var g *depGraph
		var err error
		if direction == depUsedBy {
			g, err = d.fullGraph(ctx, tree)
		} else {
			g = newDepGraph(tree)
			err = d.walkInputs(ctx, g, ids, depth)
		}
		if err != nil {
			return backend.DataResponse{}, err
		}
		if err := d.loadEventNames(ctx, g); err != nil {
			notices = append(notices, "Could not look up event names: "+err.Error())
		}
		graphs[tree] = g
		prefix := ""
		if tree == flow.TreeTemplate {
			prefix = "t:"
		}
		for _, id := range ids {
			before := len(rows)
			rows = g.appendTree(rows, prefix, id, direction, depth)
			for _, r := range rows[before:] {
				treeOf[r.ID] = tree
			}
			if len(rows) >= depMaxRows {
				notices = append(notices, fmt.Sprintf("Stopped at %d nodes; lower the depth or pick fewer measures.", depMaxRows))
				break
			}
		}
		for id := range g.failed {
			notices = append(notices, fmt.Sprintf("Flow could not return how %s measure %d is built (a server error on its metadata), so its inputs are not shown.", tree, id))
		}
	}
	sort.Strings(notices)

	values := map[string]depValue{}
	if qm.DepValues != depValuesNone {
		var warn []string
		values, warn = d.depMeasureValues(ctx, graphs[flow.TreeModel], rows, treeOf, qm, q.TimeRange)
		notices = append(notices, warn...)
	}

	names, err := d.depNames(ctx, graphs, rows, treeOf)
	if err != nil {
		notices = append(notices, "Could not look up measure names: "+err.Error())
	}
	if err := d.depMetricNames(ctx, graphs, names); err != nil {
		notices = append(notices, "Could not read full measure names: "+err.Error())
	}

	frames := depNodeGraph(rows, names, values, treeOf, graphs, direction, qm.DepNames == depNamesMeasure, qm.DepTypeMark == depTypeSymbol, qm.Limits == limitsLines)
	if len(notices) > 0 {
		frames[0].Meta.Notices = append(frames[0].Meta.Notices, noticesOf(notices)...)
	}
	return backend.DataResponse{Frames: frames}, nil
}

func noticesOf(texts []string) []data.Notice {
	out := make([]data.Notice, len(texts))
	for i, t := range texts {
		out[i] = data.Notice{Severity: data.NoticeSeverityWarning, Text: t}
	}
	return out
}

// walkInputs loads the roots and their inputs, level by level, up to depth.
func (d *Datasource) walkInputs(ctx context.Context, g *depGraph, roots []int, depth int) error {
	frontier := roots
	for level := 0; level <= depth && len(frontier) > 0; level++ {
		if err := d.loadMeasures(ctx, g, frontier); err != nil {
			return err
		}
		var next []int
		for _, id := range frontier {
			for _, l := range depLinks(g.measures[id]) {
				if l.SourceType != "measure" {
					continue
				}
				if _, seen := g.measures[l.SourceID]; !seen && !g.failed[l.SourceID] {
					next = appendUnique(next, l.SourceID)
				}
			}
		}
		frontier = next
	}
	return nil
}

func (d *Datasource) loadEventNames(ctx context.Context, g *depGraph) error {
	var ids []int
	for _, m := range g.measures {
		for _, l := range depLinks(m) {
			if l.SourceType == "event" {
				ids = appendUnique(ids, l.SourceID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := d.client.EventSchemeNames(ctx, g.tree, ids)
	if err != nil {
		return err
	}
	for id, n := range names {
		g.events[id] = n
	}
	return nil
}

// appendTree walks from root depth-first, in the query's direction.
func (g *depGraph) appendTree(rows []depRow, prefix string, root int, direction string, maxDepth int) []depRow {
	var walk func(id int, rowID, parentID string, depth int, rel, detail string, onPath map[int]bool)
	walk = func(id int, rowID, parentID string, depth int, rel, detail string, onPath map[int]bool) {
		if len(rows) >= depMaxRows {
			return
		}
		row := depRow{ID: rowID, ParentID: parentID, Depth: depth, Type: "measure", MeasureID: id, Relation: rel, Detail: detail}
		if onPath[id] {
			row.Cycle = true
			rows = append(rows, row)
			return
		}
		var children []depEdge
		if direction == depUsedBy {
			children = g.usedBy[id]
		} else {
			for _, l := range depLinks(g.measures[id]) {
				children = append(children, depEdge{measureID: l.SourceID, link: l})
			}
		}
		if depth >= maxDepth && len(children) > 0 {
			row.Truncated = true
			children = nil
		}
		rows = append(rows, row)
		onPath[id] = true
		for _, c := range children {
			if c.link.SourceType == "event" && direction == depInputs {
				rows = append(rows, depRow{
					ID: rowID + "/e" + strconv.Itoa(c.link.SourceID), ParentID: rowID, Depth: depth + 1,
					Type: "event", MeasureID: c.link.SourceID, Relation: c.link.Kind, Detail: c.link.Detail,
				})
				continue
			}
			walk(c.measureID, rowID+"/"+strconv.Itoa(c.measureID), rowID, depth+1, c.link.Kind, c.link.Detail, onPath)
		}
		delete(onPath, id)
	}
	walk(root, prefix+strconv.Itoa(root), "", 0, "", "", map[int]bool{})
	return rows
}

// depName is how a node is labelled.
type depName struct {
	Name string // Metric.Measure
	// Own is the measure's own name, without its metric and parent
	// measures.
	Own  string
	Path string // folder path for model measures
	UOM  string
}

// depNames names every measure in the rows: model measures with their folder
// path from the series index, template measures by name.
func (d *Datasource) depNames(ctx context.Context, graphs map[flow.Tree]*depGraph, rows []depRow, treeOf map[string]flow.Tree) (map[string]depName, error) {
	var modelIDs []int
	for _, r := range rows {
		if r.Type == "measure" && treeOf[r.ID] == flow.TreeModel {
			modelIDs = appendUnique(modelIDs, r.MeasureID)
		}
	}
	var infos map[int]seriesInfo
	var err error
	if len(modelIDs) > 0 {
		infos, err = d.index.describe(ctx, modelIDs)
	}
	names := map[string]depName{}
	for _, r := range rows {
		tree := treeOf[r.ID]
		key := depKey(tree, r.Type, r.MeasureID)
		if _, done := names[key]; done {
			continue
		}
		g := graphs[tree]
		n := depName{}
		switch r.Type {
		case "event":
			n.Name = "Event: " + g.events[r.MeasureID]
			if g.events[r.MeasureID] == "" {
				n.Name = fmt.Sprintf("Event %d", r.MeasureID)
			}
		default:
			m, loaded := g.measures[r.MeasureID]
			n.Name = m.Name
			n.UOM = m.UOM
			if !loaded || n.Name == "" {
				n.Name = fmt.Sprintf("Measure %d", r.MeasureID)
			}
			if info, ok := infos[r.MeasureID]; ok && tree == flow.TreeModel {
				n.Path = info.Path
				if n.UOM == "" {
					n.UOM = info.UOM
				}
				// A measure whose metadata failed is still named by the index.
				if !loaded || m.Name == "" {
					n.Name = strings.Trim(info.Metric+"."+info.Measure, ".")
				}
			}
		}
		names[key] = n
	}
	return names, err
}

// depMetricNames names measures from their metric's measure list. Flow
// builds a name by walking parent measures, but only as far as the parents
// it loaded for that request, so measures fetched by id can come back as
// "Metric.Parent.WTD" one time and "Metric.Grandparent.Parent.WTD" the
// next. A metric's list loads all its measures together, so every name in
// it is whole. The list also gives each measure's own name: what is left
// after its parent's full name, or the metric's for a top-level measure
// (names can hold dots of their own, e.g. "110-FQ-001.PV").
func (d *Datasource) depMetricNames(ctx context.Context, graphs map[flow.Tree]*depGraph, names map[string]depName) error {
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for tree, g := range graphs {
		var metricIDs []int
		for _, m := range g.measures {
			if _, named := names[depKey(tree, "measure", m.ID)]; named && refID(m.Metric) != 0 {
				metricIDs = appendUnique(metricIDs, refID(m.Metric))
			}
		}
		if len(metricIDs) == 0 {
			continue
		}
		metricNames := map[int]string{}
		found, err := d.client.Metrics(ctx, tree, metricIDs)
		note(err)
		for _, m := range found {
			metricNames[m.ID] = m.Name
		}
		for _, metricID := range metricIDs {
			list, err := d.client.MetricMeasures(ctx, tree, metricID)
			if err != nil {
				note(err)
				continue
			}
			byID := make(map[int]flow.Measure, len(list))
			for _, m := range list {
				byID[m.ID] = m
			}
			for _, m := range list {
				key := depKey(tree, "measure", m.ID)
				n, ok := names[key]
				if !ok {
					continue
				}
				n.Name = m.Name
				prefix := metricNames[metricID]
				if id := refID(m.Parent); id != 0 {
					prefix = byID[id].Name
				}
				if prefix != "" && strings.HasPrefix(m.Name, prefix+".") {
					n.Own = strings.TrimPrefix(m.Name, prefix+".")
				}
				names[key] = n
			}
		}
	}
	return firstErr
}

func depKey(tree flow.Tree, typ string, id int) string {
	return string(tree) + ":" + typ + ":" + strconv.Itoa(id)
}

// Node colours by how a measure gets its values, for the Node Graph.
var retrievalColors = map[string]string{
	"calculated": "#5794F2",
	"aggregated": "#FF9830",
	"manual":     "#B877D9",
	"retrieved":  "#73BF69",
	"replicated": "#8AB8FF",
}

// nodeKinds are the ring colours of the Node Graph, in legend order: the
// retrieval type, or grey for events and measures Flow could not describe.
// A value outside its limits fills the node red instead (highlighted).
var nodeKinds = []struct{ key, label, color string }{
	{"calculated", "Calculated", retrievalColors["calculated"]},
	{"aggregated", "Aggregated", retrievalColors["aggregated"]},
	{"manual", "Manual", retrievalColors["manual"]},
	{"retrieved", "Retrieved", retrievalColors["retrieved"]},
	{"replicated", "Replicated", retrievalColors["replicated"]},
	{"other", "Other", "#8E8E8E"},
}

// typeSymbols mark the retrieval type under a node's value when the query
// asks for symbols instead of ring colours. Grafana's node icons cannot be
// used: a node with an icon shows the icon instead of its value.
var typeSymbols = map[string]string{
	"calculated": "ƒx",
	"aggregated": "Σ",
	"manual":     "✎",
	"retrieved":  "⇣",
	"replicated": "⇄",
}

// The legend entry explaining the red fill Grafana draws inside a
// highlighted node (an out-of-limit value).
const (
	redFillLegend = "Red fill = outside limits"
	redFillColor  = "#AA0000"
)

// depTypeSymbol is the query value that asks for symbols.
const depTypeSymbol = "symbol"

// depSource describes where a measure's values come from: its connection,
// or the Flow tag it reads, named like its node.
func depSource(info depInfo, tree flow.Tree, names map[string]depName) string {
	if info.TagID == 0 {
		return info.Source
	}
	tag := names[depKey(tree, "measure", info.TagID)].Name
	if tag == "" {
		tag = fmt.Sprintf("measure %d", info.TagID)
	}
	text := "Flow tag " + tag
	if info.TagAggregation != "" {
		text += " · " + strings.ToLower(info.TagAggregation)
	}
	return strings.Trim(text+"; "+info.Source, "; ")
}

// depNodeGraph lays the rows out for Grafana's Node Graph panel: each
// measure once (the walk's tree folded back into a graph), and an edge per
// link, pointing the way values flow. A node shows its latest value, with
// how the measure is built as its subtitle; the rest (path, formula, source,
// who entered it) is in the menu a click on the node opens.
func depNodeGraph(rows []depRow, names map[string]depName, values map[string]depValue, treeOf map[string]flow.Tree, graphs map[flow.Tree]*depGraph, direction string, ownNames, symbols, limits bool) data.Frames {
	type node struct {
		id, title, subtitle, mainstat, kind, measureID             string
		path, calculation, aggregation, source, enteredBy, valueAt string
		limits, note                                               string
		outside                                                    bool
	}
	var nodes []node
	seen := map[string]int{} // node key -> index in nodes
	nodeIDOf := map[string]string{}
	var edgeIDs, sources, targets, edgeStats, edgeDetails []string
	seenEdge := map[string]bool{}
	for _, r := range rows {
		tree := treeOf[r.ID]
		key := depKey(tree, r.Type, r.MeasureID)
		nodeIDOf[r.ID] = key
		if i, ok := seen[key]; ok {
			if r.Truncated && !strings.Contains(nodes[i].note, depTruncatedNote(direction)) {
				nodes[i].note = strings.TrimSpace(nodes[i].note + " " + depTruncatedNote(direction))
			}
		} else {
			seen[key] = len(nodes)
			nm := names[key]
			n := node{id: key, title: nm.Name, path: nm.Path, kind: "other"}
			if ownNames && nm.Own != "" {
				n.title = nm.Own
			}
			if r.Type == "measure" {
				n.measureID = strconv.Itoa(r.MeasureID)
				if tree == flow.TreeTemplate {
					n.measureID += " (template)"
				}
			}
			if m, ok := graphs[tree].measures[r.MeasureID]; ok && r.Type == "measure" {
				n.subtitle = depStats(m)
				info := depInfoOf(m)
				n.calculation, n.aggregation, n.source = info.Calculation, info.Aggregation, depSource(info, tree, names)
				if _, ok := retrievalColors[strings.ToLower(m.Retrieval)]; ok {
					n.kind = strings.ToLower(m.Retrieval)
				}
				if v, ok := values[key]; ok && v.Value != nil {
					n.mainstat = v.Text
					n.valueAt = v.Time.UTC().Format(time.RFC3339)
					if strings.EqualFold(m.Retrieval, "manual") {
						n.enteredBy = v.enteredBy()
					}
					if v.Outside != "" {
						n.outside, n.limits = true, "Outside: "+v.Outside
					}
				} else if ok {
					n.mainstat = "–"
					n.note = v.Note
				}
			}
			if r.Type == "event" {
				n.subtitle = "event"
			}
			if tree == flow.TreeTemplate {
				n.subtitle = strings.TrimSpace(n.subtitle + " (template)")
			}
			if r.Truncated {
				n.note = strings.TrimSpace(n.note + " " + depTruncatedNote(direction))
			}
			nodes = append(nodes, n)
		}
		if r.ParentID == "" {
			continue
		}
		parent := nodeIDOf[r.ParentID]
		// Edges point the way values flow: from an input to the measure
		// built from it. Walking inputs, a child is its parent's input;
		// walking "used by", a child is built from its parent.
		src, dst := key, parent
		if direction == depUsedBy {
			src, dst = parent, key
		}
		eid := src + ">" + dst
		if seenEdge[eid] {
			continue
		}
		seenEdge[eid] = true
		edgeIDs, sources, targets = append(edgeIDs, eid), append(sources, src), append(targets, dst)
		edgeStats, edgeDetails = append(edgeStats, r.Relation), append(edgeDetails, r.Detail)
	}

	col := func(get func(node) string) []string {
		out := make([]string, len(nodes))
		for i, n := range nodes {
			out[i] = get(n)
		}
		return out
	}
	detail := func(name, label string, get func(node) string) *data.Field {
		return detailField(name, label, col(get))
	}
	nodeFrame := data.NewFrame("nodes",
		data.NewField("id", nil, col(func(n node) string { return n.id })),
		data.NewField("title", nil, col(func(n node) string { return n.title })),
		data.NewField("subtitle", nil, col(func(n node) string { return n.subtitle })),
		latestValueField(col(func(n node) string { return n.mainstat })),
		detail("measureId", "Measure ID", func(n node) string { return n.measureID }),
		detail("path", "Path", func(n node) string { return n.path }),
		detail("calculation", "Calculation", func(n node) string { return n.calculation }),
		detail("aggregation", "Aggregation", func(n node) string { return n.aggregation }),
		detail("source", "Data source", func(n node) string { return n.source }),
		detail("enteredBy", "Entered by", func(n node) string { return n.enteredBy }),
		detail("valueTime", "Value at", func(n node) string { return n.valueAt }),
		detail("limits", "Limits", func(n node) string { return n.limits }),
		detail("note", "Note", func(n node) string { return n.note }),
	)
	// A value outside its limits fills the node: Grafana draws a highlighted
	// node dark red inside, so the ring can still show the retrieval type.
	highlighted := make([]bool, len(nodes))
	for i, n := range nodes {
		highlighted[i] = n.outside
	}
	nodeFrame.Fields = append(nodeFrame.Fields, data.NewField("highlighted", nil, highlighted))

	if symbols {
		// The type as a symbol under the value, and a plain grey ring. The
		// panel lists both fields in its legend, so the symbol field is
		// named as the key to the symbols in this graph.
		var key []string
		seenKind := map[string]bool{}
		for _, k := range nodeKinds {
			for _, n := range nodes {
				if n.kind == k.key && typeSymbols[k.key] != "" && !seenKind[k.key] {
					seenKind[k.key] = true
					key = append(key, typeSymbols[k.key]+" "+strings.ToLower(k.label))
				}
			}
		}
		symbol := data.NewField("secondarystat", nil, col(func(n node) string { return typeSymbols[n.kind] }))
		symbol.Config = &data.FieldConfig{DisplayName: strings.Join(key, " · ")}
		nodeFrame.Fields = append(nodeFrame.Fields, symbol)
		// Without a colour field Grafana draws grey rings, and the legend
		// is the value and the symbol key. With limits, a grey colour field
		// adds a legend entry that explains the red fill. It cannot carry
		// a red swatch: Grafana colours the rings from the field's config
		// colour when it has one.
		if limits {
			ring := data.NewField("color", nil, col(func(node) string { return "#8E8E8E" }))
			ring.Config = &data.FieldConfig{DisplayName: redFillLegend}
			nodeFrame.Fields = append(nodeFrame.Fields, ring)
		}
		nodeFrame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}
		return data.Frames{nodeFrame, depEdgeFrame(edgeIDs, sources, targets, edgeStats, edgeDetails)}
	}

	// The ring colour comes from arc fields rather than one colour field:
	// the panel then lists them as its legend. Only the kinds in this graph
	// get a field, so the legend stays short.
	present := map[string]bool{}
	for _, n := range nodes {
		present[n.kind] = true
	}
	for _, k := range nodeKinds {
		if !present[k.key] {
			continue
		}
		vals := make([]float64, len(nodes))
		for i, n := range nodes {
			if n.kind == k.key {
				vals[i] = 1
			}
		}
		f := data.NewField("arc__"+k.key, nil, vals)
		f.Config = &data.FieldConfig{DisplayName: k.label, Color: map[string]any{"mode": "fixed", "fixedColor": k.color}}
		nodeFrame.Fields = append(nodeFrame.Fields, f)
	}
	if limits {
		// An arc of zero on every node: no ring section, only a legend
		// entry that explains the red fill.
		f := data.NewField("arc__redfill", nil, make([]float64, len(nodes)))
		f.Config = &data.FieldConfig{DisplayName: redFillLegend, Color: map[string]any{"mode": "fixed", "fixedColor": redFillColor}}
		nodeFrame.Fields = append(nodeFrame.Fields, f)
	}
	nodeFrame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}
	return data.Frames{nodeFrame, depEdgeFrame(edgeIDs, sources, targets, edgeStats, edgeDetails)}
}

func depEdgeFrame(ids, sources, targets, stats, details []string) *data.Frame {
	f := data.NewFrame("edges",
		data.NewField("id", nil, ids),
		data.NewField("source", nil, sources),
		data.NewField("target", nil, targets),
		data.NewField("mainstat", nil, stats),
		detailField("formula", "Detail", details),
	)
	f.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}
	return f
}

// depTruncatedNote marks a node whose links the depth limit cut off.
func depTruncatedNote(direction string) string {
	if direction == depUsedBy {
		return "More measures are built from this one: raise the depth to see them."
	}
	return "This measure has more inputs: raise the depth to see them."
}

// detailField is a Node Graph detail column: the panel lists these in the
// menu a click on a node or edge opens, labelled by display name.
func detailField(name, label string, values []string) *data.Field {
	f := data.NewField("detail__"+name, nil, values)
	f.Config = &data.FieldConfig{DisplayName: label}
	return f
}

// latestValueField is the value shown in each circle. The panel lists it
// in its legend too, so it is named for what it is, not "mainstat".
func latestValueField(values []string) *data.Field {
	f := data.NewField("mainstat", nil, values)
	f.Config = &data.FieldConfig{DisplayName: "Latest value"}
	return f
}
