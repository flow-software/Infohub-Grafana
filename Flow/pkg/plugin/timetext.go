package plugin

import (
	"fmt"
	"time"
)

// timeText marks an instant inside text meant for people: node menus,
// annotation and comment tooltips. Grafana does not tell the backend the
// dashboard's time zone, so the frontend replaces each mark with the time
// in that zone, as panels show their time axes. Alert rules, which skip the
// frontend, would see the mark itself; none of this text is used there.
func timeText(t time.Time) string {
	return fmt.Sprintf("{{time:%d}}", t.UnixMilli())
}

// periodText is a period as "start – end", or "start – running" while it
// has no end yet.
func periodText(start time.Time, end *time.Time) string {
	if end == nil {
		return timeText(start) + " – running"
	}
	return timeText(start) + " – " + timeText(*end)
}
