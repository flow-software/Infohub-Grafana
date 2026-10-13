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
  MultiCombobox,
  RadioButtonGroup,
  Stack,
  useStyles2,
} from '@grafana/ui';

import type { BrowseKind, DataSource } from '../datasource';
import {
  AGGREGATIONS,
  AttributeValueOption,
  ContextEvent,
  EventFormat,
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
import { AttributeFilterField } from './AttributeFilterField';
import { ChartBrowser } from './ChartBrowser';
import { hasIntervalIcon, IntervalIcon } from './IntervalIcon';
import { ModelBrowser } from './ModelBrowser';

type Props = QueryEditorProps<DataSource, FlowQuery, FlowOptions>;

export const LABEL_WIDTH = 14;

const queryTypeOptions: Array<{ label: string; value: QueryType; description: string }> = [
  { label: 'Measures', value: 'measures', description: 'Pick metrics, measures and tags from the model or templates' },
  { label: 'Events', value: 'events', description: 'Event periods and their attributes, as a table or a timeline' },
  { label: 'Chart', value: 'chart', description: 'Show a Flow chart as a table, with its limits' },
  {
    label: 'Dependencies',
    value: 'dependencies',
    description: 'How measures are built from each other, as a node graph',
  },
];

/**
 * Legend presets. Each value is a pattern the backend expands; a pattern
 * typed by hand (or saved before the dropdown) shows as a custom value.
 */
const legendOptions: Array<ComboboxOption<string>> = [
  { label: 'Full path', value: '', description: 'Site/Area/Line 1/Production/Good Count' },
  {
    label: 'Full path and unit',
    value: '{{path}} ({{uom}})',
    description: 'Site/Area/Line 1/Production/Good Count (kg)',
  },
  { label: 'Folder and measure', value: '{{folder}} · {{measure}}', description: 'Site/Area/Line 1 · Good Count' },
  {
    label: 'Metric and measure',
    value: '{{metric}}.{{measure}}',
    description: 'Production.Good Count; the same on every template instance',
  },
  { label: 'Measure', value: '{{measure}}', description: 'Good Count; the same on every template instance' },
  {
    label: 'Measure and unit',
    value: '{{measure}} ({{uom}})',
    description: 'Good Count (kg); the same on every template instance',
  },
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
            // Events and measures cannot be mixed, so crossing over clears the picks.
            const crossing = (v === 'events') !== (queryType === 'events');
            onChange({ ...query, queryType: v, ...(crossing ? { items: [] } : {}) });
            onRunQuery();
          }}
        />
      </InlineField>
      {queryType === 'chart' && <ChartQueryEditor {...props} />}
      {queryType === 'dependencies' && <DependenciesQueryEditor {...props} />}
      {queryType === 'measures' && <MeasuresQueryEditor {...props} />}
      {queryType === 'events' && <EventsQueryEditor {...props} />}
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
          <Button
            variant="secondary"
            icon="folder-open"
            className={styles.browseButton}
            onClick={() => setBrowsing(true)}
          >
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
  const styles = useStyles2(getStyles);
  const items = useMemo(() => query.items ?? [], [query.items]);
  const summary = useSummary(datasource, items);
  // Measures from a variable are unknown until the panel runs, so their
  // period options show too.
  const fromVariable = !!query.measureVariable;
  const showPeriods = (items.length > 0 && (!summary || summary.periodMeasures > 0)) || fromVariable;
  const showTags = items.length > 0 && (!summary || summary.tags > 0);
  const aggregated = !!query.aggregation && query.aggregation !== 'raw';
  const bucket = query.bucket ?? 'auto';
  const eventContext = query.context === 'event';
  // Calendar buckets for tags read the query's calendar too.
  const showCalendar = (showPeriods && !eventContext) || (showTags && aggregated && bucket === 'calendar');

  const update = (patch: Partial<FlowQuery>) => {
    onChange({ ...query, ...patch });
    onRunQuery();
  };

  const aggregationOptions = useMemo<Array<ComboboxOption<string>>>(
    () => [
      {
        label: 'Auto',
        value: '',
        description: 'Raw values while they fit the panel, else averages per panel interval',
      },
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
      <InlineField
        label="Variable"
        labelWidth={LABEL_WIDTH}
        tooltip="A dashboard variable holding measure ids, such as a Measures variable of this data source: $measure. Its measures are added to those picked above; with Multi-value or All, every selected one."
      >
        <Input
          id="query-editor-measure-variable"
          width={30}
          placeholder="$measure"
          defaultValue={query.measureVariable ?? ''}
          onBlur={(e) => update({ measureVariable: e.currentTarget.value.trim() || undefined })}
        />
      </InlineField>
      {(items.length > 0 || fromVariable) && (
        <AttributeFilterField
          datasource={datasource}
          label="Filter"
          labelWidth={LABEL_WIDTH}
          tooltip="Keep only measures whose model attribute has one of these values, e.g. Line = Line 1. Pick a template measure above and a $line variable here to get one dashboard for every line."
          attribute={query.attributeFilter}
          values={query.attributeValues}
          onChange={(attributeFilter, attributeValues) => update({ attributeFilter, attributeValues })}
        />
      )}
      {query.refId === ANNOTATION_REF_ID && (
        <InlineField label="Shown as" labelWidth={LABEL_WIDTH}>
          <span className={styles.hint}>
            The comments on these measures&apos; values are marked on the time series panels of this dashboard, with who
            wrote them and when.
          </span>
        </InlineField>
      )}

      {showPeriods && <GroupHeading title="Period measures" count={summary?.periodMeasures} />}
      {showPeriods && (
        <InlineField
          label="Context"
          labelWidth={LABEL_WIDTH}
          tooltip="Calendar: a value per calendar period (hour, shift, day, ...). Event: a value per period of an event the measures have as context in Flow, such as a batch or a product run."
        >
          <RadioButtonGroup<'calendar' | 'event'>
            options={[
              { label: 'Calendar', value: 'calendar' },
              { label: 'Event', value: 'event' },
            ]}
            value={eventContext ? 'event' : 'calendar'}
            onChange={(v) =>
              // Event periods are best drawn whole; calendar periods have no such option.
              update(
                v === 'event'
                  ? { context: 'event', timeAt: 'period' }
                  : { context: undefined, timeAt: query.timeAt === 'period' ? 'start' : query.timeAt }
              )
            }
          />
        </InlineField>
      )}
      {showPeriods && eventContext && (
        <EventContextFields datasource={datasource} query={query} items={items} update={update} />
      )}
      {showCalendar && (
        <CalendarField
          datasource={datasource}
          value={query.calendarId}
          onChange={(calendarId) => update({ calendarId })}
          tooltip="Period measures (hourly, shift, daily, ...) are read in this calendar; the measure must have it as context in Flow. Calendar buckets of tag aggregations use it too."
        />
      )}
      {showPeriods && (
        <InlineField
          label="Time at"
          labelWidth={LABEL_WIDTH}
          tooltip={
            eventContext
              ? "Whole period draws each value as a line from its event period's start to its end, with a gap until the next period, so you see when each period ran. Period start or end places the value at one of them; a running period has no end yet, so its value stays at the start."
              : "Place each period's value at the start or the end of the period."
          }
        >
          <RadioButtonGroup<'start' | 'end' | 'period'>
            options={[
              ...(eventContext ? [{ label: 'Whole period', value: 'period' as const }] : []),
              { label: 'Period start', value: 'start' },
              { label: 'Period end', value: 'end' },
            ]}
            value={!eventContext && query.timeAt === 'period' ? 'start' : (query.timeAt ?? 'start')}
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

      {(items.length > 0 || fromVariable) && query.refId !== ANNOTATION_REF_ID && <GroupHeading title="Display" />}
      {(items.length > 0 || fromVariable) && query.refId !== ANNOTATION_REF_ID && (
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
          <LimitsField datasource={datasource} query={query} items={items} update={update} />
        </>
      )}
    </>
  );
}

/** The Browse button and the chips of the picked metrics and measures, or events. */
export function ItemsPicker({
  datasource,
  items,
  onChange,
  tooltip,
  label = 'Measures',
  kind,
}: {
  datasource: DataSource;
  items: QueryItem[];
  onChange: (items: QueryItem[]) => void;
  tooltip: string;
  label?: string;
  kind?: BrowseKind;
}) {
  const styles = useStyles2(getStyles);
  const [browsing, setBrowsing] = useState(false);
  return (
    <>
      <InlineField label={label} labelWidth={LABEL_WIDTH} grow tooltip={tooltip}>
        <div className={styles.itemsRow}>
          <Button
            variant="secondary"
            icon="folder-open"
            className={styles.browseButton}
            onClick={() => setBrowsing(true)}
          >
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
          kind={kind}
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

// Annotations are not offered here: an annotation query (dashboard settings)
// asks for that layout itself.
const eventFormatOptions: Array<{ label: string; value: EventFormat; description: string }> = [
  { label: 'Table', value: 'table', description: 'One row per event period, attribute cells in Flow colours' },
  { label: 'Timeline', value: 'timeline', description: 'A band per attribute for the State timeline panel' },
];

/** Grafana gives annotation queries this refId. */
const ANNOTATION_REF_ID = 'Anno';

function EventsQueryEditor({ datasource, query, onChange, onRunQuery }: Props) {
  const styles = useStyles2(getStyles);
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
        label="Events"
        kind="events"
        onChange={(next) => update({ items: next })}
        tooltip="Pick events from the model or from templates. A template event returns the periods of every event created from it."
      />
      {query.refId === ANNOTATION_REF_ID ? (
        <InlineField label="Shown as" labelWidth={LABEL_WIDTH}>
          <span className={styles.hint}>
            Each event period is shaded on the time series panels of this dashboard, titled with the event and labelled
            with its attributes.
          </span>
        </InlineField>
      ) : (
        <InlineField
          label="Format"
          labelWidth={LABEL_WIDTH}
          tooltip="Table: one row per event period with its start, end, duration and attributes, coloured as in Flow; use the Table panel. Timeline: a band per attribute; use the State timeline panel. To shade event periods on other panels, add an annotation query with this data source in the dashboard settings."
        >
          <RadioButtonGroup<EventFormat>
            options={eventFormatOptions}
            value={query.eventFormat === 'timeline' ? 'timeline' : 'table'}
            onChange={(v) => update({ eventFormat: v })}
          />
        </InlineField>
      )}
    </>
  );
}

/**
 * Picks the event that period measures are read in, and optionally an
 * attribute to filter periods by or to split series on.
 */
function EventContextFields({
  datasource,
  query,
  items,
  update,
}: {
  datasource: DataSource;
  query: FlowQuery;
  items: QueryItem[];
  update: (patch: Partial<FlowQuery>) => void;
}) {
  // Answers are kept with the request they answer, so a stale one never shows.
  const [eventsAnswer, setEventsAnswer] = useState<{ key: string; list?: ContextEvent[]; error?: string }>();
  const [valuesAnswer, setValuesAnswer] = useState<{ key: string; list: AttributeValueOption[] }>();

  const modelItems = useMemo(() => items.filter((it) => it.tree === 'model'), [items]);
  const modelKey = modelItems.map(itemKey).join(',');
  useEffect(() => {
    if (!modelKey) {
      return;
    }
    let cancelled = false;
    datasource
      .getContextEvents(modelItems)
      .then((list) => !cancelled && setEventsAnswer({ key: modelKey, list }))
      .catch(
        (err) =>
          !cancelled &&
          setEventsAnswer({ key: modelKey, error: err?.data?.message ?? err?.message ?? 'Could not load events' })
      );
    return () => {
      cancelled = true;
    };
    // modelKey stands for modelItems, whose identity changes with every new query object.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [datasource, modelKey]);
  const answered = eventsAnswer?.key === modelKey ? eventsAnswer : undefined;
  const events = useMemo<ContextEvent[] | undefined>(() => (!modelKey ? [] : answered?.list), [modelKey, answered]);
  const error = answered?.error;

  const event = events?.find((e) => e.id === query.eventId);
  const attributes = event?.attributes ?? [];
  const valuesKey = query.eventId && query.eventAttribute ? `${query.eventId}:${query.eventAttribute}` : '';
  useEffect(() => {
    if (!valuesKey || !query.eventId || !query.eventAttribute) {
      return;
    }
    let cancelled = false;
    datasource
      .getEventValues(query.eventId, query.eventAttribute)
      .then((list) => !cancelled && setValuesAnswer({ key: valuesKey, list }))
      .catch(() => !cancelled && setValuesAnswer({ key: valuesKey, list: [] }));
    return () => {
      cancelled = true;
    };
  }, [datasource, valuesKey, query.eventId, query.eventAttribute]);
  const values = useMemo(() => (valuesAnswer?.key === valuesKey ? valuesAnswer.list : []), [valuesAnswer, valuesKey]);

  const eventOptions = useMemo<Array<ComboboxOption<number>>>(() => {
    const opts = (events ?? []).map((e) => ({
      label: e.name,
      value: e.id,
      description: `Context of ${e.measures} of the selected measures`,
    }));
    // Keep a saved event visible before the list loads, or when it is no longer context.
    if (query.eventId && !opts.some((o) => o.value === query.eventId)) {
      opts.unshift({ label: query.eventLabel ?? `Event ${query.eventId}`, value: query.eventId, description: '' });
    }
    return opts;
  }, [events, query.eventId, query.eventLabel]);

  const valueOptions = useMemo<Array<ComboboxOption<string>>>(() => {
    const opts: Array<ComboboxOption<string>> = values.map((v) => ({ label: v.value, value: v.value }));
    for (const v of query.eventValues ?? []) {
      if (!opts.some((o) => o.value === v)) {
        opts.push({ label: v, value: v });
      }
    }
    return opts;
  }, [values, query.eventValues]);

  const noEvents = events !== undefined && events.length === 0 && !error && modelItems.length > 0;
  const templateOnly = items.length > 0 && modelItems.length === 0;
  let problem = error;
  if (!problem && templateOnly) {
    problem = 'Event context reads model measures only; each template instance has its own event';
  } else if (!problem && noEvents) {
    problem = 'None of the selected measures has an event as context in Flow';
  }
  return (
    <>
      <InlineField
        label="Event"
        labelWidth={LABEL_WIDTH}
        invalid={!!problem}
        error={problem}
        tooltip="The event whose periods the measures are read in. Listed: events the selected model measures have as context in Flow, most shared first."
      >
        <Combobox
          id="query-editor-event"
          width={40}
          options={eventOptions}
          value={query.eventId ?? null}
          placeholder={events === undefined && !error ? 'Loading events...' : 'Pick an event'}
          onChange={(o) =>
            update({
              eventId: o?.value,
              eventLabel: o?.label,
              eventAttribute: undefined,
              eventValues: undefined,
              eventSplit: undefined,
            })
          }
        />
      </InlineField>
      {query.eventId !== undefined && attributes.length > 0 && (
        <InlineField
          label="Attribute"
          labelWidth={LABEL_WIDTH}
          tooltip="Keep only periods whose attribute has one of the picked values, and/or give each value its own series. The values listed are those seen in the last 90 days; others can be typed."
        >
          <Stack gap={1} wrap="wrap" alignItems="center">
            <Combobox
              id="query-editor-event-attribute"
              width={20}
              isClearable
              placeholder="None"
              options={attributes.map((a) => ({ label: a, value: a }))}
              value={query.eventAttribute ?? null}
              onChange={(o) =>
                update({
                  eventAttribute: o?.value,
                  eventValues: undefined,
                  eventSplit: o ? query.eventSplit : undefined,
                })
              }
            />
            {query.eventAttribute && (
              <MultiCombobox
                id="query-editor-event-values"
                width={40}
                placeholder="All values"
                createCustomValue
                options={valueOptions}
                value={query.eventValues ?? []}
                onChange={(opts) => update({ eventValues: opts.length ? opts.map((o) => o.value) : undefined })}
              />
            )}
            {query.eventAttribute && (
              <InlineSwitch
                id="query-editor-event-split"
                label="Series per value"
                showLabel
                value={!!query.eventSplit}
                onChange={(e) => update({ eventSplit: e.currentTarget.checked || undefined })}
              />
            )}
          </Stack>
        </InlineField>
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
      <InlineField
        label="Depth"
        labelWidth={LABEL_WIDTH}
        tooltip="How many levels to follow from each measure. Default 10."
      >
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
      {query.depValues !== 'none' && (
        <CalendarField
          datasource={datasource}
          value={query.calendarId}
          onChange={(calendarId) => update({ calendarId })}
          tooltip="Period measures show their latest value in this calendar. A measure that does not have the calendar as context in Flow shows a dash; click it for why. Tags do not use a calendar."
        />
      )}
      {query.depValues !== 'none' && (
        <InlineField
          label="Limits"
          labelWidth={LABEL_WIDTH}
          tooltip="Turn a node red when its latest value is outside its limits in this calendar, as Flow flags it; click the node for the limit it breaks. Off, nodes keep their retrieval type colour."
        >
          <InlineSwitch
            id="query-editor-dep-limits"
            label="Show limits"
            showLabel
            value={query.limits === 'lines'}
            onChange={(e) => update({ limits: e.currentTarget.checked ? 'lines' : undefined })}
          />
        </InlineField>
      )}
      <InlineField
        label="Type as"
        labelWidth={LABEL_WIDTH}
        tooltip="How each node shows its retrieval type. Colour: the ring's colour (blue calculated, orange aggregated, purple manual, green retrieved, light blue replicated), named in the legend. Symbol: a symbol under the value (ƒx calculated, Σ aggregated, ✎ manual, ⇣ retrieved, ⇄ replicated) with grey rings; the legend lists the symbols. Either way, a node outside its limits is filled red when Show limits is on."
      >
        <RadioButtonGroup<'colour' | 'symbol'>
          options={[
            { label: 'Colour', value: 'colour' },
            { label: 'Symbol', value: 'symbol' },
          ]}
          value={query.depTypeMark === 'symbol' ? 'symbol' : 'colour'}
          onChange={(v) => update({ depTypeMark: v === 'symbol' ? 'symbol' : undefined })}
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

/**
 * Shows the measures' Flow limits as dashed lines. Limits belong to a
 * calendar (or event) in Flow, so only those of the query's calendar are
 * offered. A measure can have several (Site, Zone, ...), and drawing every
 * set at once is hard to read, so they are picked by name; switching on
 * picks the first.
 */
function LimitsField({
  datasource,
  query,
  items,
  update,
}: {
  datasource: DataSource;
  query: FlowQuery;
  items: QueryItem[];
  update: (patch: Partial<FlowQuery>) => void;
}) {
  const eventId = query.context === 'event' ? query.eventId : undefined;
  const key = [items.map(itemKey).join(','), query.calendarId ?? 0, eventId ?? 0].join('|');
  const [answer, setAnswer] = useState<{ key: string; names: string[] }>();
  useEffect(() => {
    if (!items.length) {
      return;
    }
    let cancelled = false;
    datasource
      .getLimitNames(items, query.calendarId, eventId)
      .then((names) => !cancelled && setAnswer({ key, names }))
      .catch(() => !cancelled && setAnswer({ key, names: [] }));
    return () => {
      cancelled = true;
    };
    // key stands for items, calendar and event.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [datasource, key]);
  const names = useMemo(() => (answer?.key === key ? answer.names : undefined), [answer, key]);

  const on = query.limits === 'lines';
  const options = useMemo<Array<ComboboxOption<string>>>(() => {
    const opts: Array<ComboboxOption<string>> = (names ?? []).map((n) => ({ label: n, value: n }));
    for (const n of query.limitNames ?? []) {
      if (!opts.some((o) => o.value === n)) {
        opts.push({ label: n, value: n, description: 'Not set for these measures in this calendar' });
      }
    }
    return opts;
  }, [names, query.limitNames]);

  return (
    <InlineField
      label="Limits"
      labelWidth={LABEL_WIDTH}
      tooltip="Draw the measures' limits from Flow as dashed lines: bounds red, targets green. Only the limits of the query's calendar (or event) are offered, as they apply now. Pick which limits to draw; with none picked, all are drawn. Limits that take their value from another measure are listed in the series description but not drawn."
    >
      <Stack gap={1} wrap="wrap" alignItems="center">
        <InlineSwitch
          id="query-editor-limits"
          label="Show limits"
          showLabel
          value={on}
          onChange={(e) =>
            update(
              e.currentTarget.checked
                ? { limits: 'lines', limitNames: names?.length ? [names[0]] : undefined }
                : { limits: undefined, limitNames: undefined }
            )
          }
        />
        {on && (
          <MultiCombobox
            width={40}
            placeholder={names !== undefined && names.length === 0 ? 'No limits in this calendar' : 'All limits'}
            options={options}
            value={query.limitNames ?? []}
            onChange={(opts) => update({ limitNames: opts.length ? opts.map((o) => o.value) : undefined })}
          />
        )}
      </Stack>
    </InlineField>
  );
}

/** Picks the Flow calendar period measures are read in; unset is the datasource default. */
function CalendarField({
  datasource,
  value,
  onChange,
  tooltip,
}: {
  datasource: DataSource;
  value?: number;
  onChange: (calendarId: number | undefined) => void;
  tooltip: string;
}) {
  const [calendars, setCalendars] = useState<Calendar[]>([]);
  const [error, setError] = useState<string>();

  useEffect(() => {
    let cancelled = false;
    datasource
      .getCalendars()
      .then((list) => !cancelled && setCalendars(list))
      .catch((err) => !cancelled && setError(err?.data?.message ?? err?.message ?? 'Could not load calendars'));
    return () => {
      cancelled = true;
    };
  }, [datasource]);

  const options = useMemo<Array<ComboboxOption<number>>>(
    () => [
      { label: 'Datasource default', value: 0 },
      ...calendars.map((c) => ({ label: c.name, value: c.id, description: c.description })),
    ],
    [calendars]
  );

  return (
    <InlineField label="Calendar" labelWidth={LABEL_WIDTH} invalid={!!error} error={error} tooltip={tooltip}>
      <Combobox
        id="query-editor-calendar"
        width={30}
        options={options}
        value={value ?? 0}
        onChange={(o) => onChange(o?.value || undefined)}
      />
    </InlineField>
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
  if (item.type === 'metric' || item.type === 'event') {
    return item.type;
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
    <span
      className={styles.chip}
      title={`${item.tree === 'template' ? 'Template ' : ''}${item.type} ${item.id}: ${item.label}`}
    >
      {item.type === 'measure' && hasIntervalIcon(item.interval) ? (
        <IntervalIcon interval={item.interval!} size={14} />
      ) : (
        <Icon
          name={
            item.type === 'metric'
              ? 'apps'
              : item.type === 'event'
                ? 'bolt'
                : kind === 'tag'
                  ? 'chart-line'
                  : 'clock-nine'
          }
          size="sm"
        />
      )}
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
  hint: css({ color: theme.colors.text.secondary, alignSelf: 'center', padding: theme.spacing(0, 1) }),
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
