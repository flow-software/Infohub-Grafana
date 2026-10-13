package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// browseNode is one row of the query editor's model browser.
type browseNode struct {
	Tree flow.Tree `json:"tree"`
	// Type is folder, metric or measure.
	Type        string `json:"type"`
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Interval    string `json:"interval,omitempty"`
	UOM         string `json:"uom,omitempty"`
	Description string `json:"description,omitempty"`
	// FromTemplate marks model objects created from a template.
	FromTemplate bool `json:"fromTemplate,omitempty"`
	// Attributes are an event's attribute names.
	Attributes []string `json:"attributes,omitempty"`
	// Events names the events a measure has as context, so the browser can
	// mark measures that can be read per event period.
	Events []string `json:"events,omitempty"`
	order  *int
}

// CallResource serves the query editor:
//
//	GET calendars                          Flow calendars
//	GET browse?tree=model|template         root folders
//	GET browse?tree=..&type=folder&id=N    a folder's folders and metrics
//	GET browse?tree=..&type=metric&id=N    a metric's measures
//	GET browse?..&type=folder&id=N&kind=events  a folder's folders and events
//	GET search?tree=..&q=text              metrics and measures by name, or a measure by id
//	GET search?tree=..&q=text&kind=events  events by name
//	GET contextEvents?item=<tree>:<type>:<id>..  events the selected model measures have as context
//	GET eventValues?event=N&attribute=Name  values of an event attribute seen in the last 90 days
//	GET attributes                         model attribute names on measures
//	GET attributeValues?attribute=Name     values of a model attribute on measures
//	GET charts                             root chart folders
//	GET charts?id=N                        a chart folder's folders and charts
//	GET summary?item=<tree>:<type>:<id>..  how many period measures and tags a selection holds
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	reply := func(status int, body any) error {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		return sender.Send(&backend.CallResourceResponse{
			Status:  status,
			Headers: map[string][]string{"Content-Type": {"application/json"}},
			Body:    b,
		})
	}
	fail := func(status int, msg string) error {
		return reply(status, map[string]string{"message": msg})
	}

	if req.Method != http.MethodGet {
		return fail(http.StatusMethodNotAllowed, "method not allowed")
	}
	if d.configErr != nil {
		return fail(http.StatusBadRequest, d.configErr.Error())
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		return fail(http.StatusBadRequest, err.Error())
	}
	params := u.Query()
	tree := flow.Tree(params.Get("tree"))
	if tree == "" {
		tree = flow.TreeModel
	}

	var body any
	switch strings.Trim(req.Path, "/") {
	case "calendars":
		body, err = d.listCalendars(ctx)
	case "browse":
		if !tree.Valid() {
			return fail(http.StatusBadRequest, "tree must be model or template")
		}
		id, _ := strconv.Atoi(params.Get("id"))
		if params.Get("kind") == "events" {
			body, err = d.browseEvents(ctx, tree, params.Get("type"), id)
		} else {
			body, err = d.browse(ctx, tree, params.Get("type"), id)
		}
	case "search":
		if !tree.Valid() {
			return fail(http.StatusBadRequest, "tree must be model or template")
		}
		if params.Get("kind") == "events" {
			body, err = d.searchEvents(ctx, tree, params.Get("q"))
		} else {
			body, err = d.search(ctx, tree, params.Get("q"))
		}
	case "charts":
		id, _ := strconv.Atoi(params.Get("id"))
		body, err = d.browseCharts(ctx, id)
	case "summary":
		body, err = d.summary(ctx, params["item"])
	case "contextEvents":
		body, err = d.contextEvents(ctx, params["item"])
	case "attributes":
		body, err = d.attributeNamesResource(ctx)
	case "attributeValues":
		body, err = d.attributeValuesResource(ctx, params.Get("attribute"))
	case "eventValues":
		id, _ := strconv.Atoi(params.Get("event"))
		body, err = d.eventValues(ctx, id, params.Get("attribute"))
	default:
		return fail(http.StatusNotFound, "not found")
	}
	if err != nil {
		var withStatus interface{ HTTPStatus() int }
		status := http.StatusBadGateway
		if errors.As(err, &withStatus) && withStatus.HTTPStatus() == http.StatusBadRequest {
			status = http.StatusBadRequest
		}
		return fail(status, err.Error())
	}
	return reply(http.StatusOK, body)
}

func (d *Datasource) browse(ctx context.Context, tree flow.Tree, typ string, id int) ([]browseNode, error) {
	nodes := []browseNode{}
	switch typ {
	case "", "root":
		folders, err := d.client.Folders(ctx, tree)
		if err != nil {
			return nil, err
		}
		for _, f := range folders {
			if f.Parent == nil {
				nodes = append(nodes, folderNode(tree, f))
			}
		}
	case "folder":
		folders, err := d.client.ChildFolders(ctx, tree, id)
		if err != nil {
			return nil, err
		}
		metrics, err := d.client.FolderMetrics(ctx, tree, id)
		if err != nil {
			return nil, err
		}
		for _, f := range folders {
			nodes = append(nodes, folderNode(tree, f))
		}
		for _, m := range metrics {
			nodes = append(nodes, browseNode{Tree: tree, Type: itemMetric, ID: m.ID, Name: m.Name, FromTemplate: m.Template != nil, order: m.Order})
		}
	case itemMetric:
		measures, err := d.client.MetricMeasures(ctx, tree, id)
		if err != nil {
			return nil, err
		}
		// The metric's name, to drop it from the front of measure names.
		metricName := ""
		if metrics, err := d.client.Metrics(ctx, tree, []int{id}); err == nil && len(metrics) > 0 {
			metricName = metrics[0].Name
		}
		for _, m := range measures {
			nodes = append(nodes, measureNode(tree, m, metricName))
		}
		d.markEventContext(ctx, tree, nodes)
	default:
		return nil, &badRequest{"type must be root, folder or metric"}
	}
	sortNodes(nodes)
	return nodes, nil
}

