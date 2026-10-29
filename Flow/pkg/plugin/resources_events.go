package plugin

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
)

// browseEvents is the browser for events queries: folders, and the events
// in them. Metrics are left out, and so are folders with no event anywhere
// below them; events are what can be picked.
func (d *Datasource) browseEvents(ctx context.Context, tree flow.Tree, typ string, id int) ([]browseNode, error) {
	withEvents, err := d.eventFolders(ctx, tree)
	if err != nil {
		return nil, err
	}
	keep := func(nodes []browseNode) []browseNode {
		out := []browseNode{}
		for _, n := range nodes {
			if n.Type != "folder" || withEvents[n.ID] {
				out = append(out, n)
			}
		}
		return out
	}
	if typ != "folder" {
		nodes, err := d.browse(ctx, tree, typ, id)
		return keep(nodes), err
	}
	folders, err := d.client.ChildFolders(ctx, tree, id)
	if err != nil {
		return nil, err
	}
	events, err := d.client.FolderEvents(ctx, tree, id)
	if err != nil {
		return nil, err
	}
	nodes := []browseNode{}
	for _, f := range folders {
		nodes = append(nodes, folderNode(tree, f))
	}
	nodes = keep(nodes)
	for _, e := range events {
		nodes = append(nodes, browseNode{Tree: tree, Type: itemEvent, ID: e.ID, Name: e.Name, FromTemplate: e.Template != nil, order: e.Order})
	}
	sortNodes(nodes)
	return nodes, nil
}

// eventFolders is every folder that holds an event, itself or in a folder
// below it.
func (d *Datasource) eventFolders(ctx context.Context, tree flow.Tree) (map[int]bool, error) {
	events, err := d.client.AllEvents(ctx, tree)
	if err != nil {
		return nil, err
	}
	folders, err := d.client.Folders(ctx, tree)
	if err != nil {
		return nil, err
	}
	parent := make(map[int]int, len(folders))
	for _, f := range folders {
		parent[f.ID] = refID(f.Parent)
	}
	out := map[int]bool{}
	for _, e := range events {
		// Walk up to the root; a loop in the data stops at a folder seen before.
		for id := refID(e.Folder); id != 0 && !out[id]; id = parent[id] {
			out[id] = true
		}
	}
	return out, nil
}

// searchEvents finds events by name; model events are named by folder path.
func (d *Datasource) searchEvents(ctx context.Context, tree flow.Tree, text string) ([]browseNode, error) {
	text = strings.TrimSpace(text)
	nodes := []browseNode{}
	if text == "" {
		return nodes, nil
	}
	typ := "Event"
	if tree == flow.TreeTemplate {
		typ = "TemplateEvent"
	}
	found, err := d.client.Search(ctx, text, []string{typ}, searchLimit)
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, o := range found {
		nodes = append(nodes, browseNode{Tree: tree, Type: itemEvent, ID: o.ID, Name: o.Name})
		ids = append(ids, o.ID)
	}
	if tree == flow.TreeModel && len(ids) > 0 {
		if events, err := d.client.Events(ctx, tree, ids); err == nil {
			if paths, err := d.index.eventPaths(ctx, events); err == nil {
				for i, n := range nodes {
					if p := paths[n.ID]; p != "" {
						nodes[i].Name = p
					}
				}
			}
		}
	}
	sortNodes(nodes)
	return nodes, nil
}

// eventChoice is an event that selected measures can be read in.
type eventChoice struct {
	ID         int      `json:"id"`
	Name       string   `json:"name"`
	Attributes []string `json:"attributes"`
	// Measures is how many of the selected measures have it as context.
	Measures int `json:"measures"`
}

