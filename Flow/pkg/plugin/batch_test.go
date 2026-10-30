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
	if !strings.HasSuffix(golden.Fields[2].Config.DisplayNameFromDS, " · golden") || golden.Fields[3].Config.Custom["fillBelowTo"] != golden.Fields[4].Config.DisplayNameFromDS {
		t.Errorf("golden config: %q, fillBelowTo %v", golden.Fields[2].Config.DisplayNameFromDS, golden.Fields[3].Config.Custom["fillBelowTo"])
	}
}
