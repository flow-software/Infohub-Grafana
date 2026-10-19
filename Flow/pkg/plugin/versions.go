package plugin

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// correctionColor marks corrected periods on a panel.
const correctionColor = "#FF9830"

// correctionsFrame marks each period that has more than one version with an
// annotation on the panel: how many versions, who made the latest, and every
// version, newest first. It is an annotations frame (data topic), so the
// panel draws it without a dashboard annotation query. Nil when nothing was
// corrected.
func correctionsFrame(preferred, superseded []series, infoOf func(int) seriesInfo, qm queryModel) *data.Frame {
	type key struct {
		id int
		t  time.Time
	}
	versions := map[key][]point{}
	for _, s := range superseded {
		for _, p := range s.points {
			k := key{s.id, p.t}
			versions[k] = append(versions[k], p)
		}
	}
	if len(versions) == 0 {
		return nil
	}
	for _, s := range preferred {
		for _, p := range s.points {
			if k := (key{s.id, p.t}); versions[k] != nil {
				versions[k] = append(versions[k], p)
			}
		}
	}
	keys := make([]key, 0, len(versions))
	for k := range versions {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if !keys[i].t.Equal(keys[j].t) {
			return keys[i].t.Before(keys[j].t)
		}
		return keys[i].id < keys[j].id
	})

	times := make([]time.Time, len(keys))
	titles, texts, colors := make([]string, len(keys)), make([]string, len(keys)), make([]string, len(keys))
	for i, k := range keys {
		vs := versions[k]
		sortNewestFirst(vs)
		name := legendName(qm.Legend, infoOf(k.id))
		latest := vs[0]
		title := fmt.Sprintf("%s: %d versions", name, len(vs))
		if latest.user != "" {
			title += " · latest by " + latest.user
		}
		if !latest.captured.IsZero() {
			title += ", " + timeText(latest.captured)
		}
		// Grafana shows annotation text as (sanitised) HTML: one version per
		// line, each escaped.
		lines := make([]string, len(vs))
		for j, p := range vs {
			lines[j] = html.EscapeString(versionLine(p))
		}
		times[i], titles[i], texts[i], colors[i] = k.t, title, strings.Join(lines, "<br>"), correctionColor
	}
	f := data.NewFrame("corrections",
		data.NewField("time", nil, times),
		data.NewField("title", nil, titles),
		data.NewField("text", nil, texts),
		data.NewField("color", nil, colors),
	)
	f.Meta = &data.FrameMeta{DataTopic: data.DataTopicAnnotations}
	return f
}

// versionLine describes one version for a correction's annotation, e.g.
// "v15 · 9 · Manual · Charl, 2026-10-19 10:26".
func versionLine(p point) string {
	parts := []string{"v?"}
	if p.version != nil {
		parts[0] = "v" + strconv.Itoa(*p.version)
	}
	parts = append(parts, pointText(p))
	if p.versionType != "" {
		parts = append(parts, p.versionType)
	}
	who := p.user
	if !p.captured.IsZero() {
		who = strings.TrimSpace(who + " " + timeText(p.captured))
	}
	if who != "" {
		parts = append(parts, who)
	}
	return strings.Join(parts, " · ")
}

// pointText is a value as text: its number, its text, or "empty".
func pointText(p point) string {
	switch {
	case p.value != nil:
		return strconv.FormatFloat(*p.value, 'f', -1, 64)
	case p.text != nil:
		return *p.text
	default:
		return "empty"
	}
}

// sortNewestFirst orders versions from the latest to the first.
func sortNewestFirst(vs []point) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := versionNumber(vs[i]), versionNumber(vs[j])
		if a != b {
			return a > b
		}
		return vs[i].captured.After(vs[j].captured)
	})
}

func versionNumber(p point) int {
	if p.version == nil {
		return 0
	}
	return *p.version
}

// historyFrame is every version of every period value in one table, like
// Flow's History tab: newest period first, and within a period the latest
// version first.
func historyFrame(preferred, superseded []series, infoOf func(int) seriesInfo, qm queryModel) *data.Frame {
	type row struct {
		id        int
		p         point
		preferred bool
	}
	var rows []row
	for _, s := range preferred {
		if s.tag {
			continue
		}
		for _, p := range s.points {
			rows = append(rows, row{s.id, p, true})
		}
	}
	for _, s := range superseded {
		for _, p := range s.points {
			rows = append(rows, row{s.id, p, false})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if !a.p.t.Equal(b.p.t) {
			return a.p.t.After(b.p.t)
		}
		if an, bn := legendName(qm.Legend, infoOf(a.id)), legendName(qm.Legend, infoOf(b.id)); an != bn {
			return an < bn
		}
		return versionNumber(a.p) > versionNumber(b.p)
	})
	n := len(rows)
	measures, qualities, types, users := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	periods := make([]time.Time, n)
	captured := make([]*time.Time, n)
	values := make([]*float64, n)
	preferredCol := make([]string, n)
	versions := make([]*int64, n)
	for i, r := range rows {
		measures[i] = legendName(qm.Legend, infoOf(r.id))
		periods[i], values[i] = r.p.t, r.p.value
		if r.preferred {
			preferredCol[i] = "Yes"
		}
		qualities[i], types[i], users[i] = qualityName(r.p.quality), r.p.versionType, r.p.user
		if r.p.version != nil {
			v := int64(*r.p.version)
			versions[i] = &v
		}
		if !r.p.captured.IsZero() {
			c := r.p.captured
			captured[i] = &c
		}
	}
	value := data.NewField("Value", nil, values)
	// Named outright: with one number column, Grafana would title it after
	// the frame.
	value.Config = &data.FieldConfig{DisplayNameFromDS: "Value"}
	return data.NewFrame("history",
		data.NewField("Measure", nil, measures),
		data.NewField("Period", nil, periods),
		value,
		data.NewField("Quality", nil, qualities),
		data.NewField("Preferred", nil, preferredCol),
		data.NewField("Version", nil, versions),
		data.NewField("Version type", nil, types),
		data.NewField("Captured", nil, captured),
		data.NewField("Entered by", nil, users),
	)
}

// hasTags reports whether any series is a tag.
func hasTags(all []series) bool {
	for _, s := range all {
		if s.tag {
			return true
		}
	}
	return false
}
