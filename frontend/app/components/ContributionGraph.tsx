"use client";

import { useState } from "react";

import type { Contribution } from "../lib/api";

// ContributionGraph renders a GitHub-style commit calendar: 53 weeks of daily
// squares, coloured by activity level. It's a pure server component — given the
// raw heatmap entries it aggregates by day and lays out the grid.

const WEEKS = 53;
const DAY_MS = 24 * 60 * 60 * 1000;
const WEEKDAY_LABELS = ["", "Mon", "", "Wed", "", "Fri", ""];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// dateKey returns a UTC YYYY-MM-DD key so entries land in the right day box
// regardless of the viewer's timezone.
function dateKey(d: Date): string {
  return d.toISOString().slice(0, 10);
}

// level buckets a day's count into 0–4 for colour intensity.
function level(count: number): number {
  if (count <= 0) return 0;
  if (count <= 2) return 1;
  if (count <= 5) return 2;
  if (count <= 9) return 3;
  return 4;
}

// tipLabel renders the hover text for a day cell: "3 contributions on Nov 9".
function tipLabel(count: number, date: Date): string {
  const day = `${MONTHS[date.getUTCMonth()]} ${date.getUTCDate()}`;
  return count === 0
    ? `No contributions on ${day}`
    : `${count} contribution${count === 1 ? "" : "s"} on ${day}`;
}

export function ContributionGraph({ data }: { data: Contribution[] }) {
  // One floating tooltip for the whole grid: which cell is hovered and where
  // the pointer is (viewport coords - the tip is position: fixed, so it never
  // gets clipped by the grid's horizontal scroll container).
  const [tip, setTip] = useState<{ x: number; y: number; text: string } | null>(null);
  const showTip = (cell: { count: number; date: Date }) => (e: React.MouseEvent) =>
    setTip({ x: e.clientX, y: e.clientY, text: tipLabel(cell.count, cell.date) });

  // Sum contributions per UTC day.
  const byDay = new Map<string, number>();
  for (const e of data) {
    const key = dateKey(new Date(e.timestamp * 1000));
    byDay.set(key, (byDay.get(key) ?? 0) + e.contributions);
  }

  // Build the grid ending in the week of "today", aligned so each column is a
  // Sun-Sat week. The anchor is the Sunday of today's week; the leftmost column
  // starts 52 weeks earlier. Subtracting a fixed 370-day span and then walking
  // back to Sunday (the old approach) ends the grid days before today, which
  // dropped all activity from the trailing partial week - the graph looked
  // empty right after committing.
  const today = new Date();
  const end = new Date(Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate()));
  const weekAnchor = new Date(end);
  weekAnchor.setUTCDate(weekAnchor.getUTCDate() - weekAnchor.getUTCDay());
  const start = new Date(weekAnchor.getTime() - (WEEKS - 1) * 7 * DAY_MS);

  const weeks: { key: string; count: number; date: Date }[][] = [];
  let total = 0;
  const cursor = new Date(start);
  for (let w = 0; w < WEEKS; w++) {
    const col: { key: string; count: number; date: Date }[] = [];
    for (let d = 0; d < 7; d++) {
      const key = dateKey(cursor);
      const count = cursor.getTime() <= end.getTime() ? byDay.get(key) ?? 0 : -1; // -1 = future, render blank
      if (count > 0) total += count;
      col.push({ key, count, date: new Date(cursor) });
      cursor.setUTCDate(cursor.getUTCDate() + 1);
    }
    weeks.push(col);
  }

  // Month labels: show a month name above the first column whose first day is
  // in a new month.
  const monthLabels = weeks.map((col, i) => {
    const first = col[0].date;
    const prev = i > 0 ? weeks[i - 1][0].date : null;
    if (!prev || first.getUTCMonth() !== prev.getUTCMonth()) {
      return MONTHS[first.getUTCMonth()];
    }
    return "";
  });

  return (
    <div className="contrib">
      <div className="contrib-head">
        <span className="contrib-total">{total} contributions in the last year</span>
        <div className="contrib-legend">
          <span>Less</span>
          {[0, 1, 2, 3, 4].map((l) => (
            <i key={l} className={`contrib-cell lvl-${l}`} />
          ))}
          <span>More</span>
        </div>
      </div>

      <div className="contrib-grid-wrap">
        <div className="contrib-weekdays">
          {WEEKDAY_LABELS.map((label, i) => (
            <span key={i}>{label}</span>
          ))}
        </div>
        <div className="contrib-body">
          <div className="contrib-months">
            {monthLabels.map((m, i) => (
              <span key={i} style={{ gridColumn: i + 1 }}>
                {m}
              </span>
            ))}
          </div>
          <div className="contrib-grid">
            {weeks.map((col, i) => (
              <div className="contrib-col" key={i}>
                {col.map((cell) =>
                  cell.count < 0 ? (
                    <i key={cell.key} className="contrib-cell lvl-empty" />
                  ) : (
                    <i
                      key={cell.key}
                      className={`contrib-cell lvl-${level(cell.count)}`}
                      role="img"
                      aria-label={tipLabel(cell.count, cell.date)}
                      onMouseEnter={showTip(cell)}
                      onMouseMove={showTip(cell)}
                      onMouseLeave={() => setTip(null)}
                    />
                  ),
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
      {tip && (
        <div className="contrib-tip" style={{ left: tip.x, top: tip.y - 32 }}>
          {tip.text}
        </div>
      )}
    </div>
  );
}
