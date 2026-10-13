import React, { useCallback, useEffect, useRef, useState } from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import {
  Alert,
  Button,
  Checkbox,
  FilterInput,
  Icon,
  IconButton,
  IconName,
  Modal,
  Spinner,
  Tab,
  TabsBar,
  useStyles2,
} from '@grafana/ui';

import type { BrowseKind, DataSource } from '../datasource';
import { MeasureIcon } from './MeasureIcon';
import { BrowseNode, itemKey, QueryItem, Tree } from '../types';

interface Props {
  datasource: DataSource;
  /** The query's current items. */
  selected: QueryItem[];
  onApply: (items: QueryItem[]) => void;
  onDismiss: () => void;
  /** "events" lists folders and events, for events queries. */
  kind?: BrowseKind;
}

type Children = { state: 'loading' } | { state: 'error'; message: string } | { state: 'done'; nodes: BrowseNode[] };

const SEARCH_DELAY_MS = 300;

function parentKey(tree: Tree, node?: Pick<BrowseNode, 'type' | 'id'>): string {
  return node ? `${tree}:${node.type}:${node.id}` : `${tree}:root`;
}

function errorText(err: unknown): string {
  const e = err as { data?: { message?: string }; message?: string };
  return e?.data?.message ?? e?.message ?? 'Request failed';
}

/**
 * Browses the Flow model (or its templates) folder by folder, loading each
 * level on demand. Metrics and measures can be ticked: a metric stands for
 * all its measures, and a template item for every instance of it.
 */
