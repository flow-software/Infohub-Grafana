package plugin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/flow-software/Infohub-Grafana/Flow/pkg/flow"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// annotateComments makes a measures query return its value comments as
// annotations.
const annotateComments = "comments"

// valueComment is one comment on one measure value.
type valueComment struct {
	measureID int
	at        time.Time // the value's period start (or end, with Time at end)
	text      string
	user      string
	written   *time.Time
}

// runComments reads the comments on the selected period measures' values,
// in their calendar or event context, as an annotation frame: each comment
// is a marker at its value's time, titled with the measure, with the
// comment, its author and when it was written as text. Tags have no
// comments in Flow.
func (d *Datasource) runComments(ctx context.Context, qm queryModel, q backend.DataQuery) (backend.DataResponse, error) {
	sel, err := d.resolve(ctx, qm.Items)
	if err != nil {
		return backend.DataResponse{}, err
	}
	notices := append([]string(nil), sel.warnings...)
	if len(sel.tagIDs)+len(sel.tagTemplateIDs) > 0 {
		notices = append(notices, "Tags have no comments in Flow; only period measures are shown.")
	}
	var comments []valueComment
	if len(sel.periodIDs)+len(sel.periodTemplateIDs) > 0 {
		req := flow.MeasureDataRequest{IDs: sel.periodIDs, TemplateIDs: sel.periodTemplateIDs, From: q.TimeRange.From, To: q.TimeRange.To, Limit: periodLimit, Comments: true}
		if qm.Context == contextEvent && qm.EventID > 0 {
			req.EventID = qm.EventID
			req.TemplateIDs = nil
		} else {
			if req.CalendarID, err = d.calendarFor(ctx, qm.CalendarID); err != nil {
				return backend.DataResponse{}, err
			}
		}
		res, err := d.client.MeasureData(ctx, req)
		switch {
		case flow.IsNoData(err):
		case err != nil:
			return backend.DataResponse{}, err
		case res != nil:
			for _, s := range res.Values {
				for _, p := range s.Data {
					t := p.Start
					if qm.TimeAt == "end" && p.End != nil {
						t = p.End
					}
					if t == nil {
						continue
					}
					for _, c := range p.Comments {
						if strings.TrimSpace(c.Comment) == "" {
							continue
						}
						vc := valueComment{measureID: s.ID, at: t.Time, text: c.Comment, user: userName(c.User)}
						if c.DateTime != nil {
							w := c.DateTime.Time
							vc.written = &w
						}
						comments = append(comments, vc)
					}
				}
			}
		}
	}

	if qm.AttributeFilter != "" && len(qm.AttributeValues) > 0 && len(comments) > 0 {
		measures, err := d.modelMeasures(ctx)
		if err != nil {
			return backend.DataResponse{}, err
		}
		kept := comments[:0]
		for _, c := range comments {
			if m, ok := measures[c.measureID]; ok && attributeMatch(m, qm.AttributeFilter, qm.AttributeValues) {
				kept = append(kept, c)
			}
		}
		comments = kept
	}

	var ids []int
	for _, c := range comments {
		ids = appendUnique(ids, c.measureID)
	}
	infos, err := d.index.describe(ctx, ids)
	if err != nil {
		notices = append(notices, "Could not look up measure names: "+err.Error())
		infos = map[int]seriesInfo{}
	}
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].at.Before(comments[j].at) })

	n := len(comments)
	times, titles, texts, tags := make([]time.Time, n), make([]string, n), make([]string, n), make([]string, n)
	for i, c := range comments {
		info, ok := infos[c.measureID]
		if !ok {
			info = seriesInfo{Path: fmt.Sprintf("measure %d", c.measureID), Measure: fmt.Sprintf("measure %d", c.measureID)}
		}
		times[i], titles[i], tags[i] = c.at, info.Path, info.Measure
		var by []string
		if c.user != "" {
			by = append(by, c.user)
		}
		if c.written != nil {
			by = append(by, timeText(*c.written))
		}
		texts[i] = c.text
		if len(by) > 0 {
			texts[i] += "\n— " + strings.Join(by, ", ")
		}
	}
	frame := data.NewFrame("comments",
		data.NewField("time", nil, times),
		data.NewField("title", nil, titles),
		data.NewField("text", nil, texts),
		data.NewField("tags", nil, tags),
	)
	if len(notices) > 0 {
		frame.Meta = &data.FrameMeta{Notices: noticesOf(notices)}
	}
	return backend.DataResponse{Frames: data.Frames{frame}}, nil
}

// filterByAttribute keeps the series whose measure has one of the values of
// the model attribute. Measures Flow did not list (or that are newer than
// the cached list) are left out. warn explains an empty result.
func (d *Datasource) filterByAttribute(ctx context.Context, all []series, name string, values []string) ([]series, string, error) {
	measures, err := d.modelMeasures(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("reading model attributes: %w", err)
	}
	var kept []series
	for _, s := range all {
		if m, ok := measures[s.id]; ok && attributeMatch(m, name, values) {
			kept = append(kept, s)
		}
	}
	warn := ""
	if len(all) > 0 && len(kept) == 0 {
		warn = fmt.Sprintf("None of the selected measures has %s = %s.", name, strings.Join(values, " or "))
	}
	return kept, warn, nil
}
