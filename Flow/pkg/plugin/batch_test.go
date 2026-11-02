package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestParseBatch(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	w, err := parseBatch("2:1791244800000:1791248400000", now)
	if err != nil || w.eventID != 2 || w.running || w.end.Sub(w.start) != time.Hour {
		t.Errorf("closed batch = %+v, %v", w, err)
	}
	w, err = parseBatch("{2:1791244800000:,2:1:2}", now)
	if err != nil || !w.running || !w.end.Equal(now) {
		t.Errorf("running batch from a multi-value variable = %+v, %v", w, err)
	}
	for _, bad := range []string{"", "$batch", "2:x:3", "2:5:4"} {
		if _, err := parseBatch(bad, now); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// A batch query reads the batch's window and the golden batch's window,
// each on seconds since its own start, the golden values dashed with a
// band around them.
func TestBatchAgainstGolden(t *testing.T) {
	f, srv := newFakeFlow(t)
	var reads []string
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		reads = append(reads, start)
		// The batch ran 6 Oct 00:00-02:00, the golden batch 1 Oct 10:00-12:00.
		if strings.HasPrefix(start, "2026-10-06") {
			writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{
				{"start": "2026-10-06T00:00:00Z", "end": "2026-10-06T01:00:00Z", "value": 5},
				{"start": "2026-10-06T01:00:00Z", "end": "2026-10-06T02:00:00Z", "value": 8},
			}}}})
			return
		}
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{
			{"start": "2026-10-01T10:00:00Z", "end": "2026-10-01T11:00:00Z", "value": 4},
			{"start": "2026-10-01T11:00:00Z", "end": "2026-10-01T12:00:00Z", "value": 6},
		}}}})
	}
	d := testDatasource(srv)
	ms := func(y, m, day, h int) int64 {
		return time.Date(y, time.Month(m), day, h, 0, 0, 0, time.UTC).UnixMilli()
	}
	qm := queryModel{
		Items:    []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 300}},
		Batch:    fmt.Sprintf("2:%d:%d", ms(2026, 10, 6, 0), ms(2026, 10, 6, 2)),
		Golden:   fmt.Sprintf("2:%d:%d", ms(2026, 10, 1, 10), ms(2026, 10, 1, 12)),
		BandMode: bandPercent, Band: 50,
	}
	b, _ := json.Marshal(qm)
	// The dashboard's range is ignored: each batch is read over its own window.
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b, TimeRange: backend.TimeRange{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if len(reads) != 2 || !strings.HasPrefix(reads[0], "2026-10-06T00:00:00") || !strings.HasPrefix(reads[1], "2026-10-01T10:00:00") {
		t.Errorf("reads = %v", reads)
	}
	if len(resp.Frames) != 1 {
		t.Fatalf("frames = %d, want one joined frame", len(resp.Frames))
	}
	row := func(fr *data.Frame) string {
		var parts []string
		for i := 0; i < fr.Rows(); i++ {
			var cells []string
			for _, fl := range fr.Fields {
				switch v := fl.At(i).(type) {
				case float64:
					cells = append(cells, fmt.Sprint(v))
				case *float64:
					if v == nil {
						cells = append(cells, "-")
					} else {
						cells = append(cells, fmt.Sprint(*v))
					}
				}
			}
			parts = append(parts, strings.Join(cells, "/"))
		}
		return strings.Join(parts, " ")
	}
	fr := resp.Frames[0]
	if fr.Fields[0].Name != elapsedField {
		t.Errorf("x field = %s", fr.Fields[0].Name)
	}
	// Elapsed, batch value, golden value and the golden ±50 % band.
	if got := row(fr); got != "0/5/4/6/2 3600/8/6/9/3" {
		t.Errorf("rows = %s", got)
	}
	golden := fr
	// The golden line and its band take the batch line's colour.
	batchColor := fr.Fields[1].Config.Color["fixedColor"]
	if batchColor == nil || fr.Fields[2].Config.Color["fixedColor"] != batchColor || fr.Fields[3].Config.Color["fixedColor"] != batchColor {
		t.Errorf("colours: batch %v, golden %v, band %v", batchColor, fr.Fields[2].Config.Color["fixedColor"], fr.Fields[3].Config.Color["fixedColor"])
	}
	if !strings.HasSuffix(golden.Fields[2].Config.DisplayNameFromDS, " · golden") || golden.Fields[3].Config.Custom["fillBelowTo"] != golden.Fields[4].Config.DisplayNameFromDS {
		t.Errorf("golden config: %q, fillBelowTo %v", golden.Fields[2].Config.DisplayNameFromDS, golden.Fields[3].Config.Custom["fillBelowTo"])
	}
}

