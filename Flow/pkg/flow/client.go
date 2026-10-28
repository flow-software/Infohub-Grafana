// Package flow is a client for the Flow Server public API (api/v1), reached
// through Flow Bootstrap at .../flow-software/flow/instances/<InstanceID>/server.
package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client calls the public API with a bearer token. Flow issues long-lived
// server tokens from Flow Config, so there is nothing to renew.
type Client struct {
	// BaseURL ends at ".../server", without "/api".
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// APIError is a non-2xx answer. Flow reports every failure as a list of
// messages, and turns any server-side exception into a 400.
type APIError struct {
	Status   int
	Messages []string
}

func (e *APIError) Error() string {
	msg := strings.Join(e.Messages, "; ")
	switch {
	case e.Status == http.StatusUnauthorized:
		return "Flow rejected the API token (401); create a new server token in Flow Config"
	case e.Status == http.StatusForbidden && msg == "":
		return "Flow refused the request (403): the server may not be licensed"
	case msg == "":
		return fmt.Sprintf("Flow returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("Flow returned HTTP %d: %s", e.Status, msg)
}

func (e *APIError) HTTPStatus() int { return e.Status }

// HasMessage reports whether any message contains s.
func (e *APIError) HasMessage(s string) bool {
	for _, m := range e.Messages {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// get calls GET api/<path> and decodes the JSON body into out. A 204 (Flow's
// "nothing matched") leaves out untouched and returns nil.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.BaseURL + "/api/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &APIError{Status: resp.StatusCode}
		var e struct {
			Errors []string `json:"errors"`
		}
		if isJSON(resp) && json.Unmarshal(body, &e) == nil {
			apiErr.Messages = e.Errors
		}
		return apiErr
	}
	// token/validate answers 200 with an empty, untyped body.
	if out == nil || len(body) == 0 {
		return nil
	}
	if !isJSON(resp) {
		// Bootstrap answers unknown paths with the Flow web app's HTML page.
		return fmt.Errorf("the server answered %s with %q, not JSON: check the URL ends with /flow-software/flow/instances/<InstanceID>/server", path, resp.Header.Get("Content-Type"))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("could not read Flow's answer to %s: %w", path, err)
	}
	return nil
}

func isJSON(resp *http.Response) bool {
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

// Validate checks the token: 200 if Flow accepts it as a user or server key.
func (c *Client) Validate(ctx context.Context) error {
	return c.get(ctx, "token/validate", nil, nil)
}

func (c *Client) Calendars(ctx context.Context) ([]Calendar, error) {
	var r struct {
		Calendars []Calendar `json:"calendars"`
	}
	err := c.get(ctx, "v1/config/calendars", nil, &r)
	return r.Calendars, err
}

// Folders lists every folder in the tree, flat; roots have no parent.
func (c *Client) Folders(ctx context.Context, tree Tree) ([]Folder, error) {
	var r struct {
		Folders []Folder `json:"folders"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/%s/folders", tree), nil, &r)
	return r.Folders, err
}

func (c *Client) ChildFolders(ctx context.Context, tree Tree, folderID int) ([]Folder, error) {
	var r struct {
		Folders []Folder `json:"folders"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/%s/folders/%d/folders", tree, folderID), nil, &r)
	return r.Folders, err
}

// AllMetrics lists every metric in the tree the token can read, with its folder.
func (c *Client) AllMetrics(ctx context.Context, tree Tree) ([]Metric, error) {
	var r struct {
		Metrics []Metric `json:"metrics"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/%s/metrics", tree), nil, &r)
	return r.Metrics, err
}

func (c *Client) FolderMetrics(ctx context.Context, tree Tree, folderID int) ([]Metric, error) {
	var r struct {
		Metrics []Metric `json:"metrics"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/%s/folders/%d/metrics", tree, folderID), nil, &r)
	return r.Metrics, err
}

func (c *Client) FolderEvents(ctx context.Context, tree Tree, folderID int) ([]Event, error) {
	var r struct {
		Events []Event `json:"events"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/%s/folders/%d/events", tree, folderID), nil, &r)
	return r.Events, err
}

// AllEvents lists every event in the tree the token can read.
func (c *Client) AllEvents(ctx context.Context, tree Tree) ([]Event, error) {
	var r struct {
		Events []Event `json:"events"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/%s/events", tree), nil, &r)
	return r.Events, err
}

// Events looks events up by id, with their attributes. Ids Flow does not
// know are left out.
func (c *Client) Events(ctx context.Context, tree Tree, ids []int) ([]Event, error) {
	var all []Event
	for _, chunk := range chunks(ids, idChunk) {
		var r struct {
			Events []Event `json:"events"`
		}
		q := idValues("id", chunk)
		q.Set("attributes", "true")
		if err := c.get(ctx, fmt.Sprintf("v1/%s/events", tree), q, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Events...)
	}
	return all, nil
}

// EventDataRequest reads event periods with their attribute values. Template
// ids expand on the server to every model event created from that template.
type EventDataRequest struct {
	IDs, TemplateIDs []int
	From, To         time.Time
	// Limit caps periods across all events; Flow sets Continuation past it.
	Limit int
}

// EventData reads event periods. Flow answers 204 (nil, nil) when there are
// none in the range.
func (c *Client) EventData(ctx context.Context, r EventDataRequest) (*EventData, error) {
	q := url.Values{
		"start":      {formatTime(r.From)},
		"end":        {formatTime(r.To)},
		"attributes": {"true"},
	}
	addIDs(q, "id", r.IDs)
	addIDs(q, "templateid", r.TemplateIDs)
	if r.Limit > 0 {
		q.Set("limit", strconv.Itoa(r.Limit))
	}
	var out EventData
	if err := c.get(ctx, "v1/data/events", q, &out); err != nil {
		return nil, err
	}
	if out.Values == nil && out.Errors == nil {
		return nil, nil
	}
	return &out, nil
}

func (c *Client) MetricMeasures(ctx context.Context, tree Tree, metricID int) ([]Measure, error) {
	var r struct {
		Measures []Measure `json:"measures"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/%s/metrics/%d/measures", tree, metricID), nil, &r)
	return r.Measures, err
}

// idChunk keeps lookups by id well inside URL length limits.
const idChunk = 200

// Measures looks measures up by id. Ids Flow does not know are left out.
func (c *Client) Measures(ctx context.Context, tree Tree, ids []int) ([]Measure, error) {
	var all []Measure
	for _, chunk := range chunks(ids, idChunk) {
		var r struct {
			Measures []Measure `json:"measures"`
		}
		if err := c.get(ctx, fmt.Sprintf("v1/%s/measures", tree), idValues("id", chunk), &r); err != nil {
			return nil, err
		}
		all = append(all, r.Measures...)
	}
	return all, nil
}

// MeasuresWithMetadata looks measures up by id with their metadata. Flow
// fails the whole call with a 500 when any one measure's metadata cannot be
// built (seen on aggregated tag measures without an aggregation record), so
// a failing chunk is retried one id at a time; ids that still fail are
// returned in failed.
func (c *Client) MeasuresWithMetadata(ctx context.Context, tree Tree, ids []int) (found []Measure, failed []int, err error) {
	path := fmt.Sprintf("v1/%s/measures", tree)
	fetch := func(chunk []int) ([]Measure, error) {
		q := idValues("id", chunk)
		q.Set("metadata", "true")
		var r struct {
			Measures []Measure `json:"measures"`
		}
		err := c.get(ctx, path, q, &r)
		return r.Measures, err
	}
	for _, chunk := range chunks(ids, idChunk) {
		ms, err := fetch(chunk)
		if err == nil {
			found = append(found, ms...)
			continue
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status < 500 {
			return nil, nil, err
		}
		for _, id := range chunk {
			one, err := fetch([]int{id})
			switch {
			case err == nil:
				found = append(found, one...)
			case errors.As(err, &apiErr) && apiErr.Status >= 500:
				failed = append(failed, id)
			default:
				return nil, nil, err
			}
		}
	}
	return found, failed, nil
}

// AllMeasuresWithFields lists every measure in the tree the token can read,
// with its model attributes.
func (c *Client) AllMeasuresWithFields(ctx context.Context, tree Tree) ([]Measure, error) {
	var r struct {
		Measures []Measure `json:"measures"`
	}
	err := c.get(ctx, fmt.Sprintf("v1/%s/measures", tree), url.Values{"fields": {"true"}}, &r)
	return r.Measures, err
}

// AllMeasureIDs lists every measure in the tree the token can read.
func (c *Client) AllMeasureIDs(ctx context.Context, tree Tree) ([]int, error) {
	var r struct {
		Measures []Measure `json:"measures"`
	}
	if err := c.get(ctx, fmt.Sprintf("v1/%s/measures", tree), nil, &r); err != nil {
		return nil, err
	}
	ids := make([]int, len(r.Measures))
	for i, m := range r.Measures {
		ids[i] = m.ID
	}
	return ids, nil
}

// EventSchemeNames looks event scheme names up by id (an aggregation can
// read from an event scheme instead of a measure).
func (c *Client) EventSchemeNames(ctx context.Context, tree Tree, ids []int) (map[int]string, error) {
	var r struct {
		Events []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"events"`
	}
	if err := c.get(ctx, fmt.Sprintf("v1/%s/events", tree), idValues("id", ids), &r); err != nil {
		return nil, err
	}
	names := make(map[int]string, len(r.Events))
	for _, e := range r.Events {
		names[e.ID] = e.Name
	}
	return names, nil
}

// Metrics looks metrics up by id. Ids Flow does not know are left out.
func (c *Client) Metrics(ctx context.Context, tree Tree, ids []int) ([]Metric, error) {
	var all []Metric
	for _, chunk := range chunks(ids, idChunk) {
		var r struct {
			Metrics []Metric `json:"metrics"`
		}
		if err := c.get(ctx, fmt.Sprintf("v1/%s/metrics", tree), idValues("id", chunk), &r); err != nil {
			return nil, err
		}
		all = append(all, r.Metrics...)
	}
	return all, nil
}

// Search finds objects whose name contains text. types are Flow's
// SearchObjectType names, e.g. Metric, Measure, TemplateMetric.
func (c *Client) Search(ctx context.Context, text string, types []string, limit int) ([]SearchObject, error) {
	q := url.Values{"query": {text}, "match": {"name"}, "limit": {strconv.Itoa(limit)}}
	for _, t := range types {
		q.Add("type", t)
	}
	var r struct {
		Objects []SearchObject `json:"objects"`
	}
	err := c.get(ctx, "v1/search", q, &r)
	return r.Objects, err
}

// MeasureDataRequest reads calendar-period values. Template ids expand on the
// server to every model measure created from that template.
type MeasureDataRequest struct {
	IDs         []int
	TemplateIDs []int
	CalendarID  int
	From, To    time.Time
	// Limit caps the number of measure periods in the answer, across all
	// measures. Flow does not order them, so a truncated answer cannot be
	// paged reliably; callers ask for enough and report truncation.
	Limit int
	// Users asks for the user who entered each value (manual measures).
	Users bool
	// EventID reads one value per period of this event instead of per
	// calendar period, with each period's attributes. CalendarID is unused.
	EventID int
	// Comments asks for each value's comments, with their users.
	Comments bool
	// Exceptions asks for the limits each value breaks.
	Exceptions bool
	// AllVersions returns every version of each period's value, not only
	// the preferred one (preferred=false).
	AllVersions bool
}

func (c *Client) MeasureData(ctx context.Context, r MeasureDataRequest) (*MeasureData, error) {
	q := url.Values{
		"start": {formatTime(r.From)},
		"end":   {formatTime(r.To)},
	}
	if r.EventID > 0 {
		q.Set("eventPeriods", "true")
		q.Set("eventid", strconv.Itoa(r.EventID))
		q.Set("attributes", "true")
	} else {
		q.Set("calendarid", strconv.Itoa(r.CalendarID))
	}
	addIDs(q, "id", r.IDs)
	addIDs(q, "templateid", r.TemplateIDs)
	if r.Limit > 0 {
		q.Set("limit", strconv.Itoa(r.Limit))
	}
	if r.AllVersions {
		q.Set("preferred", "false")
	}
	if r.Users || r.Comments {
		q.Set("users", "true")
	}
	if r.Comments {
		q.Set("comments", "true")
	}
	if r.Exceptions {
		q.Set("exceptions", "true")
	}
	var out MeasureData
	if err := c.get(ctx, "v1/data/measures", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TagDataRequest reads timestamp measures, raw or aggregated.
type TagDataRequest struct {
	IDs         []int
	TemplateIDs []int
	From, To    time.Time
	// Aggregation is a Flow AggregationType name (average, maximum, ...);
	// empty reads raw values.
	Aggregation string
	// ResolutionMs buckets an aggregation into fixed-size intervals.
	ResolutionMs int64
	// Limit caps values per tag; Flow sets Continuation when it is reached.
	Limit int
}

func (c *Client) TagData(ctx context.Context, r TagDataRequest) (*TagData, error) {
	q := url.Values{
		"start": {formatTime(r.From)},
		"end":   {formatTime(r.To)},
	}
	addIDs(q, "tagid", r.IDs)
	addIDs(q, "templateid", r.TemplateIDs)
	if r.Aggregation != "" {
		q.Set("aggregation", r.Aggregation)
		if r.ResolutionMs > 0 {
			q.Set("resolution", strconv.FormatInt(r.ResolutionMs, 10))
		}
	}
	if r.Limit > 0 {
		q.Set("limit", strconv.Itoa(r.Limit))
	}
	var out TagData
	if err := c.get(ctx, "v1/data/tags", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// formatTime always sends UTC with a "Z": Flow reads a time without one as
// the server's local time.
func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func addIDs(q url.Values, key string, ids []int) {
	for _, id := range ids {
		q.Add(key, strconv.Itoa(id))
	}
}

func idValues(key string, ids []int) url.Values {
	q := url.Values{}
	addIDs(q, key, ids)
	return q
}

func chunks(ids []int, size int) [][]int {
	var out [][]int
	for len(ids) > size {
		out = append(out, ids[:size])
		ids = ids[size:]
	}
	if len(ids) > 0 {
		out = append(out, ids)
	}
	return out
}

// IsNoData reports Flow errors that only mean "nothing in this range": the
// measure endpoint fails with an empty-sequence exception when no period
// falls in the range, and the aggregated tag endpoint when no calendar period
// does.
// IsCalendarMismatch reports Flow refusing to read measures in a calendar
// none of them has as context.
func IsCalendarMismatch(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest && apiErr.HasMessage("Invalid measure and calendar combination")
}

func IsNoData(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		return false
	}
	return apiErr.HasMessage("Sequence contains no elements") || apiErr.HasMessage("No valid periods for the specified date range")
}
