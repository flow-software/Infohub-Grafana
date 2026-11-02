package plugin

import (
	"context"
	"fmt"
	"slices"
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

// batchValuesChange draws each run's change since its start.
const batchValuesChange = "change"

// sinceStart makes each series its change since the run's start: its first
// value is subtracted from every value, so every run starts at 0. A
// counter (an energy or flow total) then shows what this run used, which
// can be compared with the golden run's; its raw reading only shows where
// the counter happened to be. Limit lines and other fields are dropped:
// they do not shift with the values.
func sinceStart(frames data.Frames) {
	for _, f := range frames {
		if len(f.Fields) < 2 || f.Fields[1].Type() != data.FieldTypeNullableFloat64 {
			continue
		}
		value := f.Fields[1]
		var first *float64
		for i := 0; i < value.Len() && first == nil; i++ {
			first = value.At(i).(*float64)
		}
		if first == nil {
			continue
		}
		base := *first
		for i := 0; i < value.Len(); i++ {
			if v := value.At(i).(*float64); v != nil {
				d := *v - base
				value.Set(i, &d)
			}
		}
		if value.Config == nil {
			value.Config = &data.FieldConfig{}
		}
		value.Config.Description = strings.TrimSpace("Change since the batch's start. " + value.Config.Description)
		f.Fields = f.Fields[:2]
	}
}

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
	if qm.BatchValues == batchValuesChange {
		sinceStart(frames)
	}
	// Each measure keeps one colour, so its golden line (dashed) and band
	// pair visibly with its batch line, however many measures there are.
	colors := map[string]string{}
	for i, f := range frames {
		if len(f.Fields) > 1 {
			c := batchPalette[i%len(batchPalette)]
			colors[seriesKey(f.Fields[1])] = c
			setColor(f.Fields[1], c)
		}
	}
	if batch.running {
		notices = append(notices, "The batch is still running: it is shown up to now.")
	}

	if goldenValues := splitBatches(qm.Golden); len(goldenValues) > 0 {
		gm := base
		gm.Limits = ""
		// Each golden batch's frames, on seconds since its own start.
		var reads []data.Frames
		for _, value := range goldenValues {
			golden, err := parseBatch(value, now)
			if err != nil {
				return backend.DataResponse{}, err
			}
			qg := q
			qg.TimeRange = backend.TimeRange{From: golden.start, To: golden.end}
			gresp, err := d.run(ctx, gm, qg)
			if err != nil {
				return backend.DataResponse{}, fmt.Errorf("reading the golden batch: %w", err)
			}
			for _, n := range takeNotices(gresp.Frames) {
				// Several golden batches often say the same thing: once is enough.
				if !slices.Contains(notices, "Golden batch: "+n) {
					notices = append(notices, "Golden batch: "+n)
				}
			}
			read := elapsedFrames(gresp.Frames, golden.start)
			if qm.BatchValues == batchValuesChange {
				sinceStart(read)
			}
			reads = append(reads, read)
		}
		if len(reads) == 1 {
			for _, f := range reads[0] {
				color := ""
				if len(f.Fields) > 1 {
					color = colors[seriesKey(f.Fields[1])]
				}
				frames = append(frames, goldenFrame(f, qm, color))
			}
		} else {
			points := int(q.MaxDataPoints)
			if points <= 0 || points > 2000 {
				points = 1000
			}
			frames = append(frames, envelopeFrames(reads, qm, colors, points)...)
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
func goldenFrame(f *data.Frame, qm queryModel, color string) *data.Frame {
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
	bandColor := "yellow"
	if color != "" {
		setColor(value, color)
		bandColor = color
	}
	// Only the golden value itself; limits and evaluations belong to the batch.
	f.Fields = f.Fields[:2]
	if qm.Band <= 0 || value.Type() != data.FieldTypeNullableFloat64 {
		return f
	}
	n := value.Len()
	vals := make([]*float64, n)
	for i := range vals {
		vals[i] = value.At(i).(*float64)
	}
	upper, lower := widen(vals, vals, vals, qm)
	f.Fields = append(f.Fields, bandFields(value, upper, lower, bandColor)...)
	return f
}

// widen adds the query's band (± amount, or ± % of the reference value) to
// an upper and lower edge: around the golden values, or around the
// envelope of several golden batches.
func widen(ref, hi, lo []*float64, qm queryModel) (upper, lower []*float64) {
	upper, lower = make([]*float64, len(ref)), make([]*float64, len(ref))
	for i := range ref {
		if ref[i] == nil || hi[i] == nil || lo[i] == nil {
			continue
		}
		d := 0.0
		if qm.Band > 0 {
			d = qm.Band
			if qm.BandMode == bandPercent {
				d = abs(*ref[i]) * qm.Band / 100
			}
		}
		u, l := *hi[i]+d, *lo[i]-d
		upper[i], lower[i] = &u, &l
	}
	return upper, lower
}

// bandFields are a band's upper and lower edges, filled between, in the
// series' colour, left out of the legend.
func bandFields(value *data.Field, upper, lower []*float64, bandColor string) []*data.Field {
	base := strings.TrimSuffix(value.Config.DisplayNameFromDS, " · golden")
	lowerName := base + " · golden lower"
	band := func(field, display string, vals []*float64) *data.Field {
		bf := data.NewField(field, value.Labels, vals)
		bf.Config = &data.FieldConfig{
			DisplayNameFromDS: display,
			Unit:              value.Config.Unit,
			Color:             map[string]any{"mode": "fixed", "fixedColor": bandColor},
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
	return []*data.Field{up, band("Golden lower", lowerName, lower)}
}

// splitBatches reads the golden batch value: one batch, or several from a
// multi-value variable ("a,b", or "{a,b}").
func splitBatches(s string) []string {
	var out []string
	for _, part := range strings.Split(strings.Trim(strings.TrimSpace(s), "{}"), ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// envelopeFrames combine several golden batches into one reference per
// measure: on a shared grid of seconds since start, the mean of the golden
// batches (dashed) and their minimum and maximum (filled, widened by the
// band if any). Each golden batch is read between its own points
// (linearly) and only within its own run, so a shorter one adds nothing
// past its end.
func envelopeFrames(reads []data.Frames, qm queryModel, colors map[string]string, points int) data.Frames {
	type run struct{ xs, ys []float64 }
	runs := map[string][]run{}
	templates := map[string]*data.Frame{}
	var order []string
	maxX := 0.0
	for _, frames := range reads {
		for _, f := range frames {
			if len(f.Fields) < 2 || f.Fields[1].Type() != data.FieldTypeNullableFloat64 {
				continue
			}
			key := seriesKey(f.Fields[1])
			if _, ok := templates[key]; !ok {
				templates[key] = f
				order = append(order, key)
			}
			var r run
			for i := 0; i < f.Fields[0].Len(); i++ {
				if v := f.Fields[1].At(i).(*float64); v != nil {
					x := f.Fields[0].At(i).(float64)
					r.xs, r.ys = append(r.xs, x), append(r.ys, *v)
					maxX = max(maxX, x)
				}
			}
			if len(r.xs) > 0 {
				runs[key] = append(runs[key], r)
			}
		}
	}
	if maxX <= 0 || points < 2 {
		return nil
	}
	grid := make([]float64, points)
	for i := range grid {
		grid[i] = maxX * float64(i) / float64(points-1)
	}
	at := func(r run, x float64) (float64, bool) {
		j := sort.SearchFloat64s(r.xs, x)
		switch {
		case j < len(r.xs) && r.xs[j] == x:
			return r.ys[j], true
		case j == 0 || j == len(r.xs):
			return 0, false
		}
		x0, x1, y0, y1 := r.xs[j-1], r.xs[j], r.ys[j-1], r.ys[j]
		return y0 + (y1-y0)*(x-x0)/(x1-x0), true
	}
	var out data.Frames
	for _, key := range order {
		mean, lo, hi := make([]*float64, points), make([]*float64, points), make([]*float64, points)
		for i, x := range grid {
			n, sum, mn, mx := 0, 0.0, 0.0, 0.0
			for _, r := range runs[key] {
				if v, ok := at(r, x); ok {
					if n == 0 || v < mn {
						mn = v
					}
					if n == 0 || v > mx {
						mx = v
					}
					sum += v
					n++
				}
			}
			if n > 0 {
				m := sum / float64(n)
				mean[i], lo[i], hi[i] = &m, &mn, &mx
			}
		}
		tpl := templates[key]
		value := data.NewField(tpl.Fields[1].Name, tpl.Fields[1].Labels, mean)
		value.Config = &data.FieldConfig{}
		if tpl.Fields[1].Config != nil {
			c := *tpl.Fields[1].Config
			value.Config = &c
		}
		name := value.Config.DisplayNameFromDS
		if name == "" {
			name = tpl.Name
		}
		value.Config.DisplayNameFromDS = fmt.Sprintf("%s · golden (mean of %d)", name, len(reads))
		value.Config.Custom = map[string]any{"lineStyle": map[string]any{"fill": "dash", "dash": []int{10, 10}}}
		color := colors[key]
		bandColor := "yellow"
		if color != "" {
			setColor(value, color)
			bandColor = color
		}
		elapsed := data.NewField(elapsedField, nil, grid)
		elapsed.Config = tpl.Fields[0].Config
		f := data.NewFrame(tpl.Name+" · golden", elapsed, value)
		upper, lower := widen(mean, hi, lo, qm)
		// The band's names follow the batch line's, as with one golden batch.
		named := data.NewField(value.Name, value.Labels, []*float64{})
		named.Config = &data.FieldConfig{DisplayNameFromDS: name}
		f.Fields = append(f.Fields, bandFields(named, upper, lower, bandColor)...)
		for _, bf := range f.Fields[2:] {
			bf.Config.Unit = value.Config.Unit
		}
		out = append(out, f)
	}
	return out
}

// batchPalette is Grafana's classic palette, in its order: batch lines get
// the colours a panel would give them anyway, fixed so that each golden
// line can take its batch line's.
var batchPalette = []string{"#73BF69", "#F2CC0C", "#8AB8FF", "#FF780A", "#F2495C", "#5794F2", "#B877D9", "#705DA0", "#37872D", "#FADE2A"}

// seriesKey identifies a measure's series across the batch and golden
// reads: its labels (measure id, and the value of a split attribute).
func seriesKey(f *data.Field) string {
	return f.Labels.String()
}

func setColor(f *data.Field, color string) {
	if f.Config == nil {
		f.Config = &data.FieldConfig{}
	}
	f.Config.Color = map[string]any{"mode": "fixed", "fixedColor": color}
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
