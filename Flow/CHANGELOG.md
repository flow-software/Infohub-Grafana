# Changelog

## Unreleased

- The plugin is titled **Infohub** in Grafana (by Flow Software), with an up-to-date description.
- Connection settings ask for the Flow Bootstrap **host**, **port** (default 4501), **HTTPS** and the **instance ID**, and build the Flow Server address from them, showing it. Existing data sources are split into these fields; pasting a Flow Server or Swagger address into Host fills them all; **Enter a full URL instead** keeps any other address (e.g. a reverse proxy).
- Metrics show a cube icon in the browser and in the selection chips.
- **Golden batch**: a Measures query's new **Batch** section reads the measures over one batch (`$batch`) instead of the time range, on seconds since the batch's start, and **Compare with** draws the same measures over a reference batch (`$golden`) dashed beside them, aligned on its start, with an optional ± value or ± % **Band**. Use the Trend panel.
- Variables: **Events → Periods (batches)** lists each run of an event in the time range, newest first, labelled by an attribute (e.g. _Batch ID_) and filtered with **Where** (e.g. _Product_ = `$product`), for batch and golden-batch pickers.
- Events table: **Numbers** returns the picked event attributes as numbers instead of text (Flow keeps attribute values as text), so they can be plotted on a time series without a Convert field type transformation. Values that are not numbers are left empty.
- **Show limits** is now **Show limit values** (and the dependency graph's **Show limit evaluation**, as it shows Flow's verdict). Limits are drawn as dashed lines that follow the limit's settings over time (_Site high_, _Site target_, ...), stepping where a new setting starts, instead of one fixed threshold at today's setting, which was wrong for any earlier period. Note: Flow's API currently returns only a limit's latest setting (a bug in `PublicRepository.Measures.cs`), so until it is fixed, the lines start at that setting's date. A limit value that comes from another measure (e.g. _high from measure 4299_) is drawn from that measure's value for the same period, until Flow's API returns limits' own values.
- Limits: **Show limit evaluation** adds a **Limit evaluation** field with Flow's verdict on each period value (_Within limits_ green, _Above high limit_ / _Below low limit_ red), so a bar chart (Color by field) or a table can be coloured by Flow's own limit logic. Period measures only: Flow does not check tags. When Flow fails to evaluate the limits, the values are still shown, without the evaluation, and the panel says why (this also applies to the dependency graph's limit evaluation).
- Tags: a tag with more values than the panel can show is now drawn as each panel interval's **minimum and maximum** instead of its average, so spikes and dips stay visible at any zoom. **Values** is now _Auto_ | _Average_ | _Raw_: _Average_ is the former smoothing, _Raw_ the former **All raw values** switch. Panels saved without a choice move from averages to min/max.
- The query editor's labels are a little wider, so **Event variable** no longer wraps onto two lines and pushes its row out of line.
- The variable editor groups its lists: **List** _Measures_, _Model attributes_, _Events_ or _Charts_, then **Show** what to list of it (e.g. _Attribute names_ or _Values of an attribute_). Existing variables are unchanged; a new variable starts on _Measures_.
- Variables no longer offer **Charts** for new variables; existing chart variables (the Chart viewer's `$chart`) keep working.
- Event variables: **Only** (was _Of_) now narrows the list to the picked events too, not just to template events' instances. _Values of an event attribute_ takes one event (a template event gives the values of every event created from it), and its **Attribute** is a list of the event's attributes instead of a text box. The event can also come from an **Event variable** (`$event`), so the values follow an events dropdown.
- The attribute pickers (**Where** and _Values of an attribute_'s **Attribute**) list dashboard variables too, so a measures variable can follow an attribute-names variable and a values variable: _Where_ `$attribute` = `$value`.

## 1.8.0

- Whole folders can be picked: a folder stands for every measure in it and the folders below it (new measures included), in measures, dependency and variable queries. Folders show up in the browser's search, and a ticked folder shows how many measures it holds.
- The **Filter** lists only the model attributes the selected measures carry, and only the values among them; with measures from a variable, it lists all of them as before.

## 1.7.0

- Times in dependency node menus, correction and comment tooltips are shown in the dashboard's time zone (they were UTC), and a node's **Value at** is now its **Period**, e.g. _2026-10-07 06:00 – 2026-10-08 06:00_.
- Dependencies: each node's menu shows its latest value's version (number, type, who and when) and its retrieval type. With retrieval-type rings the subtitle no longer repeats the retrieval type.
- The Overview page and its screenshots are brought up to date, with a screenshot of marked corrections.
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
