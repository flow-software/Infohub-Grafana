# Changelog

## Unreleased

- Dependencies: each node's menu shows its latest value's version (number, type, who and when) and its retrieval type. **Ring colour** _Versions_ colours each node by how its latest value came about (green the original v1, blue updated by the engine, purple entered by hand), with the version number leading the subtitle. With retrieval-type rings the subtitle no longer repeats the retrieval type.
- Value versions: **Version field** adds each period value's version, type (original, updated, manual, ...) and who entered it. _Mark corrections_ marks every period with more than one version on the time axis, listing its versions in the mark's tooltip, newest first; _History table_ returns every version in one table, like Flow's History tab, with when each was captured.
- Charts can be shown as their **measures**: **Show** _Measures_ in the Chart tab reads the measures in any chart, of any chart type (event-based tables and forms, widgets, trends), as series for any panel, with every option of the Measures tab. _As configured_ is the Flow layout as before. Template measures in a chart come back as their instances, and an event-based chart starts in its event's context.
- A measures query with no calendar picked, whose measures Flow cannot read in the default calendar, now reads them in the calendar they have and says so, instead of failing.

## 1.6.0

- The plugin is now **Infohub by Flow**, with the ID `flowsoftware-infohub-datasource` (was `flowsoftware-flow-datasource`) and the backend `gpx_infohub`. Grafana treats it as a new plugin: install it into the folder `flowsoftware-infohub-datasource`, allow that ID in `allow_loading_unsigned_plugins`, and recreate data sources and dashboards made with the old one. The example dashboards are now _Infohub: measure explorer_, _Infohub: measures by attribute_, _Infohub: event analysis_ and _Infohub: chart viewer_.
- Dependencies can show how far the engine has got with each measure, as the Flow Config monitor counts it: periods behind, pending, and queued to become pending. The counts lead each node's subtitle and are in full in its menu, and **Ring colour** _Processing_ colours rings green (up to date), orange (behind or pending) or yellow (inactive: a measure that is not deployed, which the engine does not process). It is read from the Flow database, for the graph's measures only, so it needs the new, optional **Flow database** connection in the data source settings (server, database, read-only login); **Save & test** checks it. Quarantine is not included: Flow keeps it in the engine, not the database.
- The measure browser is titled **Browse Infohub**, and the chart picker **Choose an Infohub chart**.
- The editor offers only the calendars the selected measures have as context in Flow (noting how many measures have each when not all do), for measures and dependencies, and offers event context only when a selected measure has an event as context. With measures from a variable, or when Flow's context cannot be read, everything is offered as before.
- The measure browser can also show only measures of some retrieval types: a second row of chips (Aggregated, Calculated, Manual, Retrieved, Replicated, with Flow's retrieval marks) narrows the tree and the search results, together with the interval chips.

## 1.5.0

- The measure browser can show only measures of some intervals: chips under the search box (Tag, Minutely, Hourly, Shift, Daily, Weekly, Monthly, Quarterly, Yearly) narrow the tree and the search results.

## 1.4.0

- Example dashboards ship with the plugin: import them from the data source's **Dashboards** tab. _Measure explorer_, _Measures by attribute_, _Event analysis_ and _Chart viewer_ pick measures, events and charts from dropdowns, so they work on any Flow model.
- The plugin's Overview page is a feature tour with screenshots, and the screenshots are listed in the plugin's gallery.
- Events queries take a **Variable** of event ids (`$event`), measures queries in event context an **Event variable**, and chart queries a **Variable** of a chart id (`$chart`).
- New variable lists: _Limits_, the limit names of some measures (the _Measure explorer_ dashboard draws the first and lets people tick more), _Attributes_, the model attributes the measures carry (the _Measures by attribute_ dashboard picks its attribute from it), and _Charts_, the time-based charts in Flow's chart folders. A _Measures_ variable with nothing picked lists every measure in the model.
- Attribute names (filter, variable and event attribute) can be dashboard variables, e.g. `$attribute`.
- One tag Flow cannot read no longer fails the whole query: when Flow rejects a read of several tags (for example "Could not determine a latch source" for a Latch transform without a latch source), the tags are read one at a time, the others are shown, and each rejected tag is named in a warning.
- Dependencies: tag transforms (Latch, Exception, ...) are linked to the tag they transform, with the transform type on the node and the arrow. Flow describes them as `tagTransform` metadata from the build after 7.2.0 `08964ed26`; earlier builds return a server error for these measures, which the plugin reports as before.

## 1.3.1

- Tag aggregations removed from the Measures tab: tags are read raw, averaged per panel interval only when there are more values than the panel can show, or every raw value with All raw values. Panels saved with an aggregation read as before this option, raw/averaged.
- The annotation query editor offers only Measures (value comments) and Events (event periods). Chart and Dependencies were listed but could not produce annotations.

## 1.3.0

- Dashboard variables: model attribute values, measures (template instances or a metric's measures, valued by id), events and event attribute values, each optionally narrowed by a model attribute that can use other variables.
- Measures can be filtered by a model attribute (e.g. Line = `$line`, multi-value and All supported), and taken from a variable of measure ids. Model measures outside the filter are not read.
- Show limits: the measures' Flow limits for the query's calendar or event as dashed threshold lines, listed in the series description. Pick which limits to draw (e.g. only Site); switching on picks the first. Limits of other calendars are never shown on a period measure.
- Annotation queries on the Measures tab mark the comments on the measures' values, with who wrote them and when.
- Dependency node graph: with Show limits on, a node whose latest value is outside its limits (as Flow flags it) is filled red, keeping its type colour on the ring, and its click menu names the limit. The panel's legend lists the ring colours in the graph and explains the red fill, instead of "mainstat / color".
- The model browser and the picked-measure chips show Flow's retrieval marks on each measure's interval icon, as Flow Config does: a triangle for aggregated, an equals sign for calculated, a tray with a plus for manual, a cylinder for retrieved and a boxed cross for replicated, in Flow's blue.

## 1.2.0

- Chart queries show the chart's model attribute columns (after UOM, titled as in Flow), one per attribute. A measure without a value for the attribute gets a blank cell; charts without model attribute columns are unchanged.

## 1.1.0

- Events query (second tab): event periods and their attributes as a table (attribute cells coloured as in Flow) or a state timeline, and as annotations on other panels through a dashboard annotation query. The events browser lists only folders that hold events; events can also be searched by name.
- The model browser shows each measure's interval as a symbol (circled M, H, S, D for minutely to daily; calendar boxes W, M, Q, Y for weekly to yearly) and marks measures with event context with a lightning bolt that names the events. Search results show intervals too.
- Measures can be read in event context: a value per period of an event the measures have as context, optionally kept to some values of an attribute and split into a series per value, coloured as in Flow. _Whole period_ draws each value from its period's start to its end.
- Dependency queries have a Calendar picker: period measures show their latest value in that calendar. Measures that do not have the calendar as context in Flow show a dash, and their node says why.
- Fixed: searching the model browser for names with characters such as "-" or "." (e.g. "110-FQ") found nothing, because the search box escaped them.

## 1.0.0

First release.

- Model browser with Model and Templates tabs, lazy loading, and search by name or measure ID. Measures show their ID.
- Period measures per calendar, and tags raw or aggregated. Tags default to Auto: raw values while they fit the panel, else averages per panel interval.
- Template metrics and measures return every instance, named by folder path.
- Alerting support, quality field, bad quality as gaps.
- Legend presets (full path, folder and measure, measure, with or without unit), or a pattern of your own.
- The editor shows period-measure and tag options only when the selection holds them.
- Chart queries: a time-based Flow chart as a Flow-style table over the dashboard range, with limit colours (green within limits) and value comments as cell tooltips.
- Dependency queries: how measures are built from each other (calculation, aggregation, set to measure, retrieval from a Flow tag), built-from or used-by, in Grafana's Node Graph. Each node shows its latest value, with its period and retrieval type (e.g. _hourly · calculated_) as the subtitle; titles are the full name or the measure's own name. A click lists the measure ID, path, calculation, aggregation, data source and who entered a manual value.
- Nested measures are named in full everywhere: Flow's by-id answer can skip a grandparent, so names come from the metric's measure list.
