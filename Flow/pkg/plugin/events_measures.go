package plugin

import (
	"context"
	"fmt"
	"sort"
	"time"

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
	res, evalNotice, err := d.measureData(ctx, flow.MeasureDataRequest{
		IDs: sel.periodIDs, EventID: qm.EventID, From: tr.From, To: tr.To, Limit: periodLimit, Users: qm.IncludeVersion,
		Exceptions: qm.LimitStatus,
	})
	if evalNotice != "" {
		notices = append(notices, evalNotice)
	}
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
	// A period still running is drawn up to now, or the range end if sooner.
	openEnd := time.Now()
	if tr.To.Before(openEnd) {
		openEnd = tr.To
	}

	// periodValue is one event period's value, before it becomes points.
	type periodValue struct {
		start, end time.Time
		running    bool
		pt         point
	}
	type group struct {
		s       series
		periods []periodValue
	}
	var out []series
	for _, v := range res.Values {
		groups := map[string]*group{}
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
			if p.Start == nil {
				continue
			}
			key := ""
			if split {
				key = value
			}
			g, ok := groups[key]
			if !ok {
				g = &group{s: series{id: v.ID}}
				if split {
					g.s.attribute, g.s.value = qm.EventAttribute, value
				}
				groups[key] = g
				order = append(order, key)
			}
			if split && g.s.color == "" {
				g.s.color = color
			}
			pv := periodValue{start: p.Start.Time, end: openEnd, running: p.End == nil, pt: point{value: p.Value, quality: 192, start: p.Start.Time}}
			if p.End != nil {
				pv.end = p.End.Time
			}
			withVersion(&pv.pt, p)
			if p.Detail != nil {
				pv.pt.quality = p.Detail.Quality.Value
			}
			if qm.LimitStatus && evalNotice == "" {
				pv.pt.limit = limitStatus(p)
			}
			g.periods = append(g.periods, pv)
		}
		if len(order) == 0 {
			// Kept as an empty series so the measure still shows.
			out = append(out, series{id: v.ID})
			continue
		}
		for _, key := range order {
			g := groups[key]
			sort.SliceStable(g.periods, func(i, j int) bool { return g.periods[i].start.Before(g.periods[j].start) })
			for i, pv := range g.periods {
				switch qm.TimeAt {
				case timeAtPeriod:
					// The value held from start to end, then a gap until the
					// next period starts: event periods do not run back to back
					// like calendar periods, so one timestamp would hide when
					// the period ran.
					a, b := pv.pt, pv.pt
					a.t, b.t = pv.start, pv.end
					g.s.points = append(g.s.points, a, b)
					if i+1 < len(g.periods) && g.periods[i+1].start.After(pv.end) {
						gap := point{t: pv.end.Add(time.Millisecond), quality: pv.pt.quality}
						g.s.points = append(g.s.points, gap)
					}
				case "end":
					pt := pv.pt
					pt.t = pv.end
					if pv.running {
						// No end yet: the value stays at the start.
						pt.t = pv.start
					}
					g.s.points = append(g.s.points, pt)
				default:
					pt := pv.pt
					pt.t = pv.start
					g.s.points = append(g.s.points, pt)
				}
			}
			out = append(out, g.s)
		}
	}
	return out, notices, nil
}
