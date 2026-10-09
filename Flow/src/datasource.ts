import { CoreApp, DataSourceInstanceSettings, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

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
    // Events as annotations on any panel: the annotation editor is the
    // query editor, started on an events query, and always asks for the
    // annotation layout.
    this.annotations = {
      getDefaultQuery: () => ({ queryType: 'events', eventFormat: 'annotations', items: [] }),
      prepareQuery: (anno) => {
        const target = anno.target;
        if (!target || target.queryType !== 'events' || !(target.items?.length ?? 0)) {
          return undefined;
        }
        return { ...target, eventFormat: 'annotations' };
      },
    };
  }

  getDefaultQuery(_: CoreApp): Partial<FlowQuery> {
    return DEFAULT_QUERY;
  }

  /** Dashboard variables may be used in the legend and the fixed resolution. */
  applyTemplateVariables(query: FlowQuery, scopedVars: ScopedVars): FlowQuery {
    const srv = getTemplateSrv();
    return {
      ...query,
      legend: query.legend ? srv.replace(query.legend, scopedVars) : query.legend,
      resolution: query.resolution ? srv.replace(query.resolution, scopedVars) : query.resolution,
    };
  }

  filterQuery(query: FlowQuery): boolean {
    if (query.hide) {
      return false;
    }
    return query.queryType === 'chart' ? !!query.chartId : (query.items?.length ?? 0) > 0;
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
