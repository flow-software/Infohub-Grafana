# Changelog

## Unreleased

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
- Measures can be read in event context: a value per period of an event the measures have as context, optionally kept to some values of an attribute and split into a series per value, coloured as in Flow. *Whole period* draws each value from its period's start to its end.
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
- Dependency queries: how measures are built from each other (calculation, aggregation, set to measure, retrieval from a Flow tag), built-from or used-by, in Grafana's Node Graph. Each node shows its latest value, with its period and retrieval type (e.g. *hourly · calculated*) as the subtitle; titles are the full name or the measure's own name. A click lists the measure ID, path, calculation, aggregation, data source and who entered a manual value.
- Nested measures are named in full everywhere: Flow's by-id answer can skip a grandparent, so names come from the metric's measure list.
