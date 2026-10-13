import { expandValues } from './datasource';

// A template service with $line = Line 1 + Line 2 (multi-value) and
// $area = Packing, replacing as Grafana does: a format function gets the
// variable's value, a string or an array of the selected values.
const vars: Record<string, string | string[]> = { line: ['Line 1', 'Line 2'], area: 'Packing' };

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
