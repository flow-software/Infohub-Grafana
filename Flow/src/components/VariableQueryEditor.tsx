import React, { useEffect, useMemo, useState } from 'react';
import { QueryEditorProps } from '@grafana/data';
import { Combobox, ComboboxOption, InlineField, Input, RadioButtonGroup, Stack } from '@grafana/ui';

import type { DataSource } from '../datasource';
import { FlowOptions, FlowQuery, QueryItem, VariableKind } from '../types';
import { AttributeFilterField } from './AttributeFilterField';
import { ItemsPicker, LABEL_WIDTH } from './QueryEditor';

type Props = QueryEditorProps<DataSource, FlowQuery, FlowOptions, FlowQuery>;

const kindOptions: Array<{ label: string; value: VariableKind; description: string }> = [
  {
    label: 'Attribute values',
    value: 'attributeValues',
    description: 'The values of a model attribute, e.g. every Line',
  },
  {
    label: 'Measures',
    value: 'measures',
    description: 'Measures by path, valued by id: instances of a template, or a metric’s measures',
  },
  { label: 'Events', value: 'events', description: 'Events by path, valued by id' },
  {
    label: 'Event values',
    value: 'eventValues',
    description: 'The values of an event attribute seen in the last 90 days',
  },
  { label: 'Charts', value: 'charts', description: 'Time-based Flow charts by folder path, valued by id' },
];

/**
 * Edits a dashboard variable query. Each kind can be narrowed by a model
 * attribute whose values may be other variables, so variables can chain:
 * Line first, then the measures on $line.
 */
export function VariableQueryEditor({ datasource, query, onChange }: Props) {
  const kind = query.variableKind ?? 'attributeValues';
  const items = useMemo(() => query.items ?? [], [query.items]);
  const update = (patch: Partial<FlowQuery>) =>
    onChange({ ...query, queryType: 'variable', variableKind: kind, ...patch });

  return (
    <Stack direction="column" gap={0.5}>
      <InlineField label="List" labelWidth={LABEL_WIDTH}>
        <RadioButtonGroup<VariableKind>
          options={kindOptions}
          value={kind}
          onChange={(v) => onChange({ refId: query.refId, queryType: 'variable', variableKind: v })}
        />
      </InlineField>

      {kind === 'attributeValues' && (
        <AttributeNameField
          datasource={datasource}
          value={query.variableAttribute}
          onChange={(variableAttribute) => update({ variableAttribute })}
        />
      )}

      {(kind === 'attributeValues' || kind === 'measures') && (
        <ItemsPicker
          datasource={datasource}
          items={items}
          label={kind === 'measures' ? 'Of' : 'Only on'}
          onChange={(next) => update({ items: next })}
          tooltip={
            kind === 'measures'
              ? 'Pick template measures or metrics to list their model instances, or model metrics to list their measures. Empty lists every measure in the model.'
              : 'Optional: only values on the measures these stand for (template items stand for their instances). Empty is the whole model.'
          }
        />
      )}

      {(kind === 'events' || kind === 'eventValues') && (
        <ItemsPicker
          datasource={datasource}
          items={items}
          label={kind === 'events' ? 'Of' : 'Event'}
          kind="events"
          onChange={(next) => update({ items: next, eventId: firstModelEvent(next) })}
          tooltip={
            kind === 'events'
              ? 'Optional: template events, to list only their model instances. Empty lists every event.'
              : 'The event whose attribute values to list.'
          }
        />
      )}

      {kind === 'eventValues' && (
        <InlineField label="Attribute" labelWidth={LABEL_WIDTH} tooltip="The event attribute, e.g. Product.">
          <Input
            width={30}
            placeholder="Product"
            defaultValue={query.eventAttribute ?? ''}
            onBlur={(e) => update({ eventAttribute: e.currentTarget.value || undefined })}
          />
        </InlineField>
      )}

      {(kind === 'attributeValues' || kind === 'measures') && (
        <AttributeFilterField
          datasource={datasource}
          label="Where"
          labelWidth={LABEL_WIDTH}
          tooltip="Optional: only measures whose model attribute has one of these values. Use another variable, such as $line, to make this list follow it."
          attribute={query.attributeFilter}
          values={query.attributeValues}
          onChange={(attributeFilter, attributeValues) => update({ attributeFilter, attributeValues })}
        />
      )}
    </Stack>
  );
}

function firstModelEvent(items: QueryItem[]): number | undefined {
  return items.find((it) => it.type === 'event' && it.tree === 'model')?.id;
}

/** Picks a model attribute name from those the model's measures carry. */
function AttributeNameField({
  datasource,
  value,
  onChange,
}: {
  datasource: DataSource;
  value?: string;
  onChange: (name: string | undefined) => void;
}) {
  const [names, setNames] = useState<string[]>([]);
  useEffect(() => {
    let cancelled = false;
    datasource
      .getAttributes()
      .then((list) => !cancelled && setNames(list))
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [datasource]);
  const options = useMemo<Array<ComboboxOption<string>>>(() => {
    const opts = names.map((n) => ({ label: n, value: n }));
    if (value && !opts.some((o) => o.value === value)) {
      opts.unshift({ label: value, value });
    }
    return opts;
  }, [names, value]);
  return (
    <InlineField
      label="Attribute"
      labelWidth={LABEL_WIDTH}
      tooltip="The model attribute whose values to list, e.g. Line."
    >
      <Combobox
        width={30}
        createCustomValue
        placeholder="Pick an attribute"
        options={options}
        value={value ?? null}
        onChange={(o) => onChange(o?.value)}
      />
    </InlineField>
  );
}
