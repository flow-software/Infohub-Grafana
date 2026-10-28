import React, { useEffect, useMemo, useState } from 'react';
import { getTemplateSrv } from '@grafana/runtime';
import { Combobox, ComboboxOption, InlineField, MultiCombobox, Stack } from '@grafana/ui';

import type { DataSource } from '../datasource';
import { itemKey, QueryItem } from '../types';

/**
 * Picks a model attribute and the values to keep, e.g. Line = Line 1. The
 * values list holds the attribute's values in Flow and the dashboard's
 * variables, so a panel can follow a "$line" dropdown; others can be typed.
 */
export function AttributeFilterField({
  datasource,
  label,
  labelWidth,
  tooltip,
  attribute,
  values,
  items = [],
  onChange,
}: {
  datasource: DataSource;
  label: string;
  labelWidth: number;
  tooltip: string;
  attribute?: string;
  values?: string[];
  /** The selection: only its measures' attributes and values are listed. None lists all. */
  items?: QueryItem[];
  onChange: (attribute: string | undefined, values: string[] | undefined) => void;
}) {
  const scope = items.map(itemKey).join(',');
  const names = useAnswer(() => datasource.getAttributes(items), [datasource, scope], 'attributes:' + scope);
  const valueKey = (attribute ?? '') + ':' + scope;
  const known = useAnswer(
    () => (attribute ? datasource.getAttributeValues(attribute, items) : Promise.resolve([])),
    [datasource, valueKey],
    valueKey
  );

  const nameOptions = useMemo<Array<ComboboxOption<string>>>(() => {
    const opts = (names ?? []).map((n) => ({ label: n, value: n }));
    if (attribute && !opts.some((o) => o.value === attribute)) {
      opts.unshift({ label: attribute, value: attribute });
    }
    return opts;
  }, [names, attribute]);

  const valueOptions = useMemo<Array<ComboboxOption<string>>>(() => {
    const variables = getTemplateSrv()
      .getVariables()
      .map((v) => ({ label: '$' + v.name, value: '$' + v.name, description: 'Dashboard variable' }));
    const opts: Array<ComboboxOption<string>> = [...variables, ...(known ?? []).map((v) => ({ label: v, value: v }))];
    for (const v of values ?? []) {
      if (!opts.some((o) => o.value === v)) {
        opts.push({ label: v, value: v });
      }
    }
    return opts;
  }, [known, values]);

  return (
    <InlineField label={label} labelWidth={labelWidth} tooltip={tooltip}>
      <Stack gap={1} wrap="wrap" alignItems="center">
        <Combobox
          width={20}
          isClearable
          placeholder="Attribute"
          options={nameOptions}
          value={attribute ?? null}
          onChange={(o) => onChange(o?.value, undefined)}
        />
        {attribute && (
          <MultiCombobox
            width={40}
            placeholder="Values or $variable"
            createCustomValue
            options={valueOptions}
            value={values ?? []}
            onChange={(opts) => onChange(attribute, opts.length ? opts.map((o) => o.value) : undefined)}
          />
        )}
      </Stack>
    </InlineField>
  );
}

/**
 * Loads a value for the editor, keeping the answer with the request key it
 * answers so a stale one is never shown. Errors leave it empty: the pickers
 * still accept typed values.
 */
function useAnswer<T>(load: () => Promise<T>, deps: unknown[], key: string): T | undefined {
  const [answer, setAnswer] = useState<{ key: string; value: T }>();
  useEffect(() => {
    let cancelled = false;
    load()
      .then((value) => !cancelled && setAnswer({ key, value }))
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
    // The caller's deps stand for load, which is a new closure every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return answer?.key === key ? answer.value : undefined;
}
