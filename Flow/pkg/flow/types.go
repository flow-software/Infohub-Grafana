package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Tree selects which half of the Flow model an id belongs to. The public API
// serves the same shapes for both under api/v1/model and api/v1/template.
type Tree string

const (
	TreeModel    Tree = "model"
	TreeTemplate Tree = "template"
)

func (t Tree) Valid() bool { return t == TreeModel || t == TreeTemplate }

// IntervalTimestamp is the interval of a measure that holds raw, timestamped
// values: Flow calls these tags, and they are read through data/tags. Every
// other interval (hourly, shiftly, daily, ...) is read per calendar period
// through data/measures.
const IntervalTimestamp = "timestamp"

// Ref is how Flow nests a related object: only its id.
type Ref struct {
	ID int `json:"id"`
}

type Folder struct {
	ID       int    `json:"id"`
	Parent   *Ref   `json:"parent"`
	Name     string `json:"name"`
	Order    *int   `json:"order"`
	Template *Ref   `json:"template"`
}

type Metric struct {
	ID       int    `json:"id"`
	Folder   *Ref   `json:"folder"`
	Name     string `json:"name"`
	Order    *int   `json:"order"`
	Template *Ref   `json:"template"`
}

type Measure struct {
	ID     int  `json:"id"`
	Metric *Ref `json:"metric"`
	Event  *Ref `json:"event"`
	Parent *Ref `json:"parent"`
	// Name is hierarchical: "<metric>.<parent measures>.<measure>".
	Name        string `json:"name"`
	UOM         string `json:"uom"`
	Description string `json:"description"`
	Interval    string `json:"interval"`
	Retrieval   string `json:"retrieval"`
	Template    *Ref   `json:"template"`
	// Metadata is only filled when asked for (metadata=true).
	Metadata []MeasureMetadata `json:"metadata"`
	// Fields are the measure's model attributes, only filled when asked
	// for (fields=true). Flow resolves values that refer to other
	// attributes before sending them.
	Fields []Field `json:"fields"`
}

// Field is one model attribute value, e.g. Line = "Line 1".
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Comment is a comment on a value, with who wrote it when.
type Comment struct {
	Comment  string `json:"comment"`
	DateTime *Time  `json:"datetime"`
	User     *User  `json:"user"`
}

// MeasureMetadata describes how a measure gets its values. Type is
// calculation, aggregation, tagTransform, manual, retrieval, limit or context; Data's keys
// depend on it:
//
//	calculation: "dependents" (input measure ids) and the formula, keyed by
//	             the date it applies from
//	aggregation: "measure" or "event" (the source), "type", "interval", "scaling"
//	tagTransform: a tag measure transforming another tag: "measure" (the
//	             source), "type" (e.g. Latch), "offset", "scaling"
//	manual:      "type"; with "SetToMeasure", "id" is the measure copied
//	context:     "calendars" and "events", the ids the measure has as context
//	limit:       one entry per measure limit: {<calendar or event name>:
//	             {<limit name>: {<date it applies from>: {comparator, high,
//	             target, low, highid, targetid, lowid}}}}; an *id is a
//	             measure the value comes from instead of a constant
//	retrieval:   "source" is the connection name, or "Tag" with "id" the tag
//	             measure read from; then the connection's properties by
//	             lower-cased label ("tag", "aggregation", ...)
type MeasureMetadata struct {
	Type string                     `json:"type"`
	Data map[string]json.RawMessage `json:"data"`
}

// Event is a Flow event (an event scheme in Flow's own terms): periods that
// start and end on triggers, each with attribute values such as a product.
type Event struct {
	ID       int    `json:"id"`
	Folder   *Ref   `json:"folder"`
	Name     string `json:"name"`
	Order    *int   `json:"order"`
	Template *Ref   `json:"template"`
	// Attributes are only filled when asked for (attributes=true).
	Attributes []EventAttribute `json:"attributes"`
}

