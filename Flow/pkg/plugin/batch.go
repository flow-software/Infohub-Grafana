package plugin

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Band modes around the golden batch's values.
const (
	bandAbsolute = "absolute"
	bandPercent  = "percent"
)

// elapsedField names the x field of batch frames: seconds since the
// batch's start, so batches that ran at different times line up.
const elapsedField = "Elapsed"

// batchWindow is one event period, as an event periods variable values it:
// "<event id>:<start ms>:<end ms>", with no end while it runs.
type batchWindow struct {
	eventID    int
	start, end time.Time
	running    bool
}

// parseBatch reads a batch value. A multi-value variable ("{a,b}") gives
// its first batch.
func parseBatch(s string, now time.Time) (batchWindow, error) {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "{}"))
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ":")
	bad := &badRequest{fmt.Sprintf("%q is not a batch: use a Periods (batches) variable of this data source, such as $batch", s)}
	if len(parts) != 3 {
		return batchWindow{}, bad
	}
	id, err1 := strconv.Atoi(parts[0])
	start, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return batchWindow{}, bad
	}
	w := batchWindow{eventID: id, start: time.UnixMilli(start).UTC(), end: now, running: parts[2] == ""}
	if !w.running {
		end, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return batchWindow{}, bad
		}
		w.end = time.UnixMilli(end).UTC()
	}
	if !w.end.After(w.start) {
		return batchWindow{}, bad
	}
	return w, nil
}

// runBatch reads the query over one batch's window instead of the
// dashboard's time range, with time as seconds since the batch's start.
// With Golden, the same measures are read over the golden batch's window,
// aligned on its own start, and drawn dashed beside them, with an optional
// band around them: each measure's trend in this batch against the
// reference batch, from batch start. Grafana's Trend panel draws these
// frames (Elapsed is their x field).
func (d *Datasource) runBatch(ctx context.Context, qm queryModel, q backend.DataQuery) (backend.DataResponse, error) {
	now := time.Now()
	batch, err := parseBatch(qm.Batch, now)
	if err != nil {
		return backend.DataResponse{}, err
	}
	base := qm
	base.Batch, base.Golden = "", ""
	// Corrections annotations and history tables have no place on an
	// elapsed-time axis.
	base.Versions = ""

	qb := q
	qb.TimeRange = backend.TimeRange{From: batch.start, To: batch.end}
	resp, err := d.run(ctx, base, qb)
	if err != nil {
		return backend.DataResponse{}, err
	}
	notices := takeNotices(resp.Frames)
	frames := elapsedFrames(resp.Frames, batch.start)
	if batch.running {
		notices = append(notices, "The batch is still running: it is shown up to now.")
	}

	if strings.TrimSpace(qm.Golden) != "" {
		golden, err := parseBatch(qm.Golden, now)
		if err != nil {
			return backend.DataResponse{}, err
		}
		gm := base
		gm.Limits, gm.LimitStatus = "", false
		qg := q
		qg.TimeRange = backend.TimeRange{From: golden.start, To: golden.end}
		gresp, err := d.run(ctx, gm, qg)
		if err != nil {
			return backend.DataResponse{}, fmt.Errorf("reading the golden batch: %w", err)
		}
		for _, n := range takeNotices(gresp.Frames) {
			notices = append(notices, "Golden batch: "+n)
		}
		for _, f := range elapsedFrames(gresp.Frames, golden.start) {
			frames = append(frames, goldenFrame(f, qm))
		}
	}

	// The Trend panel takes one frame: every series joined on Elapsed.
	if joined := joinOnElapsed(frames); joined != nil {
		frames = data.Frames{joined}
	}
	if len(notices) > 0 {
		if len(frames) == 0 {
			frames = append(frames, data.NewFrame("notices"))
		}
		if frames[0].Meta == nil {
			frames[0].Meta = &data.FrameMeta{}
		}
		for _, n := range notices {
			frames[0].Meta.Notices = append(frames[0].Meta.Notices, data.Notice{Severity: data.NoticeSeverityWarning, Text: n})
		}
	}
	return backend.DataResponse{Frames: frames}, nil
}

// takeNotices removes the frames' notices and returns their texts, so the
// batch and golden reads can be combined into one set.
func takeNotices(frames data.Frames) []string {
	var out []string
	for _, f := range frames {
		if f.Meta == nil {
			continue
		}
		for _, n := range f.Meta.Notices {
			out = append(out, n.Text)
		}
		f.Meta.Notices = nil
	}
	return out
}