// Several golden batches make one reference per measure: their mean
// (dashed) and the envelope of their minimum and maximum, on a shared
// grid of seconds since each one's start.
func TestBatchAgainstSeveralGoldens(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		day, v0, v1 := start[:10], 0, 0
		switch day {
		case "2026-10-06": // the batch
			v0, v1 = 5, 8
		case "2026-10-01":
			v0, v1 = 4, 6
		case "2026-10-02":
			v0, v1 = 8, 10
		}
		h := start[11:13]
		next := fmt.Sprintf("%s:00:00Z", map[string]string{"00": "01", "10": "11"}[h])
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{
			{"start": day + "T" + h + ":00:00Z", "value": v0},
			{"start": day + "T" + next, "value": v1},
		}}}})
	}
	d := testDatasource(srv)
	ms := func(day, h int) int64 { return time.Date(2026, 10, day, h, 0, 0, 0, time.UTC).UnixMilli() }
	qm := queryModel{
		Items:  []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 300}},
		Batch:  fmt.Sprintf("2:%d:%d", ms(6, 0), ms(6, 2)),
		Golden: fmt.Sprintf("2:%d:%d,2:%d:%d", ms(1, 10), ms(1, 12), ms(2, 10), ms(2, 12)),
	}
	b, _ := json.Marshal(qm)
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b, MaxDataPoints: 5})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	fr := resp.Frames[0]
	if got := fr.Fields[2].Config.DisplayNameFromDS; !strings.HasSuffix(got, " · golden (mean of 2)") {
		t.Errorf("golden name = %q", got)
	}
	// At 0 s and 3600 s: batch, mean, max (upper), min (lower).
	cells := func(x float64) string {
		for i := 0; i < fr.Rows(); i++ {
			if fr.Fields[0].At(i).(float64) != x {
				continue
			}
			var parts []string
			for _, fl := range fr.Fields[1:] {
				if v := fl.At(i).(*float64); v != nil {
					parts = append(parts, fmt.Sprint(*v))
				} else {
					parts = append(parts, "-")
				}
			}
			return strings.Join(parts, "/")
		}
		return "no row"
	}
	if got := cells(0); got != "5/6/8/4" {
		t.Errorf("at 0 s = %s, want batch 5, mean 6, max 8, min 4", got)
	}
	if got := cells(3600); got != "8/8/10/6" {
		t.Errorf("at 3600 s = %s, want batch 8, mean 8, max 10, min 6", got)
	}
	// Halfway, the golden batches are read between their points.
	if got := cells(1800); got != "-/7/9/5" {
		t.Errorf("at 1800 s = %s, want mean 7, max 9, min 5", got)
	}
}

// Change since start makes every run start at 0: a counter's batch and
// golden runs compare by what they added, not where the counter stood.
func TestBatchChangeSinceStart(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		v0, v1 := 1000, 1030 // the batch: +30
		if strings.HasPrefix(start, "2026-10-01") {
			v0, v1 = 400, 420 // the golden batch: +20
		}
		day, h := start[:10], start[11:13]
		next := map[string]string{"00": "01", "10": "11"}[h]
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{
			{"start": day + "T" + h + ":00:00Z", "value": v0},
			{"start": day + "T" + next + ":00:00Z", "value": v1},
		}}}})
	}
	d := testDatasource(srv)
	ms := func(day, h int) int64 { return time.Date(2026, 10, day, h, 0, 0, 0, time.UTC).UnixMilli() }
	qm := queryModel{
		Items:       []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 300}},
		Batch:       fmt.Sprintf("2:%d:%d", ms(6, 0), ms(6, 2)),
		Golden:      fmt.Sprintf("2:%d:%d", ms(1, 10), ms(1, 12)),
		BatchValues: batchValuesChange,
	}
	b, _ := json.Marshal(qm)
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	fr := resp.Frames[0]
	var rows []string
	for i := 0; i < fr.Rows(); i++ {
		rows = append(rows, fmt.Sprintf("%v/%v/%v", fr.Fields[0].At(i), *fr.Fields[1].At(i).(*float64), *fr.Fields[2].At(i).(*float64)))
	}
	if got := strings.Join(rows, " "); got != "0/0/0 3600/30/20" {
		t.Errorf("rows = %s, want both runs from 0: batch +30, golden +20", got)
	}
}