type EventAttribute struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AttributeValue is one attribute's value on one event period. Color is
// the value's colour in Flow, e.g. "#59A14F".
type AttributeValue struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Value string `json:"value"`
}

// EventPeriod is one occurrence of an event. End is nil while it runs.
type EventPeriod struct {
	PeriodID   int64            `json:"periodid"`
	Start      *Time            `json:"start"`
	End        *Time            `json:"end"`
	Attributes []AttributeValue `json:"attributes"`
}

type EventSeries struct {
	ID   int           `json:"id"`
	Name string        `json:"name"`
	Data []EventPeriod `json:"data"`
}

type EventData struct {
	Errors       []string      `json:"errors"`
	Continuation *Time         `json:"continuation"`
	Values       []EventSeries `json:"values"`
}

// IsTag reports whether the measure holds raw timestamped values.
func (m Measure) IsTag() bool { return strings.EqualFold(m.Interval, IntervalTimestamp) }

type Calendar struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SearchObject struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Quality struct {
	Value int `json:"value"`
}

// PeriodValue is one measure value for one calendar period.
type PeriodValue struct {
	// Start is clamped to the requested start for the first period.
	Start    *Time    `json:"start"`
	End      *Time    `json:"end"`
	DateTime *Time    `json:"datetime"`
	Value    *float64 `json:"value"`
	// Formatted is the value in the measure's own format and the server's locale.
	Formatted string `json:"formatted"`
	Detail    *struct {
		Quality Quality `json:"quality"`
		Pending bool    `json:"pending"`
	} `json:"detail"`
	// User is who entered the value; only sent when asked for (users=true).
	User *User `json:"user"`
	// Attributes are the event period's attribute values, for values read
	// per event period (eventPeriods=true, attributes=true).
	Attributes []AttributeValue `json:"attributes"`
	// Comments are only sent when asked for (comments=true).
	Comments []Comment `json:"comments"`
	// Limits are the limits this value breaks, only sent when asked for
	// (exceptions=true) and only on values outside a limit.
	Limits []ValueLimit `json:"limits"`
}

// ValueLimit is a limit a value breaks: the limit's values and which side.
type ValueLimit struct {
	Values struct {
		High   *float64 `json:"high"`
		Target *float64 `json:"target"`
		Low    *float64 `json:"low"`
	} `json:"values"`
	Exception struct {
		High bool `json:"high"`
		Low  bool `json:"low"`
	} `json:"exception"`
}

type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

type MeasureSeries struct {
	ID   int           `json:"id"`
	Name string        `json:"name"`
	Data []PeriodValue `json:"data"`
}

type MeasureData struct {
	Errors       []string        `json:"errors"`
	Calendar     string          `json:"calendar"`
	Continuation *Time           `json:"continuation"`
	Values       []MeasureSeries `json:"values"`
}

// RawValue is one tag value: raw, or one aggregation bucket.
type RawValue struct {
	Start       *Time    `json:"start"`
	Duration    *int64   `json:"duration"`
	Value       *float64 `json:"value"`
	Formatted   string   `json:"formatted"`
	StringValue *string  `json:"stringvalue"`
	Quality     Quality  `json:"quality"`
}

type TagSeries struct {
	ID   int        `json:"id"`
	Name string     `json:"name"`
	Data []RawValue `json:"data"`
}

type TagData struct {
	Errors       []string    `json:"errors"`
	Continuation *Time       `json:"continuation"`
	Values       []TagSeries `json:"values"`
}

// Time decodes Flow's timestamps. The public API serialises UTC with a "Z",
// but a value without a zone is read as UTC too rather than failing the query.
type Time struct{ time.Time }

func (t *Time) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.9999999"} {
		if v, err := time.Parse(layout, s); err == nil {
			t.Time = v.UTC()
			return nil
		}
	}
	return fmt.Errorf("unrecognised time %q", s)
}
