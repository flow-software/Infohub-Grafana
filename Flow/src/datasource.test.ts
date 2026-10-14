import { DataSourceInstanceSettings } from '@grafana/data';

import { DataSource, expandValues } from './datasource';
import { FlowOptions } from './types';

// A template service with $line = Line 1 + Line 2 (multi-value) and
// $area = Packing, replacing as Grafana does: a format function gets the
// variable's value, a string or an array of the selected values.
const vars: Record<string, string | string[]> = {
  line: ['Line 1', 'Line 2'],
  area: 'Packing',
  event: ['7', '8'],
  chart: '5',
};

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: () => ({
    replace: (target: string, _scoped: unknown, format?: (v: string | string[]) => string) =>
      target.replace(/\$(\w+)/g, (all, name: string) => {
        const v = vars[name];
        if (v === undefined) {
          return all;
        }
        if (format) {
          return format(v);
        }
        return Array.isArray(v) ? v.join(',') : v;
      }),
    getVariables: () => [],
  }),
}));

describe('expandValues', () => {
  it('expands a multi-value variable to every selected value', () => {
    expect(expandValues(['$line'], {})).toEqual(['Line 1', 'Line 2']);
  });

  it('keeps plain values and mixes them with variables', () => {
    expect(expandValues(['Line 3', '$area'], {})).toEqual(['Line 3', 'Packing']);
  });

  it('replaces a variable inside other text as plain text', () => {
    expect(expandValues(['Area $area'], {})).toEqual(['Area Packing']);
  });

  it('leaves unknown variables and no values alone', () => {
    expect(expandValues(['$nope'], {})).toEqual(['$nope']);
    expect(expandValues(undefined, {})).toBeUndefined();
  });
});

describe('applyTemplateVariables', () => {
  const ds = new DataSource({ id: 1, uid: 'flow', jsonData: {} } as DataSourceInstanceSettings<FlowOptions>);

  it('adds the events of an event variable to an events query', () => {
    const out = ds.applyTemplateVariables(
      {
        refId: 'A',
        queryType: 'events',
        items: [{ tree: 'model', type: 'event', id: 7, label: 'Trigger' }],
        eventVariable: '$event',
      },
      {}
    );
    expect(out.items?.map((it) => it.id)).toEqual([7, 8]);
  });

  it('takes the first event of an event variable as the event context', () => {
    const out = ds.applyTemplateVariables({ refId: 'A', context: 'event', eventId: 3, eventVariable: '$event' }, {});
    expect(out.eventId).toBe(7);
  });

  it('replaces variables in attribute names', () => {
    const out = ds.applyTemplateVariables(
      { refId: 'A', attributeFilter: '$area', attributeValues: ['$line'], variableAttribute: '$area' },
      {}
    );
    expect(out.attributeFilter).toBe('Packing');
    expect(out.variableAttribute).toBe('Packing');
    expect(out.attributeValues).toEqual(['Line 1', 'Line 2']);
  });

  it('runs event annotations that take their events from a variable', () => {
    const prepare = ds.annotations!.prepareQuery!;
    const anno = (target: object) => ({ name: 'periods', enable: true, iconColor: 'red', target }) as never;
    expect(prepare(anno({ refId: 'Anno', queryType: 'events', items: [], eventVariable: '$event' }))).toMatchObject({
      eventFormat: 'annotations',
    });
    expect(prepare(anno({ refId: 'Anno', queryType: 'events', items: [] }))).toBeUndefined();
  });

  it('expands limit names and drops an empty one', () => {
    expect(ds.applyTemplateVariables({ refId: 'A', limitNames: ['$area'] }, {}).limitNames).toEqual(['Packing']);
    expect(ds.applyTemplateVariables({ refId: 'A', limitNames: [''] }, {}).limitNames).toEqual([]);
  });

  it('takes the chart of a chart variable', () => {
    const out = ds.applyTemplateVariables({ refId: 'A', queryType: 'chart', chartId: 1, chartVariable: '$chart' }, {});
    expect(out.chartId).toBe(5);
    expect(ds.filterQuery({ refId: 'A', queryType: 'chart', chartVariable: '$chart' })).toBe(true);
  });
});
