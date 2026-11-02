import React, { useMemo } from 'react';
import { getTemplateSrv } from '@grafana/runtime';
import { Combobox, ComboboxOption } from '@grafana/ui';

import { VariableKind } from '../types';

const PLUGIN_TYPE = 'flowsoftware-infohub-datasource';

/** What each kind of this data source's variables is called in the list. */
const kindLabels: Partial<Record<VariableKind, string>> = {
  measures: 'Measures variable',
  events: 'Events variable',
  eventPeriods: 'Periods (batches) variable',
};

/**
 * The dashboard's variables as options, those of this data source that list
 * the wanted kind first (e.g. Measures variables for a measure Variable
 * field), then the others.
 */
export function variableOptions(kind: VariableKind): Array<ComboboxOption<string>> {
  const matching: Array<ComboboxOption<string>> = [];
  const others: Array<ComboboxOption<string>> = [];
  for (const v of getTemplateSrv().getVariables()) {
    const model = v as unknown as { query?: unknown; datasource?: { type?: string } | string | null };
    const query = model.query as { variableKind?: VariableKind } | undefined;
    const ds = model.datasource;
    // A datasource given by variable ($ds) has no type here: judge by the query.
    const ours = !ds || typeof ds === 'string' || !ds.type || ds.type === PLUGIN_TYPE;
    const option = { label: '$' + v.name, value: '$' + v.name };
    if (ours && query && typeof query === 'object' && (query.variableKind ?? 'measures') === kind) {
      matching.push({ ...option, description: kindLabels[kind] ?? 'Variable of this data source' });
    } else {
      others.push({ ...option, description: 'Dashboard variable' });
    }
  }
  return [...matching, ...others];
}

/**
 * Picks a dashboard variable for a field that takes one, such as $measure:
 * this data source's variables of the right kind first. Another value can be
 * typed; clearing it leaves the field empty.
 */
export function VariablePicker({
  id,
  kind,
  value,
  placeholder,
  onChange,
}: {
  id: string;
  kind: VariableKind;
  value?: string;
  placeholder: string;
  onChange: (value: string | undefined) => void;
}) {
  const options = useMemo(() => {
    const opts = variableOptions(kind);
    if (value && !opts.some((o) => o.value === value)) {
      opts.unshift({ label: value, value });
    }
    return opts;
  }, [kind, value]);
  return (
    <Combobox
      id={id}
      width={30}
      isClearable
      createCustomValue
      placeholder={placeholder}
      options={options}
      value={value ?? null}
      onChange={(o: ComboboxOption<string> | null) => onChange(o?.value?.trim() || undefined)}
    />
  );
}
