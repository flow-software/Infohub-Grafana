import { CoreApp, DataSourceInstanceSettings, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import {
  BrowseNode,
  Calendar,
  ChartNode,
  DEFAULT_QUERY,
  FlowOptions,
  FlowQuery,
  itemKey,
  QueryItem,
  SelectionSummary,
  Tree,
} from './types';

export class DataSource extends DataSourceWithBackend<FlowQuery, FlowOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<FlowOptions>) {
    super(instanceSettings);
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

  /** Children of a folder or metric; no parent lists the root folders. */
  browse(tree: Tree, parent?: Pick<BrowseNode, 'type' | 'id'>): Promise<BrowseNode[]> {
    const params: Record<string, string | number> = { tree };
    if (parent) {
      params.type = parent.type;
      params.id = parent.id;
    }
    return this.getResource<BrowseNode[]>('browse', params);
  }

  search(tree: Tree, q: string): Promise<BrowseNode[]> {
    return this.getResource<BrowseNode[]>('search', { tree, q });
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
