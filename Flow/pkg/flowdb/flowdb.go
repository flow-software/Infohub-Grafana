// Package flowdb reads how far Flow's engine has got with measures, straight
// from the Flow database: the public API does not expose it. It runs the same
// sums as the monitor in Flow Config, for the measures it is given only, and
// only reads (with NOLOCK, as the monitor reads uncommitted).
package flowdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	// SQL Server driver, registered as "sqlserver".
	_ "github.com/microsoft/go-mssqldb"
)

// Config is a connection to the Flow database.
type Config struct {
	// Server is host, host:port or host\instance.
	Server   string
	Database string
	User     string
	Password string
	// Encrypt is the driver's encrypt option: "false" (encrypt the login
	// only, the default), "true", "strict" or "disable".
	Encrypt string
	// TrustServerCertificate accepts a self-signed SQL Server certificate.
	TrustServerCertificate bool
}

// ConnString builds the driver's URL-style connection string.
func (c Config) ConnString() string {
	host, instance, _ := strings.Cut(c.Server, `\`)
	q := url.Values{}
	q.Set("database", c.Database)
	encrypt := c.Encrypt
	if encrypt == "" {
		encrypt = "false"
	}
	q.Set("encrypt", encrypt)
	if c.TrustServerCertificate {
		q.Set("TrustServerCertificate", "true")
	}
	q.Set("app name", "Grafana Flow data source")
	u := url.URL{Scheme: "sqlserver", User: url.UserPassword(c.User, c.Password), Host: host, RawQuery: q.Encode()}
	if instance != "" {
		u.Path = "/" + instance
	}
	return u.String()
}

// DB is an open, pooled connection to the Flow database.
type DB struct{ db *sql.DB }

// Open prepares the pool; it does not connect until first used.
func Open(c Config) (*DB, error) {
	db, err := sql.Open("sqlserver", c.ConnString())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return &DB{db: db}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// Check connects and reads the tables the processing queries use, so the
// health check catches a wrong database or missing permissions.
func (d *DB) Check(ctx context.Context) (string, error) {
	var name string
	if err := d.db.QueryRowContext(ctx, "SELECT DB_NAME()").Scan(&name); err != nil {
		return "", err
	}
	for _, table := range []string{"Measure", "MeasureTimeScheme", "MeasureTimeSchemeRefresh", "TimeScheme", "TransitionTime", "MeasureTimePeriod"} {
		if _, err := d.db.ExecContext(ctx, "SELECT TOP 0 * FROM "+table+" WITH (NOLOCK)"); err != nil {
			return name, fmt.Errorf("cannot read %s in database %s: %w", table, name, err)
		}
	}
	return name, nil
}

// Processing is how far the engine has got with one measure.
type Processing struct {
	// Inactive marks a measure that is not deployed: the engine does not
	// process it, so it has no backlog to count.
	Inactive bool
	// Behind is the periods the engine has still to process up to now, over
	// all the measure's calendars, as the Flow Config monitor counts them.
	Behind int
	// BehindBy splits Behind by calendar, for calendars that are behind.
	BehindBy []CalendarBehind
	// Pending is periods marked for recalculation (Pending).
	Pending int
	// Queued is periods waiting to become pending (QueuePending, not
	// Pending). The engine promotes these every poll, so ones that stay
	// mean something is stuck.
	Queued int
}

type CalendarBehind struct {
	Calendar string
	Periods  int
}

// UpToDate reports an active measure with nothing left to process.
func (p Processing) UpToDate() bool {
	return !p.Inactive && p.Behind == 0 && p.Pending == 0 && p.Queued == 0
}

// behindSQL is the monitor's per-interval sum (UserControlMonitor's
// getIntervalCounts) for some measures: how many periods each refresh row is
// behind now, less the refresh offset, plus projected periods, in the
// calendar's offsets and the time zone transitions Flow stores. A row only
// counts once at least one whole period is due, as in the monitor.
const behindSQL = `
DECLARE @Now DATETIME2 = GETUTCDATE();
SET @Now = DATEADD(MINUTE, ISNULL((SELECT TOP 1 Offset FROM TransitionTime WITH (NOLOCK) WHERE @Now >= DateTime ORDER BY DateTime DESC), 0), @Now);
WITH ids AS (SELECT DISTINCT CAST([value] AS int) AS ID FROM OPENJSON(@ids)),
r AS (
	SELECT t0.MeasureID, t3.Name AS Calendar, t0.IntervalTypeID AS I, t3.MinuteInterval AS MI,
		DATEADD(MINUTE, ISNULL(t4.Offset, 0), t2.DateTime) AS S,
		DATEADD(SECOND, -t2.RefreshOffset, @Now) AS B,
		t0.Projected AS P, t3.MinuteOffset AS MO, t3.HourOffset AS HO, t3.DayOffset AS DO, t3.MonthOffset AS MonO
	FROM ids
	JOIN Measure t0 WITH (NOLOCK) ON t0.MeasureID = ids.ID
	JOIN MeasureTimeScheme t1 WITH (NOLOCK) ON t0.MeasureID = t1.MeasureID
	JOIN MeasureTimeSchemeRefresh t2 WITH (NOLOCK) ON t1.MeasureTimeSchemeID = t2.MeasureTimeSchemeID
	JOIN TimeScheme t3 WITH (NOLOCK) ON t1.TimeSchemeID = t3.TimeSchemeID
	OUTER APPLY (SELECT TOP 1 Offset FROM TransitionTime WITH (NOLOCK) WHERE t2.DateTime >= DateTime ORDER BY DateTime DESC) t4
	WHERE t0.Active = 1 AND t0.IntervalTypeID BETWEEN 1 AND 8
),
d AS (
	SELECT MeasureID, Calendar, I, MI,
		CASE I
			WHEN 1 THEN DATEDIFF(MINUTE, S, DATEADD(MINUTE, P * MI, B))
			WHEN 2 THEN DATEDIFF(HOUR, S, DATEADD(MINUTE, -MO, DATEADD(HOUR, P, B)))
			WHEN 3 THEN DATEDIFF(HOUR, S, DATEADD(MINUTE, -MO, DATEADD(HOUR, P * 8, B)))
			WHEN 4 THEN DATEDIFF(DAY, S, DATEADD(HOUR, -HO, DATEADD(MINUTE, -MO, DATEADD(DAY, P, B))))
			WHEN 5 THEN DATEDIFF(WEEK, S, DATEADD(DAY, -DO, DATEADD(HOUR, -HO, DATEADD(MINUTE, -MO, DATEADD(WEEK, P, B)))))
			WHEN 6 THEN DATEDIFF(MONTH, S, DATEADD(HOUR, -HO, DATEADD(MINUTE, -MO, DATEADD(MONTH, P, B))))
			WHEN 7 THEN DATEDIFF(MONTH, S, DATEADD(HOUR, -HO, DATEADD(MINUTE, -MO, DATEADD(MONTH, P * 3, B))))
			WHEN 8 THEN DATEDIFF(YEAR, S, DATEADD(MONTH, -MonO, DATEADD(HOUR, -HO, DATEADD(MINUTE, -MO, DATEADD(YEAR, P, B)))))
		END AS Diff
	FROM r
)
SELECT MeasureID, Calendar, SUM(CASE
		WHEN I = 1 AND MI > 0 AND Diff >= MI THEN Diff / MI
		WHEN I = 3 AND Diff >= 8 THEN Diff / 8
		WHEN I = 7 AND Diff >= 3 THEN Diff / 3
		WHEN I IN (2, 4, 5, 6, 8) AND Diff > 0 THEN Diff
		ELSE 0 END) AS Behind
FROM d
GROUP BY MeasureID, Calendar`

// pendingSQL counts the periods the engine still has to recalculate.
// Together, Pending and Queued are the monitor's pending count.
const pendingSQL = `
WITH ids AS (SELECT DISTINCT CAST([value] AS int) AS ID FROM OPENJSON(@ids))
SELECT t0.MeasureID,
	SUM(CASE WHEN t0.Pending = 1 THEN 1 ELSE 0 END) AS Pending,
	SUM(CASE WHEN t0.Pending = 0 AND t0.QueuePending = 1 THEN 1 ELSE 0 END) AS Queued
FROM ids
JOIN MeasureTimePeriod t0 WITH (NOLOCK) ON t0.MeasureID = ids.ID
JOIN Measure t1 WITH (NOLOCK) ON t1.MeasureID = t0.MeasureID
WHERE t1.Active = 1 AND (t0.Pending = 1 OR t0.QueuePending = 1)
GROUP BY t0.MeasureID`

// inactiveSQL lists the measures that are not deployed (Measure.Active = 0);
// the monitor's sums leave them out.
const inactiveSQL = `
WITH ids AS (SELECT DISTINCT CAST([value] AS int) AS ID FROM OPENJSON(@ids))
SELECT t0.MeasureID
FROM ids
JOIN Measure t0 WITH (NOLOCK) ON t0.MeasureID = ids.ID
WHERE t0.Active = 0`

// Processing reads the processing state of the given measures. Measures with
// nothing to report are left out of the map.
func (d *DB) Processing(ctx context.Context, ids []int) (map[int]Processing, error) {
	out := map[int]Processing{}
	if len(ids) == 0 {
		return out, nil
	}
	idsJSON, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	arg := sql.Named("ids", string(idsJSON))

	rows, err := d.db.QueryContext(ctx, behindSQL, arg)
	if err != nil {
		return nil, fmt.Errorf("periods behind: %w", err)
	}
	for rows.Next() {
		var id, behind int
		var calendar sql.NullString
		if err := rows.Scan(&id, &calendar, &behind); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if behind <= 0 {
			continue
		}
		p := out[id]
		p.Behind += behind
		p.BehindBy = append(p.BehindBy, CalendarBehind{Calendar: calendar.String, Periods: behind})
		out[id] = p
	}
	if err := closeRows(rows); err != nil {
		return nil, fmt.Errorf("periods behind: %w", err)
	}

	rows, err = d.db.QueryContext(ctx, inactiveSQL, arg)
	if err != nil {
		return nil, fmt.Errorf("inactive measures: %w", err)
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out[id] = Processing{Inactive: true}
	}
	if err := closeRows(rows); err != nil {
		return nil, fmt.Errorf("inactive measures: %w", err)
	}

	rows, err = d.db.QueryContext(ctx, pendingSQL, arg)
	if err != nil {
		return nil, fmt.Errorf("pending periods: %w", err)
	}
	for rows.Next() {
		var id, pending, queued int
		if err := rows.Scan(&id, &pending, &queued); err != nil {
			_ = rows.Close()
			return nil, err
		}
		p := out[id]
		p.Pending, p.Queued = pending, queued
		out[id] = p
	}
	if err := closeRows(rows); err != nil {
		return nil, fmt.Errorf("pending periods: %w", err)
	}
	return out, nil
}

func closeRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	return rows.Close()
}
