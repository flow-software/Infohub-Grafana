package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// InfoFolder is a folder of charts and dashboards (Flow's "information" tree).
type InfoFolder struct {
	ID     int    `json:"id"`
	Parent *Ref   `json:"parent"`
	Name   string `json:"name"`
	Order  *int   `json:"order"`
}

// ChartSummary is a chart as the public API lists it.
type ChartSummary struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"` // e.g. "Time Based Table"
	Folder *Ref   `json:"folder"`
}

func (c *Client) InfoFolders(ctx context.Context) ([]InfoFolder, error) {
	var r struct {
		Folders []InfoFolder `json:"folders"`
	}
	err := c.get(ctx, "v1/information/folders", nil, &r)
	return r.Folders, err
}

func (c *Client) InfoChildFolders(ctx context.Context, folderID int) ([]InfoFolder, error) {
	var r struct {
		Folders []InfoFolder `json:"folders"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/information/folders/%d/folders", folderID), nil, &r)
	return r.Folders, err
}

func (c *Client) FolderCharts(ctx context.Context, folderID int) ([]ChartSummary, error) {
	var r struct {
		Charts []ChartSummary `json:"charts"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/information/folders/%d/charts", folderID), nil, &r)
	return r.Charts, err
}

// ChartDefinition returns a chart's layout as the Flow web app loads it
// (api/definition/charts/{id}). It is not part of the public API, so it is
// returned as raw JSON for the caller to walk.
//
// Every call adds a ChartUsage row in Flow, so callers should cache it.
func (c *Client) ChartDefinition(ctx context.Context, chartID int) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.get(ctx, fmt.Sprintf("definition/charts/%d", chartID), nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// ChartPeriod is one column of a time-based chart.
type ChartPeriod struct {
	ID       int64  `json:"id"`
	Interval string `json:"it"`
	Start    Time   `json:"s"`
	End      Time   `json:"e"`
	// StartLabel is formatted by Flow in the server's time zone, e.g. "Tue 06 Oct".
	StartLabel string `json:"fs"`
	Day        int    `json:"dom"`
	Month      int    `json:"m"`
	Year       int    `json:"y"`
}

// ChartValueException flags a value outside one of its measure's limits.
type ChartValueException struct {
	LimitID int  `json:"lid"`
	High    bool `json:"h"`
	Low     bool `json:"l"`
}

// ChartComment is a comment someone added to a value in Flow.
type ChartComment struct {
	UserID *int   `json:"uid"`
	Time   *Time  `json:"t"`
	Text   string `json:"c"`
}

// ChartUser names the people the data refers to by id.
type ChartUser struct {
	ID   int    `json:"id"`
	Name string `json:"n"`
}

type ChartValue struct {
	MeasureID        int                   `json:"mid"`
	PeriodID         int64                 `json:"pid"`
	AttributeValueID *int64                `json:"avid"`
	Value            *float64              `json:"v"`
	Formatted        string                `json:"fv"`
	Quality          *int                  `json:"q"`
	Exceptions       []ChartValueException `json:"ex"`
	Comments         []ChartComment        `json:"c"`
}

type ChartLimitValue struct {
	MeasureLimitID int      `json:"mlid"`
	LimitID        int      `json:"lid"`
	AttributeValue *string  `json:"av"`
	TimeStamp      *Time    `json:"ts"`
	High           *float64 `json:"h"`
	Low            *float64 `json:"l"`
	Target         *float64 `json:"t"`
	FormattedHigh  string   `json:"fh"`
	FormattedLow   string   `json:"fl"`
	FormattedTgt   string   `json:"ft"`
}

// ChartData is what the Flow web app loads to draw a time-based chart
// (api/data/charts/{id}). Values and limits use Flow's short field names.
type ChartData struct {
	PeriodStart *Time             `json:"periodStart"`
	PeriodEnd   *Time             `json:"periodEnd"`
	Periods     []ChartPeriod     `json:"timePeriods"`
	Values      []ChartValue      `json:"measureValues"`
	LimitValues []ChartLimitValue `json:"measureLimitValues"`
	Users       []ChartUser       `json:"usernames"`
	// EventPeriods is only present for event-based charts.
	EventPeriods json.RawMessage `json:"eventPeriods"`
}

// ChartData reads a chart's data. With no times Flow uses the chart's own
// period setting (for example "today").
func (c *Client) ChartData(ctx context.Context, chartID int, from, to *time.Time) (*ChartData, error) {
	q := url.Values{}
	if from != nil {
		q.Set("periodStart", formatTime(*from))
	}
	if to != nil {
		q.Set("periodEnd", formatTime(*to))
	}
	var out ChartData
	if err := c.get(ctx, fmt.Sprintf("data/charts/%d", chartID), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
