import React from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { Icon, useStyles2 } from '@grafana/ui';

import { hasIntervalIcon, IntervalIcon } from './IntervalIcon';

/** Flow Config's blue for retrieval marks. */
const FLOW_BLUE = '#0AB6FF';

const RETRIEVAL_LABELS: Record<string, string> = {
  aggregated: 'Aggregated',
  calculated: 'Calculated',
  manual: 'Manual',
  retrieved: 'Retrieved',
  replicated: 'Replicated',
};

/**
 * Flow's retrieval marks, traced from Flow Config's overlay icons onto a
 * 7x8 grid: a triangle for aggregated, an equals sign for calculated, a
 * plus above a tray for manual, a cylinder for retrieved and a boxed cross
 * for replicated.
 */
function MarkShape({ retrieval, halo }: { retrieval: string; halo: string }) {
  switch (retrieval) {
    case 'aggregated':
      return (
        <polygon
          points="3.5,0.8 6.6,7.2 0.4,7.2"
          fill="none"
          stroke={FLOW_BLUE}
          strokeWidth={1.3}
          strokeLinejoin="round"
        />
      );
    case 'calculated':
      return (
        <g fill={FLOW_BLUE}>
          <rect x={0.5} y={2} width={6} height={1.4} />
          <rect x={0.5} y={4.6} width={6} height={1.4} />
        </g>
      );
    case 'manual':
      return (
        <g fill={FLOW_BLUE}>
          <rect x={2.8} y={0} width={1.4} height={3.6} />
          <rect x={1.7} y={1.1} width={3.6} height={1.4} />
          <path d="M0.6 4.4 h1.3 v1.7 h3.2 v-1.7 h1.3 v3.4 h-5.8 z" />
        </g>
      );
    case 'retrieved':
      return (
        <g>
          <path d="M0.6 1.6 a2.9 1.2 0 0 1 5.8 0 v4.8 a2.9 1.2 0 0 1 -5.8 0 z" fill={FLOW_BLUE} />
          <path d="M0.6 1.6 a2.9 1.2 0 0 0 5.8 0" fill="none" stroke={halo} strokeWidth={0.6} />
        </g>
      );
    case 'replicated':
      return (
        <g>
          <rect x={0} y={0.5} width={7} height={7} rx={1.2} fill={FLOW_BLUE} />
          <rect x={3} y={1.6} width={1} height={4.8} fill={halo} />
          <rect x={1.1} y={3.5} width={4.8} height={1} fill={halo} />
        </g>
      );
    default:
      return null;
  }
}

/** Flow's mark for a retrieval type on its own, e.g. for a legend. */
export function RetrievalMark({ retrieval, size = 10 }: { retrieval?: string; size?: number }) {
  const styles = useStyles2(getStyles);
  const key = retrieval?.toLowerCase() ?? '';
  if (!RETRIEVAL_LABELS[key]) {
    return null;
  }
  return (
    <svg width={size} height={size} viewBox="-0.5 -0.5 8 9" role="img" aria-label={RETRIEVAL_LABELS[key]}>
      <title>{RETRIEVAL_LABELS[key]}</title>
      <MarkShape retrieval={key} halo={styles.haloColor} />
    </svg>
  );
}

/**
 * A measure's icon as Flow Config draws it: the interval symbol (or the
 * tag icon) with the retrieval mark in its bottom-left corner.
 */
export function MeasureIcon({
  interval,
  retrieval,
  size = 16,
  className,
}: {
  interval?: string;
  retrieval?: string;
  size?: number;
  className?: string;
}) {
  const styles = useStyles2(getStyles);
  const key = retrieval?.toLowerCase() ?? '';
  const label = RETRIEVAL_LABELS[key];
  const base = hasIntervalIcon(interval) ? (
    <IntervalIcon interval={interval!} size={size} />
  ) : (
    <Icon name={interval === 'timestamp' ? 'chart-line' : 'clock-nine'} size={size <= 14 ? 'sm' : 'md'} />
  );
  return (
    <span className={`${styles.wrap} ${className ?? ''}`} title={label ? `${label} measure` : undefined}>
      {base}
      {label && (
        <svg className={styles.mark} width={size * 0.6} height={size * 0.68} viewBox="-1 -1 9 10" aria-hidden="true">
          {/* A halo in the background colour keeps the mark readable over the interval symbol. */}
          <rect x={-1} y={-1} width={9} height={10} rx={2} fill={styles.haloColor} />
          <MarkShape retrieval={key} halo={styles.haloColor} />
        </svg>
      )}
    </span>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  wrap: css({ position: 'relative', display: 'inline-flex', flexShrink: 0, lineHeight: 0 }),
  mark: css({ position: 'absolute', left: '-3px', bottom: '-3px' }),
  haloColor: theme.colors.background.primary,
});
