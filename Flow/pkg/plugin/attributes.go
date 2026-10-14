package plugin

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
)

// attributeCache keeps every model measure with its model attributes and
// template, for attribute filters and variables. One call reads the whole
// model, so it is kept for cacheTTL rather than read per panel.
type attributeCache struct {
	mu       sync.Mutex
	measures map[int]flow.Measure
	at       time.Time
}

// modelMeasures is every model measure the token can read, by id.
func (d *Datasource) modelMeasures(ctx context.Context) (map[int]flow.Measure, error) {
	d.attrs.mu.Lock()
	if d.attrs.measures != nil && time.Since(d.attrs.at) < cacheTTL {
		defer d.attrs.mu.Unlock()
		return d.attrs.measures, nil
	}
	d.attrs.mu.Unlock()

	list, err := d.client.AllMeasuresWithFields(ctx, flow.TreeModel)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]flow.Measure, len(list))
	for _, m := range list {
		byID[m.ID] = m
	}
	d.attrs.mu.Lock()
	d.attrs.measures, d.attrs.at = byID, time.Now()
	d.attrs.mu.Unlock()
	return byID, nil
}

// fieldValue is a measure's value for a model attribute, matched by name
// without regard to case; ok is false when the measure does not have it.
func fieldValue(m flow.Measure, name string) (string, bool) {
	for _, f := range m.Fields {
		if strings.EqualFold(f.Name, name) {
			return f.Value, true
		}
	}
	return "", false
}

// attributeNames lists the model attribute names that measures carry.
func attributeNames(measures map[int]flow.Measure) []string {
	// Names match without case; of the spellings of one name, keep the one
	// most measures use (ties: the first alphabetically), so the answer does
	// not depend on map order.
	counts := map[string]map[string]int{}
	for _, m := range measures {
		for _, f := range m.Fields {
			if f.Name == "" {
				continue
			}
			key := strings.ToLower(f.Name)
			if counts[key] == nil {
				counts[key] = map[string]int{}
			}
			counts[key][f.Name]++
		}
	}
	out := make([]string, 0, len(counts))
	for _, spellings := range counts {
		best := ""
		for n, c := range spellings {
			if best == "" || c > spellings[best] || (c == spellings[best] && n < best) {
				best = n
			}
		}
		out = append(out, best)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// attributeValues lists the distinct non-empty values of one attribute
// across the given measures.
func attributeValues(measures map[int]flow.Measure, name string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range measures {
		if v, ok := fieldValue(m, name); ok && v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// attributeMatch reports whether a measure's value of the attribute is one
// of the wanted values. An empty want list matches every measure.
func attributeMatch(m flow.Measure, name string, want []string) bool {
	if name == "" || len(want) == 0 {
		return true
	}
	v, _ := fieldValue(m, name)
	for _, w := range want {
		if v == w {
			return true
		}
	}
	return false
}
