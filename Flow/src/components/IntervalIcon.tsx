import React from 'react';

/**
 * Letters for Flow's period intervals. Periods up to a day are drawn in a
 * circle, like a clock; longer ones in a box with a header, like a calendar
 * page.
 */
const INTERVALS: Record<string, { letter: string; calendar: boolean; label: string }> = {
  minutely: { letter: 'M', calendar: false, label: 'Minutely' },
  hourly: { letter: 'H', calendar: false, label: 'Hourly' },
  shiftly: { letter: 'S', calendar: false, label: 'Shiftly' },
  daily: { letter: 'D', calendar: false, label: 'Daily' },
  weekly: { letter: 'W', calendar: true, label: 'Weekly' },
  monthly: { letter: 'M', calendar: true, label: 'Monthly' },
  quarterly: { letter: 'Q', calendar: true, label: 'Quarterly' },
  yearly: { letter: 'Y', calendar: true, label: 'Yearly' },
};

/** Whether IntervalIcon has a symbol for the interval. */
export function hasIntervalIcon(interval?: string): boolean {
  return !!interval && interval.toLowerCase() in INTERVALS;
}

/** A measure's period interval as a small symbol in the text colour, sized like Grafana's icons. */
export function IntervalIcon({
  interval,
  className,
  size = 16,
}: {
  interval: string;
  className?: string;
  size?: number;
}) {
  const spec = INTERVALS[interval.toLowerCase()];
  if (!spec) {
    return null;
  }
  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      role="img"
      aria-label={`${spec.label} measure`}
      style={{ flexShrink: 0 }}
    >
      <title>{`${spec.label} measure`}</title>
      {spec.calendar ? (
        <>
          <rect x={1.5} y={1.5} width={13} height={13} rx={1} fill="none" stroke="currentColor" strokeWidth={1.2} />
          <rect x={1.5} y={1.5} width={13} height={3} rx={1} fill="currentColor" />
        </>
      ) : (
        <circle cx={8} cy={8} r={6.8} fill="none" stroke="currentColor" strokeWidth={1.2} />
      )}
      <text
        x={8}
        y={spec.calendar ? 10 : 8}
        textAnchor="middle"
        dominantBaseline="central"
        fontSize={spec.calendar ? 7.5 : 8}
        fontWeight={600}
        fontFamily="inherit"
        fill="currentColor"
      >
        {spec.letter}
      </text>
    </svg>
  );
}