export function ModelBrowser({ datasource, selected, onApply, onDismiss, kind = 'measures' }: Props) {
  const events = kind === 'events';
  const styles = useStyles2(getStyles);
  const [tree, setTree] = useState<Tree>(() => selected[0]?.tree ?? 'model');
  const [children, setChildren] = useState<Record<string, Children>>({});
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [checked, setChecked] = useState<Map<string, QueryItem>>(
    () => new Map(selected.map((it) => [itemKey(it), it]))
  );
  const [search, setSearch] = useState('');
  // Results are kept with the text they answer, so stale ones are never shown.
  const [results, setResults] = useState<{ q: string; tree: Tree; children: Children }>();
  // Levels already requested. A level with no entry in children yet shows as
  // loading, so nothing has to be set before the request returns.
  const requested = useRef(new Set<string>());

  const load = useCallback(
    (t: Tree, parent?: BrowseNode) => {
      const key = parentKey(t, parent);
      requested.current.add(key);
      datasource
        .browse(t, parent, kind)
        .then((nodes) => setChildren((prev) => ({ ...prev, [key]: { state: 'done', nodes } })))
        .catch((err) => setChildren((prev) => ({ ...prev, [key]: { state: 'error', message: errorText(err) } })));
    },
    [datasource, kind]
  );

  // Root folders of the open tab, loaded the first time the tab is shown.
  useEffect(() => {
    if (!requested.current.has(parentKey(tree))) {
      load(tree);
    }
  }, [tree, load]);

  // Server-side search, debounced while typing.
  const q = search.trim();
  useEffect(() => {
    if (!q) {
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      datasource
        .search(tree, q, kind)
        .then((nodes) => !cancelled && setResults({ q, tree, children: { state: 'done', nodes } }))
        .catch((err) => !cancelled && setResults({ q, tree, children: { state: 'error', message: errorText(err) } }));
    }, SEARCH_DELAY_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [q, tree, datasource, kind]);
  const current: Children =
    results && results.q === q && results.tree === tree ? results.children : { state: 'loading' };

  const toggleExpanded = (node: BrowseNode) => {
    const key = parentKey(node.tree, node);
    setExpanded((prev) => {
      const next = new Set(prev);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
    if (!requested.current.has(key) || children[key]?.state === 'error') {
      // An event handler, so the retry can show as loading straight away.
      setChildren((prev) => ({ ...prev, [key]: { state: 'loading' } }));
      load(node.tree, node);
    }
  };

  const toggleChecked = (node: BrowseNode, label: string) => {
    if (node.type === 'folder') {
      return;
    }
    const item: QueryItem = {
      tree: node.tree,
      type: node.type,
      id: node.id,
      label,
      interval: node.interval,
      retrieval: node.retrieval,
    };
    const key = itemKey(item);
    setChecked((prev) => {
      const next = new Map(prev);
      next.has(key) ? next.delete(key) : next.set(key, item);
      return next;
    });
  };

  const apply = () => {
    // Keep the query's order, then append new picks in the order ticked, so
    // legends do not reshuffle when one measure is added.
    const kept = selected.filter((it) => checked.has(itemKey(it)));
    const keptKeys = new Set(kept.map(itemKey));
    const added = [...checked.values()].filter((it) => !keptKeys.has(itemKey(it)));
    onApply([...kept, ...added]);
  };

  const renderRow = (node: BrowseNode, depth: number, path: string) => {
    const label = path ? `${path}/${node.name}` : node.name;
    const key = parentKey(node.tree, node);
    const canExpand = node.type === 'folder' || node.type === 'metric';
    const isOpen = expanded.has(key);
    const isChecked = node.type !== 'folder' && checked.has(itemKey({ tree: node.tree, type: node.type, id: node.id }));
    return (
      <React.Fragment key={key}>
        <div
          role="treeitem"
          aria-level={depth + 1}
          aria-expanded={canExpand ? isOpen : undefined}
          aria-selected={isChecked}
          className={styles.row}
          style={{ paddingLeft: depth * 20 }}
          title={node.description || undefined}
        >
          {canExpand ? (
            <IconButton
              name={isOpen ? 'angle-down' : 'angle-right'}
              aria-label={isOpen ? `Collapse ${node.name}` : `Expand ${node.name}`}
              onClick={() => toggleExpanded(node)}
            />
          ) : (
            <span className={styles.chevronSpace} />
          )}
          {node.type === 'folder' ? (
            <span className={styles.checkboxSpace} />
          ) : (
            <Checkbox
              value={isChecked}
              aria-label={`Select ${node.name}`}
              onChange={() => toggleChecked(node, label)}
            />
          )}
          {node.type === 'measure' ? (
            <MeasureIcon interval={node.interval} retrieval={node.retrieval} className={styles.icon} />
          ) : (
            <Icon name={iconFor(node, isOpen)} className={styles.icon} />
          )}
          <span
            className={styles.label}
            onClick={() => (node.type === 'folder' ? toggleExpanded(node) : toggleChecked(node, label))}
          >
            {node.name}
            {!!node.events?.length && (
              <Icon
                name="bolt"
                size="sm"
                className={styles.eventMark}
                title={`Event context: ${node.events.join(', ')}`}
                aria-label={`Has event context: ${node.events.join(', ')}`}
              />
            )}
            {node.description && <span className={styles.description}>{node.description}</span>}
          </span>
          <NodeBadges node={node} />
        </div>
        {canExpand && isOpen && renderChildren(node.tree, node, depth + 1, label)}
      </React.Fragment>
    );
  };

  const renderChildren = (t: Tree, parent: BrowseNode | undefined, depth: number, path: string) => {
    const state = children[parentKey(t, parent)];
    const pad = { paddingLeft: depth * 20 + 8 };
    if (!state || state.state === 'loading') {
      return (
        <div className={styles.note} style={pad}>
          <Spinner inline /> Loading…
        </div>
      );
    }
    if (state.state === 'error') {
      return (
        <div className={styles.error} style={pad}>
          {state.message}
        </div>
      );
    }
    if (state.nodes.length === 0) {
      return (
        <div className={styles.note} style={pad}>
          {parent ? 'Empty' : 'No folders. The API token may not have access to any.'}
        </div>
      );
    }
    return state.nodes.map((n) => renderRow(n, depth, path));
  };

  const renderResults = () => {
    if (current.state === 'loading') {
      return (
        <div className={styles.note}>
          <Spinner inline /> Searching…
        </div>
      );
    }
    if (current.state === 'error') {
      return <div className={styles.error}>{current.message}</div>;
    }
    if (current.nodes.length === 0) {
      return <div className={styles.note}>No metrics or measures match.</div>;
    }
    return current.nodes.map((n) => renderRow(n, 0, ''));
  };

  return (
    <Modal title="Browse Flow" isOpen onDismiss={onDismiss} className={styles.modal}>
      <TabsBar>
        <Tab label="Model" icon="sitemap" active={tree === 'model'} onChangeTab={() => setTree('model')} />
        <Tab label="Templates" icon="copy" active={tree === 'template'} onChangeTab={() => setTree('template')} />
      </TabsBar>

      {tree === 'template' && (
        <Alert severity="info" title="" className={styles.hint}>
          {events
            ? 'A template event returns the periods of every model event created from it.'
            : 'A template metric or measure returns one series for every model measure created from it.'}
        </Alert>
      )}

      <div className={styles.search}>
        <FilterInput
          // Names are searched as typed: "110-FQ" must not reach Flow as "110\-FQ".
          escapeRegex={false}
          placeholder={
            events
              ? tree === 'model'
                ? 'Search events by name'
                : 'Search template events by name'
              : tree === 'model'
                ? 'Search metrics and measures by name, or a measure by ID'
                : 'Search template metrics and measures by name, or a measure by ID'
          }
          value={search}
          onChange={setSearch}
          autoFocus
        />
      </div>

      <div className={styles.list} role="tree" aria-label="Flow model">
        {q ? renderResults() : renderChildren(tree, undefined, 0, '')}
      </div>

      <Modal.ButtonRow leftItems={<span className={styles.count}>{checked.size} selected</span>}>
        <Button variant="secondary" fill="outline" onClick={onDismiss}>
          Cancel
        </Button>
        <Button onClick={apply}>Apply</Button>
      </Modal.ButtonRow>
    </Modal>
  );
}

function iconFor(node: BrowseNode, isOpen: boolean): IconName {
  switch (node.type) {
    case 'folder':
      return isOpen ? 'folder-open' : 'folder';
    case 'metric':
      return 'apps';
    case 'event':
      return 'bolt';
    default:
      return node.interval === 'timestamp' ? 'chart-line' : 'clock-nine';
  }
}

function NodeBadges({ node }: { node: BrowseNode }) {
  const styles = useStyles2(getStyles);
  if (node.type !== 'measure') {
    return node.type === 'metric' && node.fromTemplate ? (
      <span className={styles.count} title="Created from a template">
        from template
      </span>
    ) : null;
  }
  return (
    <>
      <span className={styles.count} title="Measure ID">
        #{node.id}
      </span>
      {node.uom && <span className={styles.count}>{node.uom}</span>}
      {node.interval && <span className={styles.badge}>{node.interval === 'timestamp' ? 'tag' : node.interval}</span>}
    </>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  modal: css({ width: '820px', maxWidth: '95vw' }),
  hint: css({ marginTop: theme.spacing(1), marginBottom: 0 }),
  search: css({ marginTop: theme.spacing(1) }),
  list: css({
    marginTop: theme.spacing(1),
    height: '55vh',
    overflowY: 'auto',
    border: `1px solid ${theme.colors.border.weak}`,
    borderRadius: theme.shape.radius.default,
    padding: theme.spacing(0.5, 0),
  }),
  row: css({
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(1),
    minHeight: theme.spacing(4),
    paddingRight: theme.spacing(1),
    '&:hover': { background: theme.colors.action.hover },
  }),
  chevronSpace: css({ width: theme.spacing(2), flexShrink: 0 }),
  checkboxSpace: css({ width: theme.spacing(2), flexShrink: 0 }),
  icon: css({ color: theme.colors.text.secondary, flexShrink: 0 }),
  eventMark: css({ marginLeft: theme.spacing(0.75), color: theme.colors.warning.text, verticalAlign: 'text-bottom' }),
  label: css({
    cursor: 'pointer',
    flexGrow: 1,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  }),
  description: css({
    marginLeft: theme.spacing(1.5),
    color: theme.colors.text.secondary,
    fontSize: theme.typography.bodySmall.fontSize,
  }),
  count: css({ color: theme.colors.text.secondary, fontSize: theme.typography.bodySmall.fontSize, flexShrink: 0 }),
  badge: css({
    flexShrink: 0,
    fontSize: theme.typography.bodySmall.fontSize,
    padding: theme.spacing(0, 0.75),
    borderRadius: theme.shape.radius.pill,
    border: `1px solid ${theme.colors.border.medium}`,
    color: theme.colors.text.secondary,
  }),
  note: css({ padding: theme.spacing(1, 2), color: theme.colors.text.secondary }),
  error: css({ padding: theme.spacing(1, 2), color: theme.colors.error.text }),
});
