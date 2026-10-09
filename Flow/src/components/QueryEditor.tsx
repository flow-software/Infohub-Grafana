import React, { useEffect, useMemo, useState } from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2, QueryEditorProps } from '@grafana/data';
import {
  Button,
  Combobox,
  ComboboxOption,
  Icon,
  IconButton,
  InlineField,
  InlineSwitch,
  Input,
  RadioButtonGroup,
  Stack,
  useStyles2,
} from '@grafana/ui';

import { DataSource } from '../datasource';
import {
  AGGREGATIONS,
  DepDirection,
  Calendar,
  CALENDAR_INTERVALS,
  FlowOptions,
  FlowQuery,
  itemKey,
  QueryItem,
  QueryType,
  SelectionSummary,
  TagBucket,
} from '../types';
import { ChartBrowser } from './ChartBrowser';
import { ModelBrowser } from './ModelBrowser';

type Props = QueryEditorProps<DataSource, FlowQuery, FlowOptions>;

const LABEL_WIDTH = 14;

const queryTypeOptions: Array<{ label: string; value: QueryType; description: string }> = [
  { label: 'Measures', value: 'measures', description: 'Pick metrics, measures and tags from the model or templates' },
  { label: 'Chart', value: 'chart', description: 'Show a Flow chart as a table, with its limits' },
  { label: 'Dependencies', value: 'dependencies', description: 'How measures are built from each other, as a node graph' },
];

/**
 * Legend presets. Each value is a pattern the backend expands; a pattern
 * typed by hand (or saved before the dropdown) shows as a custom value.
 */
const legendOptions: Array<ComboboxOption<string>> = [
  { label: 'Full path', value: '', description: 'Site/Area/Line 1/Production/Good Count' },
  { label: 'Full path and unit', value: '{{path}} ({{uom}})', description: 'Site/Area/Line 1/Production/Good Count (kg)' },
  { label: 'Folder and measure', value: '{{folder}} · {{measure}}', description: 'Site/Area/Line 1 · Good Count' },
  { label: 'Metric and measure', value: '{{metric}}.{{measure}}', description: 'Production.Good Count; the same on every template instance' },
  { label: 'Measure', value: '{{measure}}', description: 'Good Count; the same on every template instance' },
  { label: 'Measure and unit', value: '{{measure}} ({{uom}})', description: 'Good Count (kg); the same on every template instance' },
  { label: 'Folder', value: '{{folder}}', description: 'Site/Area/Line 1' },
];

const bucketOptions: Array<{ label: string; value: TagBucket; description: string }> = [
  { label: 'Auto', value: 'auto', description: "The panel's interval" },
  { label: 'Fixed', value: 'fixed', description: 'A fixed bucket size' },
  { label: 'Calendar', value: 'calendar', description: 'Calendar periods, such as shifts' },
];

export function QueryEditor(props: Props) {
  const { query, onChange, onRunQuery } = props;
  const queryType = query.queryType ?? 'measures';

  return (
    <Stack direction="column" gap={0.5}>
      <InlineField label="Query" labelWidth={LABEL_WIDTH}>
        <RadioButtonGroup
          options={queryTypeOptions}
          value={queryType}
          onChange={(v) => {
            onChange({ ...query, queryType: v });
            onRunQuery();
          }}
        />
      </InlineField>
      {queryType === 'chart' && <ChartQueryEditor {...props} />}
      {queryType === 'dependencies' && <DependenciesQueryEditor {...props} />}
      {queryType === 'measures' && <MeasuresQueryEditor {...props} />}
    </Stack>
  );
}

function ChartQueryEditor({ datasource, query, onChange, onRunQuery }: Props) {
  const styles = useStyles2(getStyles);
  const [browsing, setBrowsing] = useState(false);

  const update = (patch: Partial<FlowQuery>) => {
    onChange({ ...query, ...patch });
    onRunQuery();
  };

  return (
    <>
      <InlineField
        label="Chart"
        labelWidth={LABEL_WIDTH}
        grow
        tooltip="A time-based Flow chart, such as a time-based table, shown as Flow lays it out for the periods in the dashboard's time range. Its measures, sections and limits come from the chart as it is set up in Flow. Values outside a limit are coloured with the limit's colours; values within it are green."
      >
        <div className={styles.itemsRow}>
          <Button variant="secondary" icon="folder-open" className={styles.browseButton} onClick={() => setBrowsing(true)}>
            Choose chart
          </Button>
          <div className={styles.chips}>
            {query.chartId ? (
              <span className={styles.chip} title={`Chart ${query.chartId}`}>
                <Icon name="table" size="sm" />
                <span className={styles.chipLabel}>{query.chartLabel || `Chart ${query.chartId}`}</span>
              </span>
            ) : (
              <span className={styles.placeholder}>No chart selected</span>
            )}
          </div>
        </div>
      </InlineField>
      {browsing && (
        <ChartBrowser
          datasource={datasource}
          selectedId={query.chartId}
          onDismiss={() => setBrowsing(false)}
          onApply={(chart) => {
            setBrowsing(false);
            update({ chartId: chart.id, chartLabel: chart.label });
          }}
        />
      )}
    </>
  );
}

