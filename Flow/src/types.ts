import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

/** Which half of the Flow model an id belongs to. */
export type Tree = 'model' | 'template';

/**
 * A metric stands for all of its measures, resolved when the query runs.
 * Events are picked in events queries; a template event stands for every
 * event created from it.
 */
export type ItemType = 'folder' | 'metric' | 'measure' | 'event' | 'chart';

export interface QueryItem {
  tree: Tree;
  type: ItemType;
  id: number;
  /** Shown in the editor, e.g. "Site/Line 1/Production/Good". */
  label: string;
  /** Measures only, as browsed: "timestamp" (a tag) or a period such as "hourly". Display only. */
  interval?: string;
  /** Measures only, as browsed: calculated, manual, ... Display only. */
  retrieval?: string;
}

export type QueryType = 'measures' | 'chart' | 'dependencies' | 'events' | 'variable';

/** What a dashboard variable query lists. */
export type VariableKind =
  'attributeNames' | 'attributeValues' | 'measures' | 'events' | 'eventValues' | 'charts' | 'limits';

/** How an events query lays out event periods. */
export type EventFormat = 'table' | 'timeline' | 'annotations';

/** inputs: what a measure is built from. usedBy: what is built from it. */
export type DepDirection = 'inputs' | 'usedBy';

export interface FlowQuery extends DataQuery {
  /** Unset means measures. */
  queryType?: QueryType;

  /** Chart queries: a Flow chart shown as a table, over the dashboard's time range. */
  chartId?: number;
  /** A dashboard variable holding a chart id ("$chart"); it replaces chartId. */
  chartVariable?: string;
  /** Shown in the editor. */
  chartLabel?: string;
  /**
   * "measures" reads the chart's measures (of any chart type) as a measures
   * query, for any panel; unset lays the chart out as Flow does.
   */
  chartMode?: 'measures';

  /** Dependency queries; they pick measures with items too. */
  depDirection?: DepDirection;
  /** Levels to follow; unset is 10. */
  depDepth?: number;
  /** "none" leaves out each measure's latest value; unset shows it. */
  depValues?: 'none';
  /** "measure" titles nodes by the measure's own name ("WTD"); unset uses Flow's full Metric.Measure name. */
  depNames?: 'measure';
  /** Adds how far the engine has got with each measure, from the Flow database. */
  depProcessing?: boolean;
  /** "processing" colours rings by processing state instead of retrieval type. */
  depRing?: 'processing';

  /** Events queries: table (default), state timeline, or annotations. */
  eventFormat?: EventFormat;

  /**
   * Measure queries: "event" reads period measures per period of eventId
   * instead of per calendar period.
   */
  context?: 'event';
  eventId?: number;
  /**
   * A dashboard variable holding event ids ("$event"): events queries add
   * its events to items; in event context its first event replaces eventId.
   */
  eventVariable?: string;
  /** Shown in the editor. */
  eventLabel?: string;
  /** With eventValues, keep only periods whose attribute has one of them. */
  eventAttribute?: string;
  eventValues?: string[];
  /** One series per value of eventAttribute. */
  eventSplit?: boolean;

  /**
   * Keep only measures whose model attribute has one of the values, e.g.
   * Line = "Line 1". Values may be dashboard variables ("$line").
   */
  attributeFilter?: string;
  attributeValues?: string[];
  /** A dashboard variable holding measure ids ("$measure"), added to items. */
  measureVariable?: string;
  /** "lines" draws each measure's Flow limits as threshold lines. */
  limits?: 'lines';
  /** Which limits to draw, by name (e.g. Site); none draws them all. */
  limitNames?: string[];
  /** Annotation queries on measures: "comments" shows their value comments. */
  annotate?: 'comments';

  /** Variable queries. */
  variableKind?: VariableKind;
  /** attributeValues: the model attribute to list the values of. */
  variableAttribute?: string;

