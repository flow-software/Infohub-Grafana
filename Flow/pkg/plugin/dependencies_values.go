package plugin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// Dependency value modes. Unset shows each measure's latest value.
const depValuesNone = "none"

// depValue is a measure's latest value in the panel's time range.
type depValue struct {
	Value *float64
	// Text is Flow's formatted value with the unit, e.g. "4,00 #".
	Text string
	Time time.Time
	// User is who entered the value, for manual measures.
	User string
	// LastEntry is the latest value in range that someone entered, when
	// the latest value has no user (Flow filled it in, e.g. a default).
	LastEntry *depEntry
}

type depEntry struct {
	User string
	Time time.Time
}

func userName(u *flow.User) string {
	if u == nil {
		return ""
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Username
}

// enteredBy says who entered a manual measure's latest value.
func (v depValue) enteredBy() string {
	switch {
	case v.User != "":
		return v.User
	case v.LastEntry != nil:
		return fmt.Sprintf("no user on the latest value; last entered by %s for %s", v.LastEntry.User, v.LastEntry.Time.UTC().Format("2006-01-02 15:04 UTC"))
	default:
		return "no user on any value in the time range"
	}
}

// depMeasureValues reads the latest value of each model measure in the
// rows: period measures from their latest period with a value in the
// calendar, tags from Flow's "last" aggregation over the whole range.
// Template measures hold no data and are skipped.
func (d *Datasource) depMeasureValues(ctx context.Context, g *depGraph, rows []depRow, treeOf map[string]flow.Tree, qm queryModel, tr backend.TimeRange) (map[string]depValue, []string) {
	values := map[string]depValue{}
	if g == nil || g.tree != flow.TreeModel {
		return values, nil
	}
	var periodIDs, tagIDs []int
	for _, r := range rows {
		if r.Type != "measure" || treeOf[r.ID] != flow.TreeModel {
			continue
		}
		m, ok := g.measures[r.MeasureID]
		if !ok {
			continue
		}
		if m.IsTag() {
			tagIDs = appendUnique(tagIDs, m.ID)
		} else {
			periodIDs = appendUnique(periodIDs, m.ID)
		}
	}

	var notices []string
	put := func(id int, v *float64, formatted string, t time.Time, user *flow.User) {
		if v == nil {
			return
		}
		text := strings.TrimSpace(formatted)
		if text == "" {
			text = strconv.FormatFloat(*v, 'f', -1, 64)
			if f, err := strconv.ParseFloat(text, 64); err == nil && strings.Contains(text, ".") {
				text = strconv.FormatFloat(f, 'f', 2, 64)
			}
		}
		if uom := g.measures[id].UOM; uom != "" {
			text += " " + uom
		}
		values[depKey(flow.TreeModel, "measure", id)] = depValue{Value: v, Text: text, Time: t, User: userName(user)}
	}

	if len(periodIDs) > 0 {
		calendarID, err := d.calendarFor(ctx, qm.CalendarID)
		if err != nil {
			notices = append(notices, "Values of period measures: "+err.Error())
		} else {
			res, err := d.client.MeasureData(ctx, flow.MeasureDataRequest{
				IDs: periodIDs, CalendarID: calendarID, From: tr.From, To: tr.To, Limit: periodLimit, Users: true,
			})
			switch {
			case flow.IsNoData(err):
			case err != nil:
				notices = append(notices, "Could not read period measure values: "+err.Error())
			case res != nil:
				for _, s := range res.Values {
					var best, entered *flow.PeriodValue
					for i := range s.Data {
						p := &s.Data[i]
						if p.Value == nil || p.Start == nil {
							continue
						}
						if best == nil || p.Start.After(best.Start.Time) {
							best = p
						}
						if userName(p.User) != "" && (entered == nil || p.Start.After(entered.Start.Time)) {
							entered = p
						}
					}
					if best != nil {
						put(s.ID, best.Value, best.Formatted, best.Start.Time, best.User)
						if entered != nil && entered != best {
							key := depKey(flow.TreeModel, "measure", s.ID)
							v := values[key]
							v.LastEntry = &depEntry{User: userName(entered.User), Time: entered.Start.Time}
							values[key] = v
						}
					}
				}
			}
		}
	}

	if len(tagIDs) > 0 {
		span := tr.To.Sub(tr.From).Milliseconds() + 1
		res, err := d.client.TagData(ctx, flow.TagDataRequest{
			IDs: tagIDs, From: tr.From, To: tr.To,
			Aggregation: "last", ResolutionMs: max(span, 1000), Limit: tagPageLimit,
		})
		switch {
		case flow.IsNoData(err):
		case err != nil:
			notices = append(notices, "Could not read tag values: "+err.Error())
		case res != nil:
			for _, s := range res.Values {
				for i := len(s.Data) - 1; i >= 0; i-- {
					r := s.Data[i]
					if r.Value != nil && r.Start != nil {
						put(s.ID, r.Value, r.Formatted, r.Start.Time, nil)
						break
					}
				}
			}
		}
	}
	return values, notices
}

// depStats is how a node describes the way a measure gets its values: its
// period, then its retrieval type, e.g. "hourly · calculated".
func depStats(m flow.Measure) string {
	return strings.Trim(fmt.Sprintf("%s · %s", m.Interval, m.Retrieval), " ·")
}
