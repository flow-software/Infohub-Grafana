package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// eventBatches reports an events table limited to picked batches.
func eventBatches(qm queryModel) bool {
	return (qm.EventFormat == "" || qm.EventFormat == eventFormatTable) &&
		strings.TrimSpace(qm.Batch+qm.Golden) != ""
}

// runEventBatches is an events table of the picked batches only: the batch
// (Batch) and the golden batch or batches (Golden), one row each, with the
// event's attributes, so what was recorded for them can be compared. A
// first column names each row; the batch comes first, then the golden
// batches in the order picked.
func (d *Datasource) runEventBatches(ctx context.Context, qm queryModel) (backend.DataResponse, error) {
	now := time.Now()
	type pick struct {
		role string
		w    batchWindow
	}
	var picks []pick
	if b := strings.TrimSpace(qm.Batch); b != "" {
		w, err := parseBatch(b, now)
		if err != nil {
			return backend.DataResponse{}, err
		}
		picks = append(picks, pick{"Batch", w})
	}
	goldens := splitBatches(qm.Golden)
	for i, g := range goldens {
		w, err := parseBatch(g, now)
		if err != nil {
			return backend.DataResponse{}, err
		}
		role := "Golden"
		if len(goldens) > 1 {
			role = fmt.Sprintf("Golden %d", i+1)
		}
		picks = append(picks, pick{role, w})
	}

	var series []flow.EventSeries
	infos := map[int]eventInfo{}
	var notices []string
	var roles []string // per series, in order
	for _, p := range picks {
		items := []queryItem{{Tree: flow.TreeModel, Type: itemEvent, ID: p.w.eventID}}
		all, inf, n, err := d.readEvents(ctx, items, backend.TimeRange{From: p.w.start, To: p.w.end})
		if err != nil {
			return backend.DataResponse{}, err
		}
		for k, v := range inf {
			infos[k] = v
		}
		notices = append(notices, n...)
		found := false
		for _, s := range all {
			for _, period := range s.Data {
				if period.Start != nil && period.Start.UnixMilli() == p.w.start.UnixMilli() {
					one := s
					one.Data = []flow.EventPeriod{period}
					series = append(series, one)
					roles = append(roles, p.role)
					found = true
				}
			}
		}
		if !found {
			notices = append(notices, fmt.Sprintf("%s: Flow has no period of this event starting at %s.", p.role, timeText(p.w.start)))
		}
	}

	// One frame per pick keeps the picked order (the table sorts newest
	// first); the attribute columns are those of all of them.
	var frame *data.Frame
	for i, s := range series {
		one := eventTableFrame([]flow.EventSeries{s}, infos, now, qm.EventNumbers)
		role := data.NewField("Batch", nil, []string{roles[i]})
		one.Fields = append([]*data.Field{role}, one.Fields...)
		frame = appendRows(frame, one)
	}
	if frame == nil {
		frame = data.NewFrame("events")
	}
	readable(frame)
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}
	if len(notices) > 0 {
		frame.Meta.Notices = append(frame.Meta.Notices, noticesOf(notices)...)
	}
	return backend.DataResponse{Frames: data.Frames{frame}}, nil
}

// appendRows adds next's rows to acc, matching columns by name; a column
// only one of them has is empty in the other's rows. Nil acc starts with
// next.
func appendRows(acc, next *data.Frame) *data.Frame {
	if acc == nil {
		return next
	}
	rows, nextRows := acc.Rows(), next.Rows()
	byName := map[string]*data.Field{}
	for _, f := range acc.Fields {
		byName[f.Name] = f
	}
	for _, f := range next.Fields {
		if _, ok := byName[f.Name]; ok {
			continue
		}
		// A column the earlier rows lack: empty for them.
		empty := data.NewFieldFromFieldType(f.Type().NullableType(), rows)
		empty.Name, empty.Labels, empty.Config = f.Name, f.Labels, f.Config
		acc.Fields = append(acc.Fields, empty)
		byName[f.Name] = empty
	}
	for i, f := range acc.Fields {
		src, _ := next.FieldByName(f.Name)
		if f.Type() != f.Type().NullableType() {
			// Make room for missing values.
			acc.Fields[i] = nullable(f)
			f = acc.Fields[i]
		}
		for r := 0; r < nextRows; r++ {
			f.Extend(1)
			if src == nil {
				continue
			}
			if v, ok := src.ConcreteAt(r); ok {
				f.SetConcrete(f.Len()-1, v)
			}
		}
	}
	return acc
}

// nullable copies a field into its nullable type.
func nullable(f *data.Field) *data.Field {
	out := data.NewFieldFromFieldType(f.Type().NullableType(), f.Len())
	out.Name, out.Labels, out.Config = f.Name, f.Labels, f.Config
	for i := 0; i < f.Len(); i++ {
		if v, ok := f.ConcreteAt(i); ok {
			out.SetConcrete(i, v)
		}
	}
	return out
}

// readable writes Start and End as times (in the dashboard's time zone) and
// Duration as hours and minutes, as text: this table is meant to be turned
// with Grafana's Transpose transformation, which drops the columns' types
// and units and would show raw numbers.
func readable(frame *data.Frame) {
	for i, f := range frame.Fields {
		switch f.Name {
		case "Start", "End":
			vals := make([]*string, f.Len())
			for r := range vals {
				if v, ok := f.ConcreteAt(r); ok {
					text := timeText(v.(time.Time))
					vals[r] = &text
				} else if f.Name == "End" {
					running := "running"
					vals[r] = &running
				}
			}
			frame.Fields[i] = data.NewField(f.Name, f.Labels, vals)
		case "Duration":
			vals := make([]*string, f.Len())
			for r := range vals {
				if v, ok := f.ConcreteAt(r); ok {
					text := hoursMinutes(time.Duration(v.(float64) * float64(time.Second)))
					vals[r] = &text
				}
			}
			frame.Fields[i] = data.NewField(f.Name, f.Labels, vals)
		}
	}
}

// hoursMinutes writes a duration as "8 h 22 min" (or "45 min").
func hoursMinutes(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}