/**
 * Asks the backend which kinds of measures the items hold, so only the
 * options that apply are shown. A metric's measures are only known once
 * resolved. Undefined while unknown (loading or failed): show everything.
 */
function useSummary(datasource: DataSource, items: QueryItem[]): SelectionSummary | undefined {
  const key = items.map(itemKey).join(',');
  const [result, setResult] = useState<{ key: string; summary?: SelectionSummary }>();
  useEffect(() => {
    if (!key) {
      return;
    }
    let cancelled = false;
    datasource
      .getSummary(items)
      .then((summary) => !cancelled && setResult({ key, summary }))
      .catch(() => !cancelled && setResult({ key }));
    return () => {
      cancelled = true;
    };
    // items is represented by key.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [datasource, key]);
  return result?.key === key ? result.summary : undefined;
}

function MeasuresQueryEditor({ datasource, query, onChange, onRunQuery }: Props) {
  const [calendars, setCalendars] = useState<Calendar[]>([]);
  const [calendarError, setCalendarError] = useState<string>();

  useEffect(() => {
    let cancelled = false;
    datasource
      .getCalendars()
      .then((list) => !cancelled && setCalendars(list))
      .catch((err) => !cancelled && setCalendarError(err?.data?.message ?? err?.message ?? 'Could not load calendars'));
    return () => {
      cancelled = true;
    };
  }, [datasource]);

  const items = useMemo(() => query.items ?? [], [query.items]);
  const summary = useSummary(datasource, items);
  const showPeriods = items.length > 0 && (!summary || summary.periodMeasures > 0);
  const showTags = items.length > 0 && (!summary || summary.tags > 0);
  const aggregated = !!query.aggregation && query.aggregation !== 'raw';
  const bucket = query.bucket ?? 'auto';
  // Calendar buckets for tags read the query's calendar too.
  const showCalendar = showPeriods || (showTags && aggregated && bucket === 'calendar');

  const update = (patch: Partial<FlowQuery>) => {
    onChange({ ...query, ...patch });
    onRunQuery();
  };

  const calendarOptions = useMemo<Array<ComboboxOption<number>>>(
    () => [
      { label: 'Datasource default', value: 0 },
      ...calendars.map((c) => ({ label: c.name, value: c.id, description: c.description })),
    ],
    [calendars]
  );

  const aggregationOptions = useMemo<Array<ComboboxOption<string>>>(
    () => [
      { label: 'Auto', value: '', description: 'Raw values while they fit the panel, else averages per panel interval' },
      { label: 'Raw values', value: 'raw', description: 'Every stored value; slow over long ranges' },
      ...AGGREGATIONS,
    ],
    []
  );

  return (
    <>
      <ItemsPicker
        datasource={datasource}
        items={items}
        onChange={(next) => update({ items: next })}
        tooltip="Pick metrics and measures from the model or from templates. A metric returns all its measures; a template item returns every instance created from it."
      />

      {showPeriods && <GroupHeading title="Period measures" count={summary?.periodMeasures} />}
      {showCalendar && (
        <InlineField
          label="Calendar"
          labelWidth={LABEL_WIDTH}
          invalid={!!calendarError}
          error={calendarError}
          tooltip="Period measures (hourly, shift, daily, ...) are read in this calendar; the measure must have it as context in Flow. Calendar buckets of tag aggregations use it too."
        >
          <Combobox
            id="query-editor-calendar"
            width={30}
            options={calendarOptions}
            value={query.calendarId ?? 0}
            onChange={(o) => update({ calendarId: o?.value || undefined })}
          />
        </InlineField>
      )}
      {showPeriods && (
        <InlineField label="Time at" labelWidth={LABEL_WIDTH} tooltip="Place each period's value at the start or the end of the period.">
          <RadioButtonGroup<'start' | 'end'>
            options={[
              { label: 'Period start', value: 'start' },
              { label: 'Period end', value: 'end' },
            ]}
            value={query.timeAt ?? 'start'}
            onChange={(v) => update({ timeAt: v })}
          />
        </InlineField>
      )}

      {showTags && <GroupHeading title="Tags" count={summary?.tags} />}
      {showTags && (
        <InlineField
          label="Values"
          labelWidth={LABEL_WIDTH}
          tooltip="Auto returns raw values when the panel can show them all, and averages per panel interval when there are more. Raw values returns every stored value. An aggregation buckets values by the panel interval, a fixed size, or calendar periods."
        >
          <Stack gap={1} wrap="wrap">
            <Combobox
              id="query-editor-aggregation"
              width={24}
              options={aggregationOptions}
              value={query.aggregation ?? ''}
              onChange={(o) => update({ aggregation: o?.value || undefined })}
            />
            {aggregated && (
              <RadioButtonGroup options={bucketOptions} value={bucket} onChange={(v) => update({ bucket: v })} />
            )}
            {aggregated && bucket === 'fixed' && (
              <Input
                id="query-editor-resolution"
                width={12}
                placeholder="15m"
                defaultValue={query.resolution ?? ''}
                onBlur={(e) => update({ resolution: e.currentTarget.value.trim() || undefined })}
              />
            )}
            {aggregated && bucket === 'calendar' && (
              <Combobox
                id="query-editor-tag-interval"
                width={16}
                options={CALENDAR_INTERVALS}
                value={query.tagInterval ?? 'hourly'}
                onChange={(o) => update({ tagInterval: o?.value })}
              />
            )}
          </Stack>
        </InlineField>
      )}

      {items.length > 0 && <GroupHeading title="Display" />}
      {items.length > 0 && (
        <>
          <InlineField
            label="Legend"
            labelWidth={LABEL_WIDTH}
            tooltip="How each series is named. Pick a preset, or type your own pattern with {{path}}, {{folder}}, {{metric}}, {{measure}}, {{id}} and {{uom}}. Template instances share their metric and measure names, so keep the folder or path in the name to tell them apart."
          >
            <Combobox
              id="query-editor-legend"
              width={50}
              options={legendOptions}
              createCustomValue
              value={query.legend ?? ''}
              onChange={(o) => update({ legend: o?.value || undefined })}
            />
          </InlineField>
          <InlineField
            label="Quality"
            labelWidth={LABEL_WIDTH}
            tooltip="Quality field: add each value's quality (Good, Uncertain, Bad) as an extra field; leave off for alert rules. Bad quality as gaps: blank out values whose quality is not Good."
          >
            <Stack gap={1}>
              <InlineSwitch
                id="query-editor-include-quality"
                label="Quality field"
                showLabel
                value={!!query.includeQuality}
                onChange={(e) => update({ includeQuality: e.currentTarget.checked })}
              />
              <InlineSwitch
                id="query-editor-bad-as-null"
                label="Bad quality as gaps"
                showLabel
                value={!!query.badQualityAsNull}
                onChange={(e) => update({ badQualityAsNull: e.currentTarget.checked })}
              />
            </Stack>
          </InlineField>
        </>
      )}
    </>
  );
}

/** The Browse button and the chips of the picked metrics and measures. */
function ItemsPicker({
  datasource,
  items,
  onChange,
  tooltip,
}: {
  datasource: DataSource;
  items: QueryItem[];
  onChange: (items: QueryItem[]) => void;
  tooltip: string;
}) {
  const styles = useStyles2(getStyles);
  const [browsing, setBrowsing] = useState(false);
  return (
    <>
      <InlineField label="Measures" labelWidth={LABEL_WIDTH} grow tooltip={tooltip}>
        <div className={styles.itemsRow}>
          <Button variant="secondary" icon="folder-open" className={styles.browseButton} onClick={() => setBrowsing(true)}>
            Browse
          </Button>
          <div className={styles.chips}>
            {items.length === 0 && <span className={styles.placeholder}>Nothing selected</span>}
            {items.map((it) => (
              <ItemChip key={itemKey(it)} item={it} onRemove={() => onChange(items.filter((x) => x !== it))} />
            ))}
          </div>
        </div>
      </InlineField>
      {browsing && (
        // Mounted only while open, so it starts from the query's current items each time.
        <ModelBrowser
          datasource={datasource}
          selected={items}
          onDismiss={() => setBrowsing(false)}
          onApply={(next) => {
            setBrowsing(false);
            onChange(next);
          }}
        />
      )}
    </>
  );
}

function DependenciesQueryEditor({ datasource, query, onChange, onRunQuery }: Props) {
  const items = useMemo(() => query.items ?? [], [query.items]);
  const update = (patch: Partial<FlowQuery>) => {
    onChange({ ...query, ...patch });
    onRunQuery();
  };
  return (
    <>
      <ItemsPicker
        datasource={datasource}
        items={items}
        onChange={(next) => update({ items: next })}
        tooltip="Each picked measure is the root of a tree. A metric adds a tree for each of its measures."
      />
      <InlineField
        label="Direction"
        labelWidth={LABEL_WIDTH}
        tooltip="Built from: the measures each one is calculated, aggregated or set from, and theirs in turn. Used by: the measures calculated, aggregated or set from it. Used by reads how every measure is built, so it takes longer on large models."
      >
        <RadioButtonGroup<DepDirection>
          options={[
            { label: 'Built from', value: 'inputs' },
            { label: 'Used by', value: 'usedBy' },
          ]}
          value={query.depDirection ?? 'inputs'}
          onChange={(v) => update({ depDirection: v })}
        />
      </InlineField>
      <InlineField label="Depth" labelWidth={LABEL_WIDTH} tooltip="How many levels to follow from each measure. Default 10.">
        <Input
          id="query-editor-dep-depth"
          type="number"
          min={1}
          width={10}
          placeholder="10"
          defaultValue={query.depDepth ?? ''}
          onBlur={(e) => update({ depDepth: Number(e.currentTarget.value) || undefined })}
        />
      </InlineField>
      <InlineField
        label="Values"
        labelWidth={LABEL_WIDTH}
        tooltip="Show each model measure's latest value in the dashboard's time range: the latest period with a value for period measures (in the datasource's default calendar), the last value for tags. Template measures have no values."
      >
        <InlineSwitch
          id="query-editor-dep-values"
          label="Latest values"
          showLabel
          value={query.depValues !== 'none'}
          onChange={(e) => update({ depValues: e.currentTarget.checked ? undefined : 'none' })}
        />
      </InlineField>
      <InlineField
        label="Names"
        labelWidth={LABEL_WIDTH}
        tooltip="Full name: Flow's Metric.Measure name, with any parent measures. Measure name: only the measure's own name, which keeps nested measures short. Click a node for its full path either way."
      >
        <RadioButtonGroup<'full' | 'measure'>
          options={[
            { label: 'Full name', value: 'full' },
            { label: 'Measure name', value: 'measure' },
          ]}
          value={query.depNames ?? 'full'}
          onChange={(v) => update({ depNames: v === 'measure' ? 'measure' : undefined })}
        />
      </InlineField>
    </>
  );
}

function GroupHeading({ title, count }: { title: string; count?: number }) {
  const styles = useStyles2(getStyles);
  return (
    <div className={styles.group}>
      {title}
      {count !== undefined && <span className={styles.groupCount}>{count}</span>}
    </div>
  );
}

/** What an item is, for its chip: tag, a period such as hourly, or metric. */
function itemKind(item: QueryItem): string {
  if (item.type === 'metric') {
    return 'metric';
  }
  if (item.interval === 'timestamp') {
    return 'tag';
  }
  return item.interval || 'measure';
}

function ItemChip({ item, onRemove }: { item: QueryItem; onRemove: () => void }) {
  const styles = useStyles2(getStyles);
  const kind = itemKind(item);
  return (
    <span className={styles.chip} title={`${item.tree === 'template' ? 'Template ' : ''}${item.type} ${item.id}: ${item.label}`}>
      <Icon name={item.type === 'metric' ? 'apps' : kind === 'tag' ? 'chart-line' : 'clock-nine'} size="sm" />
      {item.tree === 'template' && <span className={styles.chipTag}>template</span>}
      <span className={styles.chipLabel}>{item.label}</span>
      <span className={styles.chipKind}>{kind}</span>
      <IconButton name="times" size="sm" aria-label={`Remove ${item.label}`} onClick={onRemove} />
    </span>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  itemsRow: css({
    display: 'flex',
    alignItems: 'flex-start',
    gap: theme.spacing(1),
    width: '100%',
  }),
  browseButton: css({ flexShrink: 0, height: theme.spacing(4) }),
  chips: css({
    display: 'flex',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    minHeight: theme.spacing(4),
    flex: 1,
    minWidth: 0,
  }),
  placeholder: css({ color: theme.colors.text.secondary }),
  chip: css({
    display: 'inline-flex',
    alignItems: 'center',
    gap: theme.spacing(0.5),
    maxWidth: '100%',
    padding: theme.spacing(0.25, 0.5, 0.25, 1),
    borderRadius: theme.shape.radius.default,
    background: theme.colors.background.secondary,
    border: `1px solid ${theme.colors.border.weak}`,
  }),
  chipTag: css({ fontSize: theme.typography.bodySmall.fontSize, color: theme.colors.primary.text }),
  chipKind: css({
    fontSize: theme.typography.bodySmall.fontSize,
    color: theme.colors.text.secondary,
    padding: theme.spacing(0, 0.5),
    borderRadius: theme.shape.radius.pill,
    border: `1px solid ${theme.colors.border.weak}`,
  }),
  chipLabel: css({ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }),
  group: css({
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(1),
    marginTop: theme.spacing(1),
    fontSize: theme.typography.bodySmall.fontSize,
    fontWeight: theme.typography.fontWeightMedium,
    color: theme.colors.text.secondary,
    textTransform: 'uppercase',
    letterSpacing: '0.04em',
  }),
  groupCount: css({
    padding: theme.spacing(0, 0.75),
    borderRadius: theme.shape.radius.pill,
    background: theme.colors.background.secondary,
    textTransform: 'none',
  }),
});
