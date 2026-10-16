import {
  CoreApp,
  CustomVariableSupport,
  DataQueryRequest,
  DataQueryResponse,
  DataSourceInstanceSettings,
  ScopedVars,
} from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';
import { Observable } from 'rxjs';

import { VariableQueryEditor } from './components/VariableQueryEditor';

import {
  AttributeValueOption,
  BrowseNode,
  Calendar,
  ChartNode,
  ContextEvent,
  DEFAULT_QUERY,
  FlowOptions,
  FlowQuery,
  itemKey,
  QueryItem,
  SelectionSummary,
  Tree,
} from './types';

/** What the model browser lists: metrics and measures, or events. */
export type BrowseKind = 'measures' | 'events';

export class DataSource extends DataSourceWithBackend<FlowQuery, FlowOptions> {
  /** Whether the Flow database is set up, so dependency queries can show processing. */
  readonly processingAvailable: boolean;

  constructor(instanceSettings: DataSourceInstanceSettings<FlowOptions>) {
    super(instanceSettings);
    this.processingAvailable = !!instanceSettings.jsonData?.sqlServer?.trim();
    // Annotations on any panel: the annotation editor is the query editor,
    // started on an events query. Events become regions; on the Measures tab
    // the measures' value comments become markers.
    this.annotations = {
      getDefaultQuery: () => ({ queryType: 'events', eventFormat: 'annotations', items: [] }),
      prepareQuery: (anno) => {
        const target = anno.target;
        if (!target) {
          return undefined;
        }
        if (target.queryType === 'events') {
          return target.items?.length || target.eventVariable ? { ...target, eventFormat: 'annotations' } : undefined;
        }
        if ((target.queryType ?? 'measures') === 'measures') {
          return target.items?.length || target.measureVariable ? { ...target, annotate: 'comments' } : undefined;
        }
        return undefined;
      },
    };
    this.variables = new FlowVariableSupport(this);
  }

  getDefaultQuery(_: CoreApp): Partial<FlowQuery> {
    return DEFAULT_QUERY;
  }

  /**
   * Dashboard variables may be used in the legend, attribute names and values, event attribute names and values, and
   * as lists of measure, event or chart ids. Multi-value and All variables expand to every value.
   */
  applyTemplateVariables(query: FlowQuery, scopedVars: ScopedVars): FlowQuery {
    const srv = getTemplateSrv();
    const out: FlowQuery = {
      ...query,
      legend: query.legend ? srv.replace(query.legend, scopedVars) : query.legend,
      attributeValues: expandValues(query.attributeValues, scopedVars),
      eventValues: expandValues(query.eventValues, scopedVars),
      // An empty variable (a measure without limits) leaves no names: all are drawn.
      limitNames: expandValues(query.limitNames, scopedVars)?.filter((n) => n !== ''),
      // Attribute names may be variables too, so one dashboard serves Line, Site, ...
      attributeFilter: query.attributeFilter ? srv.replace(query.attributeFilter, scopedVars) : query.attributeFilter,
      variableAttribute: query.variableAttribute
        ? srv.replace(query.variableAttribute, scopedVars)
        : query.variableAttribute,
      eventAttribute: query.eventAttribute ? srv.replace(query.eventAttribute, scopedVars) : query.eventAttribute,
    };
    if (query.measureVariable) {
      const ids = expandValues([query.measureVariable], scopedVars) ?? [];
      const added: QueryItem[] = ids
        .map((v) => Number(v))
        .filter((id) => Number.isInteger(id) && id > 0)
        .map((id) => ({ tree: 'model', type: 'measure', id, label: 'measure ' + id }));
      const seen = new Set((query.items ?? []).map(itemKey));
      out.items = [...(query.items ?? []), ...added.filter((it) => !seen.has(itemKey(it)))];
    }
    if (query.eventVariable) {
      const ids = idsOf(expandValues([query.eventVariable], scopedVars));
      if (query.queryType === 'events') {
        const seen = new Set((out.items ?? []).map(itemKey));
        const added: QueryItem[] = ids.map((id) => ({ tree: 'model', type: 'event', id, label: 'event ' + id }));
        out.items = [...(out.items ?? []), ...added.filter((it) => !seen.has(itemKey(it)))];
      } else if (query.context === 'event' && ids.length > 0) {
        out.eventId = ids[0];
      }
    }
    if (query.chartVariable) {
      const ids = idsOf(expandValues([query.chartVariable], scopedVars));
      if (ids.length > 0) {
        out.chartId = ids[0];
      }
    }
    return out;
  }

