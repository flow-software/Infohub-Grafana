# Flow data source

Chart [Flow](https://www.flow-software.com) measures and tags in Grafana. Browse the Flow model by folder and metric, and pick model measures, or template measures to get every instance at once.

- **Model browser.** Walk the model folder by folder, or search by name or measure ID (`4304`, `#4304` or `id:4304`). Each measure shows its ID. Each level loads when you open it, so large models stay quick.
- **Templates.** Pick a template metric or measure to get one series for every model measure created from it. Series are named by their folder path, so `Line 1/Production/Good` and `Line 2/Production/Good` stay apart. New instances appear without editing the panel.
- **Measures and tags.** Period measures (hourly, shift, daily, ...) are read per calendar period. Tags (timestamp measures) are read raw or aggregated: by the panel interval, a fixed bucket, or calendar periods such as shifts.
- **Flow charts as tables.** Pick a time-based chart, such as a time-based table, and see it in Grafana as Flow lays it out: sections, measures, limits, one column per period, and cells coloured by the chart's limit colours.
- **Dependencies.** See how a measure is built: the measures it is calculated, aggregated or set from, and theirs in turn, or the other way round, what is built from it. Shown in Grafana's Node Graph panel.
- **Grafana alerting.** Queries run in the plugin backend, so they work in alert rules. Each measure is its own series labelled with its path and id, so one rule produces one alert instance per instance.

## Requirements

- Grafana 12.3 or later.
- A Flow Server whose Bootstrap address the Grafana server can reach.
- A Flow server token, created in Flow Config. Its access group decides which folders Grafana can see.

## Configuration

| Setting | Example | Notes |
| --- | --- | --- |
| URL | `http://flow-server:4501/flow-software/flow/instances/<InstanceID>/server` | The Flow Server API through Bootstrap. Use the instance ID (a GUID), not the instance name. A trailing `/api` is ignored, so the Swagger page address works too. |
| API token | | A Flow server token. Stored encrypted and only read by the plugin backend. |
| Default calendar | Default | Calendar for period measures when a query does not choose one. After **Save & test** you can pick it by name. |
| TLS | | **Skip certificate verification** for a self-signed Bootstrap certificate, or **Custom CA certificate**. |

**Save & test** checks the token, then calls the public API and reports how many calendars it found.

## Queries

Each query is **Measures**, **Chart** or **Dependencies**.

### Measures

Select **Browse** to open the model browser. The **Model** tab shows the folders, metrics and measures in your model. The **Templates** tab shows the template library. Tick metrics or measures and select **Apply**.

- A **metric** stands for all of its measures. Measures added to it later are picked up automatically.
- A **template** metric or measure returns every model measure created from it.
The options below the selection depend on what it holds. **Period measures** options appear only when it contains period measures, and **Tags** options only when it contains tags; each heading shows how many. Each chip shows what the item is (tag, hourly, daily, metric) and whether it comes from a template.

- **Calendar**: period measures are read in this calendar. The measure must have the calendar as context in Flow; measures that do not are listed as a warning on the panel. Choose whether each value is placed at the **period start** or **end**.
- **Tags** applies to timestamp measures only. *Auto* (the default) returns raw values when the panel can show them all. A tag with more values than that is averaged per panel interval, and the panel says so. *Raw values* returns every stored value. An aggregation (average, maximum, delta, time in state, ...) buckets values by the panel interval (**Auto**), a **Fixed** size such as `15m`, or **Calendar** periods such as shifts.
- **Legend** names each series. Pick a preset (*Full path*, the default; *Full path and unit*; *Folder and measure*; *Metric and measure*; *Measure*; *Measure and unit*; *Folder*), or type your own pattern with `{{path}}`, `{{folder}}`, `{{metric}}`, `{{measure}}`, `{{id}}` and `{{uom}}`. An empty unit or folder is left out cleanly. Template instances share their metric and measure names, so keep the folder or path in the name to tell them apart.
- **Quality field** adds each value's quality (Good, Uncertain, Bad). Leave it off for alert rules.
- **Bad quality as gaps** blanks values whose quality is not Good.

### Chart

Select **Choose chart** and pick a chart from Flow's chart folders. Time-based charts can be shown; others (event-based charts, widgets) are listed but greyed out.

The chart is laid out as Flow's time-based table: one row per measure, grouped by section, with the UOM and Low Limit, Limit and High Limit columns the chart shows, then one column per period in the dashboard's time range, labelled as Flow labels it. A value outside its limit takes the limit's high or low colour from the chart, using the exceptions Flow itself records. A value within its limit is green, unless the limit sets a target colour of its own.

A value with comments in Flow gets a small marker in the corner of its cell. Hover over the cell, or click the marker, to read the comments, each with who added it and when (in UTC) on the line below. The box grows to fit them.

The chart's measures, sections and limits come from Flow, so changing the chart in Flow changes the panel (within five minutes, as the plugin caches the layout). Rows split by event context are not shown yet.

### Dependencies

Pick measures with **Browse**; each becomes the root of a tree (a metric adds a tree for each of its measures). The plugin follows the links Flow records for each measure:

- **calculation**: the measures a calculation's formula uses,
- **aggregation**: the measure (or event) an aggregated measure is built from, with the aggregation, e.g. *sum, weekly*,
- **set to measure**: the measure a manual measure's default copies,
- **retrieval**: the Flow tag (a timestamp measure) a retrieved measure reads.

**Values**: each model measure's latest value in the dashboard's time range is included (the latest period with a value for period measures, in the chosen **Calendar** or the datasource's default; the last value for tags), in Flow's own format with the unit. Turn **Latest values** off to leave them out. Template measures hold no values. The calendar matters for weekly, monthly and yearly measures, whose periods depend on where the calendar's weeks and months start. A period measure that does not have the calendar as context in Flow shows a dash; click it for why.

**Names**: *Full name* titles each node with Flow's full name (*Metric.Parent.Measure*); *Measure name* with the measure's own name only (*WTD*), which keeps nested measures short.

**Direction**: *Built from* follows those links to a measure's inputs, then theirs, and so on. *Used by* goes the other way: the measures built from it. *Used by* reads how every measure in the model is built (cached for five minutes), so it takes longer on large models. **Depth** limits how many levels are followed (default 10). A measure that appears again on its own branch is a cycle, and the walk stops there.

**Output**: nodes and edges for Grafana's **Node graph** panel. Each measure appears once with its latest value in the circle and how it gets its values (its period, then its retrieval type, e.g. *hourly · calculated*) under its name, coloured by retrieval type, with arrows pointing the way values flow. A cycle shows as arrows both ways. Click a node for its measure ID, path, calculation, aggregation, data source (e.g. *Flow Data Simulator · 110-FQ-001.PV · Average*), who entered it (manual measures: who entered the latest value, or the last value entered in the range when Flow filled in the latest), the latest value's time, and a note when the depth limit cut its links short. Click an edge for its formula or aggregation.

Flow returns a server error for the metadata of some aggregated tag measures; those are named in a warning on the panel and their own inputs are not shown.

### Labels

Every series carries the labels `path`, `folder`, `metric`, `measure`, `measureId` and, for template instances, `templateId`. Use them in alert rule notifications or in legends.

### Limits

- One query reads up to 100,000 measure periods. Past that, the panel warns that values are missing; narrow the time range or select fewer measures.
- *Raw values* reads up to 200,000 values per tag, in pages of 10,000. A tag that records every second passes that in a little over two days.
- Calendar buckets need the calendar to have periods of that size in Flow. On some servers there are no shift periods unless a measure uses them, and the panel then warns that there is nothing to aggregate into.
- Event-period values are not supported yet.

## License

Apache License 2.0. Copyright 2026 Flow Software.
