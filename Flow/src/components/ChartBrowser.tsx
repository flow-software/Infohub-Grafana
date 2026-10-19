import React, { useCallback, useEffect, useRef, useState } from 'react';
import { css, cx } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { Button, Icon, IconButton, Modal, Spinner, useStyles2 } from '@grafana/ui';

import { DataSource } from '../datasource';
import { ChartNode } from '../types';

interface Props {
  datasource: DataSource;
  /** The query's current chart. */
  selectedId?: number;
  onApply: (chart: { id: number; label: string }) => void;
  onDismiss: () => void;
}

type Children = { state: 'error'; message: string } | { state: 'done'; nodes: ChartNode[] };

const ROOT = 0;

function errorText(err: unknown): string {
  const e = err as { data?: { message?: string }; message?: string };
  return e?.data?.message ?? e?.message ?? 'Request failed';
}

/**
 * Browses Flow's chart folders, loading each level on demand, and picks one
 * chart. Charts the plugin cannot show yet (event-based, widgets, ...) are
 * listed but cannot be picked.
 */
export function ChartBrowser({ datasource, selectedId, onApply, onDismiss }: Props) {
  const styles = useStyles2(getStyles);
  const [children, setChildren] = useState<Record<number, Children>>({});
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [picked, setPicked] = useState<{ id: number; label: string } | undefined>();
  // Folders already requested; one without an entry in children shows as loading.
  const requested = useRef(new Set<number>());

  const load = useCallback(
    (folderId: number) => {
      requested.current.add(folderId);
      datasource
        .browseCharts(folderId || undefined)
        .then((nodes) => setChildren((prev) => ({ ...prev, [folderId]: { state: 'done', nodes } })))
        .catch((err) => setChildren((prev) => ({ ...prev, [folderId]: { state: 'error', message: errorText(err) } })));
    },
    [datasource]
  );

  useEffect(() => {
    if (!requested.current.has(ROOT)) {
      load(ROOT);
    }
  }, [load]);

  const toggle = (id: number) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
    if (!requested.current.has(id)) {
      load(id);
    }
  };

  const currentId = picked?.id ?? selectedId;

  const renderLevel = (folderId: number, depth: number, path: string): React.ReactNode => {
    const state = children[folderId];
    const pad = { paddingLeft: depth * 20 + 8 };
    if (!state) {
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
          {folderId === ROOT ? 'No chart folders. The API token may not have access to any.' : 'Empty'}
        </div>
      );
    }
    return state.nodes.map((n) => {
      const label = path ? `${path}/${n.name}` : n.name;
      if (n.type === 'folder') {
        const isOpen = expanded.has(n.id);
        return (
          <React.Fragment key={`f${n.id}`}>
            <div
              role="treeitem"
              aria-level={depth + 1}
              aria-expanded={isOpen}
              className={styles.row}
              style={{ paddingLeft: depth * 20 }}
            >
              <IconButton
                name={isOpen ? 'angle-down' : 'angle-right'}
                aria-label={isOpen ? `Collapse ${n.name}` : `Expand ${n.name}`}
                onClick={() => toggle(n.id)}
              />
              <Icon name={isOpen ? 'folder-open' : 'folder'} className={styles.icon} />
              <span className={styles.label} onClick={() => toggle(n.id)}>
                {n.name}
              </span>
            </div>
            {isOpen && renderLevel(n.id, depth + 1, label)}
          </React.Fragment>
        );
      }
      const isPicked = currentId === n.id;
      return (
        <div
          key={`c${n.id}`}
          role="treeitem"
          aria-level={depth + 1}
          aria-selected={isPicked}
          aria-disabled={!n.supported}
          className={cx(styles.row, isPicked && styles.picked, !n.supported && styles.unsupported)}
          style={{ paddingLeft: depth * 20 + 24 }}
          title={n.supported ? undefined : `${n.chartType} charts cannot be shown as a table yet`}
          onClick={() => n.supported && setPicked({ id: n.id, label })}
          onDoubleClick={() => n.supported && onApply({ id: n.id, label })}
        >
          <Icon name="table" className={styles.icon} />
          <span className={styles.label}>{n.name}</span>
          <span className={styles.type}>{n.chartType}</span>
        </div>
      );
    });
  };

  return (
    <Modal title="Choose an Infohub chart" isOpen onDismiss={onDismiss} className={styles.modal}>
      <div className={styles.list} role="tree" aria-label="Flow charts">
        {renderLevel(ROOT, 0, '')}
      </div>
      <Modal.ButtonRow leftItems={<span className={styles.type}>Time-based charts can be shown as tables.</span>}>
        <Button variant="secondary" fill="outline" onClick={onDismiss}>
          Cancel
        </Button>
        <Button disabled={!picked} onClick={() => picked && onApply(picked)}>
          Use chart
        </Button>
      </Modal.ButtonRow>
    </Modal>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  modal: css({ width: '720px', maxWidth: '95vw' }),
  list: css({
    height: '50vh',
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
    cursor: 'pointer',
    '&:hover': { background: theme.colors.action.hover },
  }),
  picked: css({ background: theme.colors.action.selected, '&:hover': { background: theme.colors.action.selected } }),
  unsupported: css({ opacity: 0.5, cursor: 'not-allowed' }),
  icon: css({ color: theme.colors.text.secondary, flexShrink: 0 }),
  label: css({ flexGrow: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }),
  type: css({ color: theme.colors.text.secondary, fontSize: theme.typography.bodySmall.fontSize, flexShrink: 0 }),
  note: css({ padding: theme.spacing(1, 2), color: theme.colors.text.secondary }),
  error: css({ padding: theme.spacing(1, 2), color: theme.colors.error.text }),
});
