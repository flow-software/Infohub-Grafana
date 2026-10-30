package plugin

import (
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// buildFrame makes one time series frame per measure: Time and Value, plus
// Quality when asked for. Values are numbers unless a tag only has text
// values, in which case the series is text.
func buildFrame(s series, info seriesInfo, qm queryModel) *data.Frame {
	n := len(s.points)
	times := make([]time.Time, n)
	qualities := make([]string, n)
	text := isText(s.points)

	var value *data.Field
	labels := seriesLabels(info)
	if s.attribute != "" {
		labels[s.attribute] = s.value
	}
	if text {
		vals := make([]*string, n)
		for i, p := range s.points {
			times[i], qualities[i] = p.t, qualityName(p.quality)
			if qm.BadQualityAsNull && !isGood(p.quality) {
				continue
			}
			vals[i] = p.text
			if vals[i] == nil && p.value != nil {
				str := strconv.FormatFloat(*p.value, 'f', -1, 64)
				vals[i] = &str
			}
		}
		value = data.NewField("Value", labels, vals)
	} else {
		vals := make([]*float64, n)
		for i, p := range s.points {
			times[i], qualities[i] = p.t, qualityName(p.quality)
			if qm.BadQualityAsNull && !isGood(p.quality) {
				continue
			}
			vals[i] = p.value
		}
		value = data.NewField("Value", labels, vals)
	}

	value.Config = &data.FieldConfig{
		DisplayNameFromDS: legendName(qm.Legend, info),
		Description:       info.Description,
	}
	name := info.Path
	if s.attribute != "" {
		value.Config.DisplayNameFromDS += " · " + s.value
		name += " · " + s.value
	}
	if s.color != "" {
		// Coloured as the attribute value is in Flow, like event tables and timelines.
		value.Config.Color = map[string]any{"mode": "fixed", "fixedColor": s.color}
	}
	if info.UOM != "" {
		// Grafana's custom-unit syntax; the space keeps "12 kg" readable.
		value.Config.Unit = "suffix: " + info.UOM
	}

	frame := data.NewFrame(name, data.NewField("Time", nil, times), value)
	withVersions := qm.IncludeVersion || qm.allVersions()
	if qm.IncludeQuality {
		frame.Fields = append(frame.Fields, data.NewField("Quality", labels, qualities))
	}
	withStatus := qm.LimitStatus && !s.tag && !text
	if withStatus {
		frame.Fields = append(frame.Fields, limitStatusField(s.points, labels))
	}
	if withVersions {
		// Text, so a time series panel does not draw the version numbers as
		// a series of their own; tables show them as they are.
		versions, types, users := make([]string, n), make([]string, n), make([]string, n)
		for i, p := range s.points {
			if p.version != nil {
				versions[i] = strconv.Itoa(*p.version)
			}
			types[i], users[i] = p.versionType, p.user
		}
		// Short column names, without the series labels.
		detail := func(f *data.Field) *data.Field {
			f.Config = &data.FieldConfig{DisplayNameFromDS: f.Name}
			return f
		}
		frame.Fields = append(frame.Fields,
			detail(data.NewField("Version", labels, versions)),
			detail(data.NewField("Version type", labels, types)),
			detail(data.NewField("Entered by", labels, users)),
		)
	}
	if !qm.IncludeQuality && !withVersions && !text && !withStatus {
		// A time field plus one numeric field is the multi-frame time series
		// shape that alerting and server-side expressions expect.
		frame.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti, TypeVersion: data.FrameTypeVersion{0, 1}}
	}
	return frame
}

// limitStatusField holds each value's limit status as Flow flags it, as
// text (so a time series panel does not draw it), coloured green within
// and red outside the limits. A bar chart colours its bars by it with
// Color by field, a table colours the cells, and a state timeline draws it.
func limitStatusField(points []point, labels data.Labels) *data.Field {
	vals := make([]*string, len(points))
	for i, p := range points {
		if p.limit != "" {
			v := p.limit
			vals[i] = &v
		}
	}
	f := data.NewField("Limit evaluation", labels, vals)
	color := func(status, c string, index int) data.ValueMapping {
		return data.ValueMapper{status: {Color: c, Index: index}}
	}
	f.Config = &data.FieldConfig{
		DisplayNameFromDS: "Limit evaluation",
		Mappings: data.ValueMappings{
			color(limitWithin, limitTargetColor, 0),
			color(limitAbove, limitBoundColor, 1),
			color(limitBelow, limitBoundColor, 2),
		},
		Custom: map[string]any{"cellOptions": map[string]any{"type": "color-background"}},
	}
	return f
}

// seriesLabels identify a series to alerting and to legend {{label}} syntax.
func seriesLabels(info seriesInfo) data.Labels {
	labels := data.Labels{"measureId": strconv.Itoa(info.ID), "path": info.Path}
	if info.Folder != "" {
		labels["folder"] = info.Folder
	}
	if info.Metric != "" {
		labels["metric"] = info.Metric
	}
	if info.Measure != "" {
		labels["measure"] = info.Measure
	}
	if info.TemplateID != 0 {
		labels["templateId"] = strconv.Itoa(info.TemplateID)
	}
	return labels
}

// isText reports a tag whose values are text: at least one text value and
// no numeric ones.
func isText(points []point) bool {
	hasText := false
	for _, p := range points {
		if p.value != nil {
			return false
		}
		hasText = hasText || p.text != nil
	}
	return hasText
}

// Flow stores OPC quality codes; the top two bits carry the status:
// 11 Good, 01 Uncertain, 00 Bad.
func qualityName(code int) string {
	switch code & 0xC0 {
	case 0xC0:
		return "Good"
	case 0x40:
		return "Uncertain"
	default:
		return "Bad"
	}
}

func isGood(code int) bool { return code&0xC0 == 0xC0 }