// contextEvents lists the events the selected model period measures have
// as context, most shared first, for the measures editor's event picker.
func (d *Datasource) contextEvents(ctx context.Context, keys []string) ([]eventChoice, error) {
	items, err := parseItems(keys)
	if err != nil {
		return nil, err
	}
	sel, err := d.resolve(ctx, items)
	if err != nil {
		return nil, err
	}
	out := []eventChoice{}
	if len(sel.periodIDs) == 0 {
		return out, nil
	}
	measures, _, err := d.client.MeasuresWithMetadata(ctx, flow.TreeModel, sel.periodIDs)
	if err != nil {
		return nil, err
	}
	count := map[int]int{}
	var ids []int
	for _, m := range measures {
		for _, id := range contextEventIDs(m) {
			count[id]++
			ids = appendUnique(ids, id)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	events, err := d.client.Events(ctx, flow.TreeModel, ids)
	if err != nil {
		return nil, err
	}
	paths, _ := d.index.eventPaths(ctx, events)
	for _, e := range events {
		ce := eventChoice{ID: e.ID, Name: e.Name, Measures: count[e.ID]}
		if p := paths[e.ID]; p != "" {
			ce.Name = p
		}
		for _, a := range e.Attributes {
			ce.Attributes = append(ce.Attributes, a.Name)
		}
		out = append(out, ce)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Measures != out[j].Measures {
			return out[i].Measures > out[j].Measures
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// markEventContext fills Events on the measure nodes that have events as
// context, and the interval where it is missing (search results have none).
// The browser listing has no metadata, so it is looked up here; if that
// fails the nodes go unmarked rather than the listing failing.
func (d *Datasource) markEventContext(ctx context.Context, tree flow.Tree, nodes []browseNode) {
	var ids []int
	for _, n := range nodes {
		if n.Type == itemMeasure {
			ids = append(ids, n.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	measures, _, err := d.client.MeasuresWithMetadata(ctx, tree, ids)
	if err != nil {
		return
	}
	byMeasure := map[int][]int{}
	interval, retrieval := map[int]string{}, map[int]string{}
	var eventIDs []int
	for _, m := range measures {
		interval[m.ID], retrieval[m.ID] = m.Interval, m.Retrieval
		for _, id := range contextEventIDs(m) {
			byMeasure[m.ID] = append(byMeasure[m.ID], id)
			eventIDs = appendUnique(eventIDs, id)
		}
	}
	for i, n := range nodes {
		if n.Type == itemMeasure && n.Interval == "" {
			nodes[i].Interval = interval[n.ID]
		}
		if n.Type == itemMeasure && n.Retrieval == "" {
			nodes[i].Retrieval = retrieval[n.ID]
		}
	}
	if len(eventIDs) == 0 {
		return
	}
	names := map[int]string{}
	if events, err := d.client.Events(ctx, tree, eventIDs); err == nil {
		for _, e := range events {
			names[e.ID] = e.Name
		}
	}
	for i, n := range nodes {
		for _, id := range byMeasure[n.ID] {
			name := names[id]
			if name == "" {
				name = "event " + strconv.Itoa(id)
			}
			if n.Type == itemMeasure {
				nodes[i].Events = append(nodes[i].Events, name)
			}
		}
	}
}

// contextEventIDs are the events a measure has as context, from its
// metadata.
func contextEventIDs(m flow.Measure) []int {
	for _, md := range m.Metadata {
		if strings.EqualFold(md.Type, "context") {
			var ids []int
			_ = json.Unmarshal(md.Data["events"], &ids)
			return ids
		}
	}
	return nil
}

// attributeValue is one value of an event attribute, with its colour.
type attributeValue struct {
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
}

// eventValuesWindow is how far back eventValues looks. Flow's public API
// does not list an attribute's possible values, so the editor suggests the
// ones that occurred recently; others can still be typed.
const eventValuesWindow = 90 * 24 * time.Hour

func (d *Datasource) eventValues(ctx context.Context, tree flow.Tree, eventID int, attribute string) ([]attributeValue, error) {
	if eventID == 0 || attribute == "" {
		return nil, &badRequest{"event and attribute are required"}
	}
	now := time.Now()
	req := flow.EventDataRequest{From: now.Add(-eventValuesWindow), To: now, Limit: eventPeriodLimit}
	// A template event's values are those of every event created from it.
	if tree == flow.TreeTemplate {
		req.TemplateIDs = []int{eventID}
	} else {
		req.IDs = []int{eventID}
	}
	res, err := d.client.EventData(ctx, req)
	out := []attributeValue{}
	if flow.IsNoData(err) || (err == nil && res == nil) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range res.Values {
		for _, p := range s.Data {
			if a, ok := attributeOf(p, attribute); ok && a.Value != "" && !seen[a.Value] {
				seen[a.Value] = true
				out = append(out, attributeValue{Value: a.Value, Color: a.Color})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Value) < strings.ToLower(out[j].Value) })
	return out, nil
}

// eventAttributeNames lists an event's attribute names in Flow's order,
// for the event variable's attribute picker.
func (d *Datasource) eventAttributeNames(ctx context.Context, tree flow.Tree, eventID int) ([]string, error) {
	if eventID == 0 {
		return nil, &badRequest{"event is required"}
	}
	events, err := d.client.Events(ctx, tree, []int{eventID})
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range events {
		for _, a := range e.Attributes {
			if !containsString(names, a.Name) {
				names = append(names, a.Name)
			}
		}
	}
	return names, nil
}

// parseItems reads "<tree>:<type>:<id>" item keys of metrics and measures.
func parseItems(keys []string) ([]queryItem, error) {
	var items []queryItem
	for _, k := range keys {
		parts := strings.Split(k, ":")
		if len(parts) != 3 {
			return nil, &badRequest{"item must be <tree>:<type>:<id>, got " + k}
		}
		id, err := strconv.Atoi(parts[2])
		it := queryItem{Tree: flow.Tree(parts[0]), Type: parts[1], ID: id}
		if err != nil || !it.Tree.Valid() || (it.Type != itemMetric && it.Type != itemMeasure && it.Type != itemChart && it.Type != itemFolder) {
			return nil, &badRequest{"invalid item " + k}
		}
		items = append(items, it)
	}
	return items, nil
}