// On the clock axis the batch keeps its real times and the golden run is
// laid from the batch's start, marked with how far it was moved.
func TestBatchClockAxis(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		day, h := start[:10], start[11:13]
		next := map[string]string{"00": "01", "10": "11"}[h]
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{
			{"start": day + "T" + h + ":00:00Z", "value": 1},
			{"start": day + "T" + next + ":00:00Z", "value": 2},
		}}}})
	}
	d := testDatasource(srv)
	ms := func(day, h int) int64 { return time.Date(2026, 10, day, h, 0, 0, 0, time.UTC).UnixMilli() }
	qm := queryModel{
		Items:     []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 300}},
		Batch:     fmt.Sprintf("2:%d:%d", ms(6, 0), ms(6, 2)),
		Golden:    fmt.Sprintf("2:%d:%d", ms(1, 10), ms(1, 12)),
		BatchAxis: batchAxisClock,
	}
	b, _ := json.Marshal(qm)
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	if len(resp.Frames) != 2 {
		t.Fatalf("frames = %d, want the batch and the golden run, not joined", len(resp.Frames))
	}
	times := func(fr *data.Frame) string {
		var out []string
		for i := 0; i < fr.Rows(); i++ {
			out = append(out, fr.Fields[0].At(i).(time.Time).Format("02 15:04"))
		}
		return strings.Join(out, ",")
	}
	batch, golden := resp.Frames[0], resp.Frames[1]
	if got := times(batch); got != "06 00:00,06 01:00" {
		t.Errorf("batch times = %s, want its real times", got)
	}
	if got := times(golden); got != "06 00:00,06 01:00" {
		t.Errorf("golden times = %s, want laid from the batch's start", got)
	}
	// Moved from 1 Oct 10:00 to 6 Oct 00:00: 4 days 14 hours.
	if got := golden.Meta.Custom.(map[string]any)["goldenOffsetMs"]; got != int64((4*24+14)*time.Hour/time.Millisecond) {
		t.Errorf("golden offset = %v", got)
	}
	if links := batch.Fields[1].Config.Links; len(links) != 1 || links[0].Title != "Zoom to this batch" || !strings.Contains(links[0].URL, fmt.Sprintf("from=%d", ms(6, 0)-6*60*1000)) {
		t.Errorf("zoom link = %+v", links)
	}
}

// On the % axis each run is scaled to its own length: a 2-hour batch and a
// 1-hour golden batch meet at 50 % halfway through both.
func TestBatchPercentAxis(t *testing.T) {
	f, srv := newFakeFlow(t)
	f.handlers["/api/v1/data/measures"] = func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("start")
		if strings.HasPrefix(start, "2026-10-06") { // the batch: 2 hours
			writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{
				{"start": "2026-10-06T00:00:00Z", "value": 1}, {"start": "2026-10-06T01:00:00Z", "value": 2},
			}}}})
			return
		}
		// The golden batch: 1 hour, a value every half hour.
		writeJSON(w, 200, map[string]any{"values": []map[string]any{{"id": 300, "data": []map[string]any{
			{"start": "2026-10-01T10:00:00Z", "value": 5}, {"start": "2026-10-01T10:30:00Z", "value": 6},
		}}}})
	}
	d := testDatasource(srv)
	ms := func(day, h, m int) int64 { return time.Date(2026, 10, day, h, m, 0, 0, time.UTC).UnixMilli() }
	qm := queryModel{
		Items:     []queryItem{{Tree: flow.TreeModel, Type: itemMeasure, ID: 300}},
		Batch:     fmt.Sprintf("2:%d:%d", ms(6, 0, 0), ms(6, 2, 0)),
		Golden:    fmt.Sprintf("2:%d:%d", ms(1, 10, 0), ms(1, 11, 0)),
		BatchAxis: batchAxisPercent,
	}
	b, _ := json.Marshal(qm)
	resp := d.query(context.Background(), backend.DataQuery{RefID: "A", JSON: b})
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	fr := resp.Frames[0]
	var xs []string
	for i := 0; i < fr.Rows(); i++ {
		xs = append(xs, fmt.Sprint(fr.Fields[0].At(i)))
	}
	// Batch at 0 and 50 % (0 h, 1 h of 2 h); golden at 0 and 50 % (0, 30 min of 1 h).
	if got := strings.Join(xs, ","); got != "0,50" {
		t.Errorf("x = %s, want both runs at 0 and 50 %%", got)
	}
	if fr.Fields[0].Config.Unit != "percent" {
		t.Errorf("x unit = %q", fr.Fields[0].Config.Unit)
	}
	// At 50 %: batch 2, golden 6.
	if b, g := *fr.Fields[1].At(1).(*float64), *fr.Fields[2].At(1).(*float64); b != 2 || g != 6 {
		t.Errorf("at 50 %%: batch %v, golden %v", b, g)
	}
}
