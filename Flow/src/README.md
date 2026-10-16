# Flow data source

Chart [Flow](https://www.flow-software.com) measures, tags, events and charts in Grafana, and see how each measure is built. Browse the Flow model by folder and metric, or pick template measures to get every instance at once.

## Get started

1. Install the plugin and restart Grafana.
2. Add a **Flow** data source with the Flow Server URL and a server token (see [Configuration](#configuration)), then select **Save & test**.
3. Open the data source's **Dashboards** tab and import the example dashboards. They pick measures, events and charts from dropdowns, so they work on any Flow model. Open any of their panels to see how its query is set up.

| Dashboard                       | What it shows                                                                                                                           |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| **Flow: measure explorer**      | Pick any measure: its trend with a limit (pick more to compare) and comments, its latest value, what it is built from and what uses it. |
| **Flow: measures by attribute** | Pick a model attribute and its values (e.g. _Line_ = _Line 1_): the trend and latest values of every measure that carries them.         |
| **Flow: event analysis**        | Pick an event: its periods as a table and a timeline, and measures read per event period.                                               |
| **Flow: chart viewer**          | Pick a time-based Flow chart: it is shown as Flow lays it out, with limit colours and comments.                                         |

## What you can do

### Trend measures and tags, with limits and comments

![A measure's trend with its Flow limits as dashed lines and its value comments as markers](/public/plugins/%PLUGIN_ID%/img/screenshots/measures.png)

Period measures (hourly, shift, daily, ...) are read per calendar period; tags (timestamp measures) raw, averaged per panel interval only when there are more values than the panel can show. **Show limits** draws a measure's Flow limits as dashed lines (all of them, or the ones you pick), and an annotation query marks the comments on its values, with who wrote them and when.

Pick items in the query editor: **Browse** walks the model folder by folder, or searches by name or measure ID (`4304`, `#4304` or `id:4304`), and can show only measures of some intervals (tags, hourly, daily, ...) or retrieval types (calculated, manual, ...). Each measure shows its ID, interval and retrieval type as Flow Config draws them. A metric stands for all of its measures, and a template metric or measure returns every model measure created from it, named by folder path so `Line 1/Production/Good` and `Line 2/Production/Good` stay apart.

![The query editor: Measures tab with a measure variable and Show limits on](/public/plugins/%PLUGIN_ID%/img/screenshots/editor.png)

### See how a measure is built

![A dependency graph: each node is a measure with its latest value, its ring coloured by retrieval type](/public/plugins/%PLUGIN_ID%/img/screenshots/dependencies.png)

The **Dependencies** query draws what a measure is calculated, aggregated, transformed, set or retrieved from, and theirs in turn, or the other way round, what uses it. Each node shows the measure's latest value; its ring colour is the retrieval type, and a red fill means the latest value is outside its limits. Click a node for its calculation, data source, limits and who entered it. With the Flow database set up, **Show processing** adds how far the engine has got with each measure, as the Flow Config monitor counts it: periods behind, pending and queued, and rings that turn orange where the backlog is.

### Analyse events

![Event periods as a table and as a state timeline, coloured as in Flow](/public/plugins/%PLUGIN_ID%/img/screenshots/events.png)

Event periods with their attributes as a table or a state timeline, coloured as in Flow, or shaded as annotations on other panels. Period measures can also be read per event period instead of per calendar period, with a series per attribute value, e.g. one per product:

![A measure per event period, a series per product, drawn from each period's start to its end](/public/plugins/%PLUGIN_ID%/img/screenshots/event-context.png)

### Show Flow charts

![A Flow time-based chart shown as a table, as Flow lays it out](/public/plugins/%PLUGIN_ID%/img/screenshots/chart.png)

Pick a time-based chart set up in Flow and see it as Flow lays it out: sections, measures, attribute and limit columns, one column per period, cells coloured by the chart's limit colours, and comments on hover.

### Build one dashboard for every line

![Every measure where Line = Line 1, with a table of their latest values](/public/plugins/%PLUGIN_ID%/img/screenshots/attributes.png)

Dashboard variables are filled from Flow: model attributes (_Line_, _Site_, ...) and their values (every _Line_), measures, events, event attribute values and charts. Panels follow them through an attribute filter (_Line_ = `$line`) or a variable of measure, event or chart ids, so one dashboard serves every line, site or product.

### Alert on Flow data

Queries run in the plugin backend, so they work in Grafana alert rules. Each measure is its own series labelled with its path and ID, so one rule produces one alert instance per measure.

## Requirements

- Grafana 12.3 or later.
- A Flow Server whose Bootstrap address the Grafana server can reach.
- A Flow server token, created in Flow Config. Its access group decides which folders Grafana can see.

## Configuration

| Setting          | Example                                                                    | Notes                                                                                                                                                            |
| ---------------- | -------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| URL              | `http://flow-server:4501/flow-software/flow/instances/<InstanceID>/server` | The Flow Server API through Bootstrap. Use the instance ID (a GUID), not the instance name. A trailing `/api` is ignored, so the Swagger page address works too. |
| API token        |                                                                            | A Flow server token. Stored encrypted and only read by the plugin backend.                                                                                       |
| Default calendar | Default                                                                    | Calendar for period measures when a query does not choose one. After **Save & test** you can pick it by name.                                                    |
| TLS              |                                                                            | **Skip certificate verification** for a self-signed Bootstrap certificate, or **Custom CA certificate**.                                                         |

**Save & test** checks the token, then calls the public API and reports how many calendars it found.

### Flow database (optional)

Dependency queries can show how far the engine has got with each measure, as the monitor in Flow Config does. The public API does not expose this, so the plugin reads it from the Flow database, for the measures in a graph only, without locks (as the monitor reads). Leave **Server** empty to skip it; everything else works without it.

| Setting                  | Example           | Notes                                                                                                                                   |
| ------------------------ | ----------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Server                   | `sql-server:1433` | SQL Server host, `host:port` or `host\instance`, as the Grafana server reaches it.                                                      |
| Database                 | `Flow`            | The Flow database.                                                                                                                      |
| User and password        |                   | SQL Server authentication. Use a read-only login, e.g. one with `db_datareader` on the Flow database. The password is stored encrypted. |
| Encryption               | Login only        | _Login only_ (SQL Server's default), _Required_, _Strict_ (TDS 8) or _Off_.                                                             |
| Trust server certificate |                   | Accept a self-signed SQL Server certificate.                                                                                            |

With a server set, **Save & test** also connects to the database and checks that the tables the plugin reads are there.

## Queries

Each query is **Measures**, **Events**, **Chart** or **Dependencies**.

### Measures

Select **Browse** to open the model browser. The **Model** tab shows the folders, metrics and measures in your model. The **Templates** tab shows the template library. Tick metrics or measures and select **Apply**. The **Interval** and **Retrieval** chips under the search box show only measures of those intervals and retrieval types, in the tree and in search results (both apply together; **All** shows every one); folders and metrics stay, since what they hold shows once they are opened. Ticking a metric still picks all of its measures.

- A **metric** stands for all of its measures. Measures added to it later are picked up automatically.
- A **template** metric or measure returns every model measure created from it.

The options below the selection depend on what it holds. **Period measures** options appear only when it contains period measures, and **Tags** options only when it contains tags; each heading shows how many. Each chip shows what the item is (tag, hourly, daily, metric) and whether it comes from a template.

- **Variable**: a dashboard variable holding measure ids, such as a _Measures_ variable of this data source (`$measure`). Its measures are added to those picked with Browse; with Multi-value or All, every selected one.
- **Filter**: keep only measures whose model attribute has one of the values, e.g. _Line_ = _Line 1_. The attribute and its values can be dashboard variables: pick a template measure and _Line_ = `$line` to get one dashboard for every line.
- **Context**: _Calendar_ reads period measures per calendar period; _Event_ reads them per period of an event (see below).
- **Calendar**: period measures are read in this calendar. The measure must have the calendar as context in Flow; measures that do not are listed as a warning on the panel. Choose whether each value is placed at the **period start** or **end**.
- **Event** (event context): the event whose periods the measures are read in. The list holds the events the selected model measures have as context in Flow; **Event variable** takes it from a dashboard variable (`$event`) instead. Optionally pick an **Attribute** to keep only periods with some of its values (the values seen in the last 90 days are suggested; others can be typed), and turn on **Series per value** to get a series per value, coloured as in Flow. **Time at** _Whole period_ (the default in event context) draws each value as a line from its event period's start to its end, with a gap until the next period: unlike calendar periods, event periods do not run back to back, so a single timestamp would hide when each one ran. _Period start_ or _end_ places the value at one of them instead. Template measures are not read in event context, because each instance has its own event. Measures that belong to an event themselves (rather than to a metric) are not supported yet.
- **Tags** applies to timestamp measures only. By default the panel gets raw values when it can show them all; a tag with more values than that is averaged per panel interval, and the panel says so. Turn on **All raw values** to get every stored value (slow over long ranges). A tag Flow cannot read (for example a Latch transform without a latch source) is named in a warning, and the other tags still show.
- **Legend** names each series. Pick a preset (_Full path_, the default; _Full path and unit_; _Folder and measure_; _Metric and measure_; _Measure_; _Measure and unit_; _Folder_), or type your own pattern with `{{path}}`, `{{folder}}`, `{{metric}}`, `{{measure}}`, `{{id}}` and `{{uom}}`. An empty unit or folder is left out cleanly. Template instances share their metric and measure names, so keep the folder or path in the name to tell them apart.
- **Quality field** adds each value's quality (Good, Uncertain, Bad). Leave it off for alert rules.
- **Bad quality as gaps** blanks values whose quality is not Good.
- **Show limits** draws the measures' limits from Flow as dashed lines: bounds red, targets green. Limits belong to a calendar in Flow, so only the limits of the query's calendar (or event) are offered and drawn, as they apply now; a tag, read without a calendar, uses the limits it has. A measure can have several limits (e.g. Site, Zone, Moonshot): pick which to draw. Switching on picks the first; with none picked, all are drawn. The legend's tooltip lists the drawn limits, e.g. _Site < 5_. A limit that takes its value from another measure is listed but not drawn.

To mark the comments on measures' values on a dashboard, add an annotation query in the dashboard settings (Annotations) with this data source, switch it to **Measures** and pick them (or give a measure **Variable**). Each comment becomes a marker at its value's time, with who wrote it and when.

### Events

Pick events with **Browse** (only folders that hold events, directly or further down, are listed; a template event returns the periods of every event created from it), or give a **Variable** holding event ids (`$event`), then a **Format**:

- _Table_: one row per event period, newest first: event, start, end (empty while it runs), duration (to now while it runs) and a column per attribute, with cells coloured by the attribute value's colour in Flow. Use the Table panel.
- _Timeline_: a band per attribute, showing each period's value from its start to its end; use the State timeline panel. An event without attributes shows when it ran.

To shade event periods on other panels, add an annotation query in the dashboard settings (Annotations) with this data source. It starts as an events query: pick the events, and each period becomes a region on the dashboard's time series panels, titled with the event and labelled with its attributes. The regions take the annotation query's colour.

Up to 10,000 event periods are read per query; past that the panel warns.

### Variables

Add a _Query_ variable with this data source and choose what it lists:

- _Attributes_: the model attributes the measures carry, e.g. _Line_ and _Site_. Use it as `$attribute` in an _Attribute values_ variable and a panel's **Filter**, so people pick the attribute too.
- _Attribute values_: the values of a model attribute, e.g. every _Line_. Optionally only on some measures (template items stand for their instances).
- _Measures_: measures by folder path, valued by measure id: the instances of template measures or metrics, a model metric's measures, or with nothing picked every measure in the model. Use it with a panel's **Variable** field.
- _Events_: events by folder path, valued by event id; optionally only the instances of template events. Use it with an events query's **Variable** or a measures query's **Event variable**.
- _Event values_: the values of an event attribute seen in the last 90 days.
- _Limits_: the limit names (e.g. _Site_) of some measures, or of a measure variable such as `$measure`, in the default calendar. Use it as `$limit` in a panel's limit picker, so people choose which limits are drawn.
- _Charts_: the time-based charts in Flow's chart folders, by folder path, valued by chart id. Use it with a chart query's **Variable**.

_Attribute values_ and _Measures_ can be narrowed with **Where** (a model attribute and values), which can use other variables, so lists can follow each other: a _Line_ variable, then the measures where _Line_ = `$line`. Model attributes are read for the whole model in one call and kept for five minutes.

### Chart

Select **Choose chart** and pick a chart from Flow's chart folders, or give a **Variable** holding a chart id (`$chart`). Time-based charts can be shown; others (event-based charts, widgets) are listed but greyed out.

The chart is laid out as Flow's time-based table: one row per measure, grouped by section, with the UOM, model attribute and Low Limit, Limit and High Limit columns the chart shows (a measure without a value for a model attribute gets a blank cell), then one column per period in the dashboard's time range, labelled as Flow labels it. A value outside its limit takes the limit's high or low colour from the chart, using the exceptions Flow itself records. A value within its limit is green, unless the limit sets a target colour of its own.

A value with comments in Flow gets a small marker in the corner of its cell. Hover over the cell, or click the marker, to read the comments, each with who added it and when (in UTC) on the line below. The box grows to fit them.

The chart's measures, sections and limits come from Flow, so changing the chart in Flow changes the panel (within five minutes, as the plugin caches the layout). Rows split by event context are not shown yet.

### Dependencies

Pick measures with **Browse** or a measure **Variable**; each becomes the root of a tree (a metric adds a tree for each of its measures). The plugin follows the links Flow records for each measure:

- **calculation**: the measures a calculation's formula uses,
- **aggregation**: the measure (or event) an aggregated measure is built from, with the aggregation, e.g. _sum, weekly_,
- **transform**: the tag a tag transform (Latch, Exception, ...) is built from,
- **set to measure**: the measure a manual measure's default copies,
- **retrieval**: the Flow tag (a timestamp measure) a retrieved measure reads.

**Values**: each model measure's latest value in the dashboard's time range is included (the latest period with a value for period measures, in the chosen **Calendar** or the datasource's default; the last value for tags), in Flow's own format with the unit. Turn **Latest values** off to leave them out. Template measures hold no values. The calendar matters for weekly, monthly and yearly measures, whose periods depend on where the calendar's weeks and months start. A period measure that does not have the calendar as context in Flow shows a dash; click it for why.

**Processing** (needs the [Flow database](#flow-database-optional)): **Show processing** adds how far the engine has got with each model measure, read from the Flow database with the same sums as the Flow Config monitor:

- **behind**: the periods still to process up to now, per calendar, from where the engine has got to (its refresh point), the refresh offset and projected periods;
- **pending**: periods marked for recalculation;
- **queued**: periods waiting to become pending. The engine promotes these on every poll, so queued periods that stay mean something is stuck.

The counts lead each node's subtitle (e.g. _90 behind · daily · retrieved_), and the node menu has them in full, e.g. _90 periods behind (Production 90)_, or _Up to date_. **Ring colour** _Processing_ colours each ring green when up to date, orange when behind or pending, and grey for what the monitor does not track (tags, events, templates). Quarantined measures are not shown: Flow keeps quarantine in the engine's memory, not in the database. Inactive measures count as up to date, as in the monitor.

**Names**: _Full name_ titles each node with Flow's full name (_Metric.Parent.Measure_); _Measure name_ with the measure's own name only (_WTD_), which keeps nested measures short.

**Direction**: _Built from_ follows those links to a measure's inputs, then theirs, and so on. _Used by_ goes the other way: the measures built from it. _Used by_ reads how every measure in the model is built (cached for five minutes), so it takes longer on large models. **Depth** limits how many levels are followed (default 10). A measure that appears again on its own branch is a cycle, and the walk stops there.

**Output**: nodes and edges for Grafana's **Node graph** panel. Each measure appears once with its latest value in the circle and how it gets its values (its period, then its retrieval type, e.g. _hourly · calculated_) under its name, with arrows pointing the way values flow. Each node's ring is coloured by its retrieval type (calculated blue, aggregated orange, manual purple, retrieved green, replicated light blue; events grey). With **Show limits** on, a node whose latest value is outside its limits in the chosen calendar, as Flow flags it, is **filled red**, and its click menu names the limit it breaks. The panel's legend lists the ring colours in the graph and explains the red fill. A cycle shows as arrows both ways. Click a node for its measure ID, path, calculation, aggregation or transform, data source (e.g. _Flow Data Simulator · 110-FQ-001.PV · Average_), who entered it (manual measures: who entered the latest value, or the last value entered in the range when Flow filled in the latest), the latest value's time, and a note when the depth limit cut its links short. Click an edge for its formula, aggregation or transform.

Older Flow servers return a server error for the metadata of tag transforms; those are named in a warning on the panel and their own inputs are not shown.

### Labels

Every series carries the labels `path`, `folder`, `metric`, `measure`, `measureId` and, for template instances, `templateId`. Use them in alert rule notifications or in legends.

### Limits

- One query reads up to 100,000 measure periods. Past that, the panel warns that values are missing; narrow the time range or select fewer measures.
- _All raw values_ reads up to 200,000 values per tag, in pages of 10,000. A tag that records every second passes that in a little over two days.
- Values of measures that belong to an event are not supported yet.

## License

Apache License 2.0. Copyright 2026 Flow Software.
