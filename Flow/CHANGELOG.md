# Changelog

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
