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
  constructor(instanceSettings: DataSourceInstanceSettings<FlowOptions>) {
    super(instanceSettings);
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
          return target.items?.length ? { ...target, eventFormat: 'annotations' } : undefined;
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
   * Dashboard variables may be used in the legend, the fixed resolution,
   * attribute filter values, event attribute values, and as a list of
   * measure ids. Multi-value and All variables expand to every value.
   */
  applyTemplateVariables(query: FlowQuery, scopedVars: ScopedVars): FlowQuery {
    const srv = getTemplateSrv();
    const out: FlowQuery = {
      ...query,
      legend: query.legend ? srv.replace(query.legend, scopedVars) : query.legend,
      resolution: query.resolution ? srv.replace(query.resolution, scopedVars) : query.resolution,
      attributeValues: expandValues(query.attributeValues, scopedVars),
      eventValues: expandValues(query.eventValues, scopedVars),
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
    return out;
  }

  filterQuery(query: FlowQuery): boolean {
    if (query.hide) {
      return false;
    }
    switch (query.queryType) {
      case 'chart':
        return !!query.chartId;
      case 'variable':
        return true;
      default:
        return (query.items?.length ?? 0) > 0 || !!query.measureVariable;
    }
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
    const targets = request.targets.map((t) => ({ ...t, queryType: 'variable' as const }));
    return this.datasource.query({ ...request, targets });
  }
}