  /** Measure queries. */
  items?: QueryItem[];
  /** Calendar for period measures; unset uses the datasource default. */
  calendarId?: number;
  /**
   * Tags (timestamp measures): unset is Auto (raw values while they fit the
   * panel, else each panel interval's minimum and maximum); "average" uses
   * averages instead; "raw" reads every raw value. Queries saved with another
   * value (a former tag aggregation) read as Auto.
   */
  aggregation?: string;
  /**
   * Place period values at the period start (default) or end. In event
   * context, "period" draws each value from its period's start to its end.
   */
  timeAt?: 'start' | 'end' | 'period';
  includeQuality?: boolean;
  /** Adds each period value's version, its type (original, updated, manual, ...) and who entered it. */
  includeVersion?: boolean;
  /**
   * Reads every version of each period value. "corrections": the preferred
   * values as usual, and an annotation on each corrected period listing its
   * versions. "history": one table of every version, like Flow's History tab.
   */
  versions?: 'corrections' | 'history';
  badQualityAsNull?: boolean;
  /** Series name pattern: {{path}}, {{folder}}, {{metric}}, {{measure}}, {{id}}, {{uom}}. */
  legend?: string;
}

export const DEFAULT_QUERY: Partial<FlowQuery> = {
  items: [],
  timeAt: 'start',
};

/** Settings stored in jsonData. The Flow Server base URL is the datasource's top-level url. */
export interface FlowOptions extends DataSourceJsonData {
  defaultCalendarId?: number;
  /** Standard Grafana TLS keys, read by the backend's HTTP client. */
  tlsSkipVerify?: boolean;
  tlsAuthWithCACert?: boolean;
  /** The optional Flow database, for the processing state of measures. */
  sqlServer?: string;
  sqlDatabase?: string;
  sqlUser?: string;
  /** The SQL Server driver's encrypt option; unset is "false" (login only). */
  sqlEncrypt?: 'false' | 'true' | 'strict' | 'disable';
  sqlTrustServerCertificate?: boolean;
}

/** Secrets: write-only from the browser, decrypted only in the backend. */
export interface FlowSecureJsonData {
  token?: string;
  tlsCACert?: string;
  sqlPassword?: string;
}

/** One row from the backend's browse and search resources. */
export interface BrowseNode {
  tree: Tree;
  type: 'folder' | ItemType;
  id: number;
  name: string;
  /** Measures only: timestamp (a tag) or a period such as hourly. */
  interval?: string;
  /** Measures only: how it gets its values (calculated, manual, ...). */
  retrieval?: string;
  uom?: string;
  description?: string;
  /** A model object created from a template. */
  fromTemplate?: boolean;
  /** Measures only: the events it has as context, by name. */
  events?: string[];
}

/** One row from the backend's charts resource. */
export interface ChartNode {
  type: 'folder' | 'chart';
  id: number;
  name: string;
  /** Charts only, e.g. "Time Based Table". */
  chartType?: string;
  /** Charts the plugin can turn into a table. */
  supported?: boolean;
}

/** What a selection holds, so the editor shows only the options that apply. */
export interface SelectionSummary {
  periodMeasures: number;
  tags: number;
  warnings?: string[];
  /** Which calendars and events the period measures can be read in; absent when not known. */
  context?: SelectionContext;
}

export interface SelectionContext {
  /** How many period measures were looked at. */
  measures: number;
  /** The calendars they have as context, with how many have each. */
  calendars: Array<{ id: number; measures: number }>;
  /** How many model measures have an event as context. */
  eventMeasures: number;
}

/** An event the selected measures can be read in. */
export interface ContextEvent {
  id: number;
  /** Folder path, e.g. "Site/Line 1/Run". */
  name: string;
  attributes?: string[] | null;
  /** How many of the selected measures have it as context. */
  measures: number;
}

/** A value of an event attribute, with its colour in Flow. */
export interface AttributeValueOption {
  value: string;
  color?: string;
}

export interface Calendar {
  id: number;
  name: string;
  description?: string;
}

export function itemKey(item: Pick<QueryItem, 'tree' | 'type' | 'id'>): string {
  return `${item.tree}:${item.type}:${item.id}`;
}
