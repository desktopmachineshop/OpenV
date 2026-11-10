// The Gantt's time scale: days as numbers, a zoom as pixels per day, and the
// two rows of ticks the chart's header draws. Dates are the plan's
// YYYY-MM-DD strings, read as UTC days so a time zone never moves a bar.

export type Zoom = 'days' | 'weeks' | 'months' | 'quarters';

export const ZOOMS: { value: Zoom; label: string }[] = [
  { value: 'days', label: 'Days' },
  { value: 'weeks', label: 'Weeks' },
  { value: 'months', label: 'Months' },
  { value: 'quarters', label: 'Quarters' },
];

/** Pixels one day takes at each zoom. */
export const DAY_WIDTH: Record<Zoom, number> = { days: 30, weeks: 12, months: 4.5, quarters: 1.6 };

const DAY_MS = 86_400_000;
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

/** A YYYY-MM-DD day as a count of days since the epoch. */
export const toDay = (date: string): number => Math.round(Date.parse(`${date}T00:00:00Z`) / DAY_MS);

/** A day count back to YYYY-MM-DD. */
export const fromDay = (day: number): string => new Date(day * DAY_MS).toISOString().slice(0, 10);

export const addDays = (date: string, days: number): string => fromDay(toDay(date) + days);

/** "3 Mar", with the year when it is not this plan's. */
export const shortDate = (date: string | null, today?: string): string => {
  if (!date) return '';
  const d = new Date(`${date}T00:00:00Z`);
  const label = `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
  return today && today.slice(0, 4) === date.slice(0, 4) ? label : `${label} ${d.getUTCFullYear()}`;
};

/** "1 Mar – 20 Mar", or one date for a single day. */
export const spanLabel = (start: string | null, end: string | null, today?: string): string => {
  if (!start || !end) return 'Not scheduled';
  return start === end ? shortDate(start, today) : `${shortDate(start, today)} – ${shortDate(end, today)}`;
};

export interface Scale {
  zoom: Zoom;
  /** The first and last day drawn. */
  first: number;
  last: number;
  dayWidth: number;
  width: number;
  /** The left edge of a day, in pixels. */
  x: (date: string) => number;
  /** The day at a pixel offset. */
  dayAt: (px: number) => string;
}

/** How much empty time the chart keeps either side of the plan, in days. */
const PADDING: Record<Zoom, number> = { days: 5, weeks: 14, months: 45, quarters: 120 };

/**
 * The scale that shows a plan spanning start to end (either may be missing:
 * an empty plan is drawn around today), wide enough to fill minWidth.
 */
export const buildScale = (zoom: Zoom, start: string | null, end: string | null, today: string, minWidth = 0): Scale => {
  const dayWidth = DAY_WIDTH[zoom];
  let first = Math.min(toDay(start || today), toDay(today)) - PADDING[zoom];
  let last = Math.max(toDay(end || today), toDay(today)) + PADDING[zoom];
  const short = Math.ceil(minWidth / dayWidth) - (last - first + 1);
  if (short > 0) last += short;
  // Start the chart on a Monday, so week ticks and weekends line up.
  first -= (new Date(first * DAY_MS).getUTCDay() + 6) % 7;
  return {
    zoom,
    first,
    last,
    dayWidth,
    width: (last - first + 1) * dayWidth,
    x: (date) => (toDay(date) - first) * dayWidth,
    dayAt: (px) => fromDay(first + Math.floor(px / dayWidth)),
  };
};

export interface Tick {
  key: string;
  label: string;
  left: number;
  width: number;
  /** A weekend day, shaded at the days zoom. */
  weekend?: boolean;
}

const clip = (scale: Scale, from: number, to: number): { left: number; width: number } | null => {
  const a = Math.max(from, scale.first);
  const b = Math.min(to, scale.last + 1);
  return b > a ? { left: (a - scale.first) * scale.dayWidth, width: (b - a) * scale.dayWidth } : null;
};

const monthStart = (year: number, month: number) => Math.round(Date.UTC(year, month, 1) / DAY_MS);

/** Calls back once per period of `months` months that overlaps the scale. */
const eachPeriod = (scale: Scale, months: number, fn: (year: number, month: number, from: number, to: number) => void) => {
  const d = new Date(scale.first * DAY_MS);
  let year = d.getUTCFullYear();
  let month = Math.floor(d.getUTCMonth() / months) * months;
  for (;;) {
    const from = monthStart(year, month);
    if (from > scale.last) return;
    fn(year, month, from, monthStart(year, month + months));
    month += months;
    if (month >= 12) {
      month -= 12;
      year += 1;
    }
  }
};

/**
 * The header's two rows. Upper: months (days, weeks), quarters or years.
 * Lower: days, weeks by their Monday, months, or quarters.
 */
export const buildTicks = (scale: Scale): { upper: Tick[]; lower: Tick[] } => {
  const upper: Tick[] = [];
  const lower: Tick[] = [];
  const push = (list: Tick[], key: string, label: string, from: number, to: number, weekend?: boolean) => {
    const box = clip(scale, from, to);
    if (box) list.push({ key, label, ...box, ...(weekend ? { weekend } : {}) });
  };
  if (scale.zoom === 'days' || scale.zoom === 'weeks') {
    eachPeriod(scale, 1, (y, m, from, to) => push(upper, `${y}-${m}`, `${MONTHS[m]} ${y}`, from, to));
    if (scale.zoom === 'days') {
      for (let day = scale.first; day <= scale.last; day++) {
        const d = new Date(day * DAY_MS);
        const dow = d.getUTCDay();
        push(lower, String(day), String(d.getUTCDate()), day, day + 1, dow === 0 || dow === 6);
      }
    } else {
      for (let day = scale.first; day <= scale.last; day += 7) {
        push(lower, String(day), String(new Date(day * DAY_MS).getUTCDate()), day, day + 7);
      }
    }
  } else if (scale.zoom === 'months') {
    eachPeriod(scale, 3, (y, m, from, to) => push(upper, `${y}-q${m}`, `Q${m / 3 + 1} ${y}`, from, to));
    eachPeriod(scale, 1, (y, m, from, to) => push(lower, `${y}-${m}`, MONTHS[m], from, to));
  } else {
    eachPeriod(scale, 12, (y, _m, from, to) => push(upper, String(y), String(y), from, to));
    eachPeriod(scale, 3, (y, m, from, to) => push(lower, `${y}-q${m}`, `Q${m / 3 + 1}`, from, to));
  }
  return { upper, lower };
};

/**
 * The zoom a plan opens at: the closest one at which the whole plan, from
 * its first day to its last, is no wider than the chart and a quarter more,
 * so the reader sees all of it with at most a short scroll.
 */
export const fitZoom = (start: string | null, end: string | null, width: number): Zoom => {
  if (!start || !end || width <= 0) return 'weeks';
  const days = toDay(end) - toDay(start) + 1;
  for (const z of ZOOMS) {
    if (days * DAY_WIDTH[z.value] <= width * 1.25) return z.value;
  }
  return 'quarters';
};