func folderNode(tree flow.Tree, f flow.Folder) browseNode {
	return browseNode{Tree: tree, Type: "folder", ID: f.ID, Name: f.Name, FromTemplate: f.Template != nil, order: f.Order}
}

// measureNode drops the "<metric>." prefix from Flow's hierarchical measure
// name; under its metric in the tree the prefix is noise.
func measureNode(tree flow.Tree, m flow.Measure, metricName string) browseNode {
	name := m.Name
	if metricName != "" {
		name = strings.TrimPrefix(name, metricName+".")
	}
	return browseNode{
		Tree: tree, Type: itemMeasure, ID: m.ID, Name: name,
		Interval: m.Interval, UOM: m.UOM, Description: m.Description, FromTemplate: m.Template != nil,
	}
}

// sortNodes keeps folders, then metrics, then measures, each in Flow's
// display order and then by name.
func sortNodes(nodes []browseNode) {
	rank := map[string]int{"folder": 0, itemMetric: 1, itemMeasure: 2}
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if rank[a.Type] != rank[b.Type] {
			return rank[a.Type] < rank[b.Type]
		}
		ao, bo := orderOf(a), orderOf(b)
		if ao != bo {
			return ao < bo
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

func orderOf(n browseNode) int {
	if n.order == nil {
		return 0
	}
	return *n.order
}

// searchLimit caps search results shown in the browser.
const searchLimit = 200

func (d *Datasource) search(ctx context.Context, tree flow.Tree, text string) ([]browseNode, error) {
	text = strings.TrimSpace(text)
	nodes := []browseNode{}
	if text == "" {
		return nodes, nil
	}
	types := []string{"Metric", "Measure"}
	if tree == flow.TreeTemplate {
		types = []string{"TemplateMetric", "TemplateMeasure"}
	}
	// A number ("4304", "#4304", "id:4304") is also looked up as a measure
	// id; it leads the results, ahead of names that contain the number.
	var byID *browseNode
	if id, ok := parseMeasureID(text); ok {
		ms, err := d.client.Measures(ctx, tree, []int{id})
		if err != nil {
			return nil, err
		}
		if len(ms) > 0 {
			n := measureNode(tree, ms[0], "")
			byID = &n
			nodes = append(nodes, n)
		}
	}
	found, err := d.client.Search(ctx, text, types, searchLimit)
	if err != nil {
		if byID == nil {
			return nil, err
		}
		// "#4304" is no use as a name search; the id match stands alone.
		found = nil
	}
	for _, o := range found {
		if byID != nil && o.ID == byID.ID && strings.HasSuffix(strings.ToLower(o.Type), "measure") {
			continue
		}
		t := strings.ToLower(o.Type)
		switch {
		case strings.HasSuffix(t, "metric"):
			nodes = append(nodes, browseNode{Tree: tree, Type: itemMetric, ID: o.ID, Name: o.Name})
		case strings.HasSuffix(t, "measure"):
			nodes = append(nodes, browseNode{Tree: tree, Type: itemMeasure, ID: o.ID, Name: o.Name})
		}
	}
	// Measures are named only by themselves in search results; for model
	// measures the index adds the folder path so same-named instances differ.
	if tree == flow.TreeModel {
		var ids []int
		for _, n := range nodes {
			if n.Type == itemMeasure {
				ids = append(ids, n.ID)
			}
		}
		if len(ids) > 0 {
			if infos, err := d.index.describe(ctx, ids); err == nil {
				for i, n := range nodes {
					if info, ok := infos[n.ID]; ok && n.Type == itemMeasure {
						nodes[i].Name = info.Path
						nodes[i].UOM = info.UOM
					}
				}
			}
		}
	}
	d.markEventContext(ctx, tree, nodes)
	sortNodes(nodes)
	if byID != nil {
		// Keep the id match first, above the name matches sortNodes ordered.
		for i, n := range nodes {
			if n.Type == itemMeasure && n.ID == byID.ID {
				copy(nodes[1:i+1], nodes[:i])
				nodes[0] = n
				break
			}
		}
	}
	return nodes, nil
}

// parseMeasureID reads a measure id typed into the search box: "4304",
// "#4304" or "id:4304".
func parseMeasureID(text string) (int, bool) {
	t := strings.TrimSpace(strings.ToLower(text))
	t = strings.TrimPrefix(t, "#")
	if rest, ok := strings.CutPrefix(t, "id"); ok {
		t = strings.TrimSpace(strings.TrimLeft(rest, ": "))
	}
	id, err := strconv.Atoi(t)
	return id, err == nil && id > 0
}

type badRequest struct{ msg string }

func (e *badRequest) Error() string   { return e.msg }
func (e *badRequest) HTTPStatus() int { return http.StatusBadRequest }
