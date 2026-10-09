package plugin

import (
	"context"
	"fmt"
	"sort"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// noEventValue is the split value of periods without the attribute.
const noEventValue = "(none)"

// readEventPeriods reads period measures per period of the query's event:
// one value per event period, at its start or end. Periods can be kept to
// some values of one attribute, and split into a series per value.
//
// Instances of a template measure each have their own event, and Flow reads
// one event per call, so template measures are left out with a notice.
func (d *Datasource) readEventPeriods(ctx context.Context, sel *selection, qm queryModel, tr backend.TimeRange) ([]series, []string, error) {
	var notices []string
	if len(sel.periodTemplateIDs) > 0 {
		notices = append(notices, "Event context reads model measures only: each instance of a template measure has its own event. Pick the instances, or use the calendar context.")
	}
	if len(sel.periodIDs) == 0 {
		return nil, notices, nil
	}
	if qm.EventID == 0 {
		return nil, append(notices, "Pick an event to read the measures in."), nil
	}
	res, err := d.client.MeasureData(ctx, flow.MeasureDataRequest{
		IDs: sel.periodIDs, EventID: qm.EventID, From: tr.From, To: tr.To, Limit: periodLimit,
	})
	if flow.IsNoData(err) || (err == nil && res == nil) {
		return nil, notices, nil
	}
	if err != nil {
		return nil, notices, err
	}
	notices = append(notices, res.Errors...)
	if res.Continuation != nil {
		notices = append(notices, fmt.Sprintf("Flow returned only the first %d event periods; some values are missing. Narrow the time range or select fewer measures.", periodLimit))
	}

	keep := map[string]bool{}
	for _, v := range qm.EventValues {
		keep[v] = true
	}
	filter := qm.EventAttribute != "" && len(keep) > 0
	split := qm.EventAttribute != "" && qm.EventSplit
	atEnd := qm.TimeAt == "end"

	var out []series
	for _, v := range res.Values {
		bySplit := map[string]*series{}
		var order []string
		for _, p := range v.Data {
			value, color := "", ""
			if qm.EventAttribute != "" {
				value = noEventValue
				if a, ok := findAttribute(p.Attributes, qm.EventAttribute); ok && a.Value != "" {
					value, color = a.Value, a.Color
				}
			}
			if filter && !keep[value] {
				continue
			}
			t := p.Start
			if atEnd && p.End != nil {
				t = p.End
			}
			if t == nil {
				continue
			}
			key := ""
			if split {
				key = value
			}
			s, ok := bySplit[key]
			if !ok {
				s = &series{id: v.ID}
				if split {
					s.attribute, s.value = qm.EventAttribute, value
				}
				bySplit[key] = s
				order = append(order, key)
			}
			if split && s.color == "" {
				s.color = color
			}
			pt := point{t: t.Time, value: p.Value, quality: 192}
			if p.Detail != nil {
				pt.quality = p.Detail.Quality.Value
			}
			s.points = append(s.points, pt)
		}
		if len(order) == 0 {
			// Kept as an empty series so the measure still shows.
			out = append(out, series{id: v.ID})
			continue
		}
		for _, key := range order {
			s := bySplit[key]
			sort.SliceStable(s.points, func(i, j int) bool { return s.points[i].t.Before(s.points[j].t) })
			out = append(out, *s)
		}
	}
	return out, notices, nil
}