// elapsedFrames replaces each series frame's Time field with Elapsed:
// seconds since start. Frames without a time field (annotations, empty
// notice frames) are dropped.
func elapsedFrames(frames data.Frames, start time.Time) data.Frames {
	var out data.Frames
	for _, f := range frames {
		if len(f.Fields) == 0 || f.Fields[0].Type() != data.FieldTypeTime || (f.Meta != nil && f.Meta.DataTopic != "") {
			continue
		}
		times := f.Fields[0]
		secs := make([]float64, times.Len())
		for i := range secs {
			secs[i] = times.At(i).(time.Time).Sub(start).Seconds()
		}
		elapsed := data.NewField(elapsedField, nil, secs)
		elapsed.Config = &data.FieldConfig{DisplayNameFromDS: "Time since batch start", Unit: "s"}
		f.Fields[0] = elapsed
		if f.Meta != nil {
			// No longer a time series: a Trend panel draws it.
			f.Meta.Type, f.Meta.TypeVersion = "", data.FrameTypeVersion{}
		}
		out = append(out, f)
	}
	return out
}

// goldenFrame marks a frame as the golden batch: its value dashed and
// named "· golden", and with a band, the band's upper and lower edges
// filled between, in gold.
func goldenFrame(f *data.Frame, qm queryModel) *data.Frame {
	f.Name += " · golden"
	if len(f.Fields) < 2 {
		return f
	}
	value := f.Fields[1]
	if value.Config == nil {
		value.Config = &data.FieldConfig{}
	}
	name := value.Config.DisplayNameFromDS
	if name == "" {
		name = f.Name
	}
	value.Config.DisplayNameFromDS = strings.TrimSuffix(name, " · golden") + " · golden"
	value.Config.Custom = map[string]any{"lineStyle": map[string]any{"fill": "dash", "dash": []int{10, 10}}}
	// Only the golden value itself; limits and evaluations belong to the batch.
	f.Fields = f.Fields[:2]
	if qm.Band <= 0 || value.Type() != data.FieldTypeNullableFloat64 {
		return f
	}
	n := value.Len()
	upper, lower := make([]*float64, n), make([]*float64, n)
	for i := 0; i < n; i++ {
		v := value.At(i).(*float64)
		if v == nil {
			continue
		}
		d := qm.Band
		if qm.BandMode == bandPercent {
			d = abs(*v) * qm.Band / 100
		}
		hi, lo := *v+d, *v-d
		upper[i], lower[i] = &hi, &lo
	}
	base := strings.TrimSuffix(value.Config.DisplayNameFromDS, " · golden")
	lowerName := base + " · golden lower"
	band := func(field, display string, vals []*float64) *data.Field {
		bf := data.NewField(field, value.Labels, vals)
		bf.Config = &data.FieldConfig{
			DisplayNameFromDS: display,
			Unit:              value.Config.Unit,
			Color:             map[string]any{"mode": "fixed", "fixedColor": "yellow"},
			Custom: map[string]any{
				"lineWidth":  0,
				"showPoints": "never",
				"hideFrom":   map[string]any{"legend": true, "tooltip": false, "viz": false},
			},
		}
		return bf
	}
	up := band("Golden upper", base+" · golden upper", upper)
	up.Config.Custom["fillBelowTo"] = lowerName
	up.Config.Custom["fillOpacity"] = 20
	f.Fields = append(f.Fields, up, band("Golden lower", lowerName, lower))
	return f
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// joinOnElapsed joins the frames into one on their Elapsed field (an outer
// join: a series has no value where another has a point of its own).
// Series lines span those gaps, so each stays one line. Nil without frames.
func joinOnElapsed(frames data.Frames) *data.Frame {
	if len(frames) == 0 {
		return nil
	}
	seen := map[float64]bool{}
	var xs []float64
	for _, f := range frames {
		for i := 0; i < f.Fields[0].Len(); i++ {
			x := f.Fields[0].At(i).(float64)
			if !seen[x] {
				seen[x] = true
				xs = append(xs, x)
			}
		}
	}
	sort.Float64s(xs)
	row := make(map[float64]int, len(xs))
	for i, x := range xs {
		row[x] = i
	}
	elapsed := data.NewField(elapsedField, nil, xs)
	elapsed.Config = frames[0].Fields[0].Config
	out := data.NewFrame("batch", elapsed)
	for _, f := range frames {
		for _, field := range f.Fields[1:] {
			joined := data.NewFieldFromFieldType(field.Type().NullableType(), len(xs))
			joined.Name, joined.Labels, joined.Config = field.Name, field.Labels, field.Config
			for i := 0; i < field.Len(); i++ {
				if v, ok := field.ConcreteAt(i); ok {
					joined.SetConcrete(row[f.Fields[0].At(i).(float64)], v)
				}
			}
			if joined.Config == nil {
				joined.Config = &data.FieldConfig{}
			}
			if joined.Config.Custom == nil {
				joined.Config.Custom = map[string]any{}
			}
			joined.Config.Custom["spanNulls"] = true
			out.Fields = append(out.Fields, joined)
		}
	}
	return out
}
