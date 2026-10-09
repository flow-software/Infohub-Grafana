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
	if info.UOM != "" {
		// Grafana's custom-unit syntax; the space keeps "12 kg" readable.
		value.Config.Unit = "suffix: " + info.UOM
	}

	frame := data.NewFrame(info.Path, data.NewField("Time", nil, times), value)
	if qm.IncludeQuality {
		frame.Fields = append(frame.Fields, data.NewField("Quality", labels, qualities))
	} else if !text {
		// A time field plus one numeric field is the multi-frame time series
		// shape that alerting and server-side expressions expect.
		frame.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti, TypeVersion: data.FrameTypeVersion{0, 1}}
	}
	return frame
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