  filterQuery(query: FlowQuery): boolean {
    if (query.hide) {
      return false;
    }
    switch (query.queryType) {
      case 'chart':
        return !!query.chartId || !!query.chartVariable;
      case 'events':
        return (query.items?.length ?? 0) > 0 || !!query.eventVariable;
      case 'variable':
        return true;
      default:
        return (query.items?.length ?? 0) > 0 || !!query.measureVariable;
    }
  }

  /** Limit names the items have in a calendar, or in an event (context "event"). */
  getLimitNames(items: QueryItem[], calendarId?: number, eventId?: number): Promise<string[]> {
    const params: Record<string, string | number | string[]> = { item: items.map(itemKey) };
    if (eventId) {
      params.context = 'event';
      params.eventId = eventId;
    } else if (calendarId) {
      params.calendarId = calendarId;
    }
    return this.getResource<string[]>('limits', params);
  }

  /** Model attribute names the model's measures carry. */
  getAttributes(): Promise<string[]> {
    return this.getResource<string[]>('attributes');
  }

  /** Values of a model attribute on the model's measures. */
  getAttributeValues(attribute: string): Promise<string[]> {
    return this.getResource<string[]>('attributeValues', { attribute });
  }

  getCalendars(): Promise<Calendar[]> {
    return this.getResource<Calendar[]>('calendars');
  }

  /**
   * Children of a folder or metric; no parent lists the root folders. With
   * kind "events", folders hold events instead of metrics.
   */
  browse(tree: Tree, parent?: Pick<BrowseNode, 'type' | 'id'>, kind?: BrowseKind): Promise<BrowseNode[]> {
    const params: Record<string, string | number> = { tree };
    if (parent) {
      params.type = parent.type;
      params.id = parent.id;
    }
    if (kind === 'events') {
      params.kind = kind;
    }
    return this.getResource<BrowseNode[]>('browse', params);
  }

  search(tree: Tree, q: string, kind?: BrowseKind): Promise<BrowseNode[]> {
    return this.getResource<BrowseNode[]>('search', kind === 'events' ? { tree, q, kind } : { tree, q });
  }

  /** Events the selected model measures have as context. */
  getContextEvents(items: QueryItem[]): Promise<ContextEvent[]> {
    return this.getResource<ContextEvent[]>('contextEvents', { item: items.map(itemKey) });
  }

  /** Values of an event attribute seen recently, with their colours. */
  getEventValues(eventId: number, attribute: string): Promise<AttributeValueOption[]> {
    return this.getResource<AttributeValueOption[]>('eventValues', { event: eventId, attribute });
  }

  /** Root chart folders, or a folder's folders and charts. */
  browseCharts(folderId?: number): Promise<ChartNode[]> {
    return this.getResource<ChartNode[]>('charts', folderId ? { id: folderId } : {});
  }

  /** How many period measures and tags the items hold. */
  getSummary(items: QueryItem[]): Promise<SelectionSummary> {
    return this.getResource<SelectionSummary>('summary', { item: items.map(itemKey) });
  }
}

/**
 * Replaces dashboard variables in a list of values. A value that is a
 * multi-value variable becomes all its values, so "$line" with Line 1 and
 * Line 2 selected filters on both.
 */
/** The positive integer ids among a variable's values. */
function idsOf(values: string[] | undefined): number[] {
  return (values ?? []).map((v) => Number(v)).filter((id) => Number.isInteger(id) && id > 0);
}

export function expandValues(values: string[] | undefined, scopedVars: ScopedVars): string[] | undefined {
  if (!values) {
    return values;
  }
  const srv = getTemplateSrv();
  return values.flatMap((v) => {
    if (!v.includes('$')) {
      return [v];
    }
    const json = srv.replace(v, scopedVars, (value: string | string[]) =>
      JSON.stringify(Array.isArray(value) ? value : [value])
    );
    try {
      const parsed = JSON.parse(json);
      if (Array.isArray(parsed)) {
        return parsed.map(String);
      }
    } catch {
      // Text around the variable: replace it as plain text.
    }
    return [srv.replace(v, scopedVars)];
  });
}

/** Dashboard variables: Flow model attribute values, measures and events. */
export class FlowVariableSupport extends CustomVariableSupport<DataSource, FlowQuery> {
  editor = VariableQueryEditor;

  constructor(private readonly datasource: DataSource) {
    super();
  }

  query(request: DataQueryRequest<FlowQuery>): Observable<DataQueryResponse> {
    // A new variable query has no refId, and Grafana drops results it
    // cannot match back to a query by refId: the preview showed nothing.
    const targets = request.targets.map((t) => ({
      ...t,
      refId: t.refId || 'variable',
      queryType: 'variable' as const,
    }));
    return this.datasource.query({ ...request, targets });
  }
}
