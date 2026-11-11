// The plan as a network diagram: a box for each task that is linked to
// another or has no sub-tasks, in a column one to the right of the tasks
// that drive it, with an arrow for each relationship.
import React, { useMemo } from 'react';
import type { PlanTask, ProjectPlan } from '../../api/client';
import { ellipsis, mutedStyle } from '../ui';
import { spanLabel } from './ganttScale';

const BOX = { w: 190, h: 62, gapX: 60, gapY: 18 };

/** Each task's column: one past the furthest of the tasks that drive it. */
const ranks = (tasks: PlanTask[]): Map<string, number> => {
  const byId = new Map(tasks.map((t) => [t.id, t]));
  const rank = new Map<string, number>();
  const visit = (t: PlanTask, seen: Set<string>): number => {
    const known = rank.get(t.id);
    if (known !== undefined) return known;
    if (seen.has(t.id)) return 0;
    seen.add(t.id);
    let r = 0;
    for (const rel of t.relationships) {
      const on = byId.get(rel.task_id);
      if (on) r = Math.max(r, visit(on, seen) + 1);
    }
    rank.set(t.id, r);
    return r;
  };
  for (const t of tasks) visit(t, new Set());
  return rank;
};

export const PlanNetwork: React.FC<{ plan: ProjectPlan; selectedTaskId: string; onSelectTask: (t: PlanTask) => void }> = ({ plan, selectedTaskId, onSelectTask }) => {
  const layout = useMemo(() => {
    const nodes = plan.tasks.filter((t) => t.subtasks_total === 0 || t.relationships.length > 0 || t.drives.length > 0);
    const rank = ranks(nodes);
    const filled = new Map<number, number>();
    const at = new Map<string, { x: number; y: number }>();
    for (const t of nodes) {
      const col = rank.get(t.id) || 0;
      const row = filled.get(col) || 0;
      filled.set(col, row + 1);
      at.set(t.id, { x: 16 + col * (BOX.w + BOX.gapX), y: 16 + row * (BOX.h + BOX.gapY) });
    }
    const width = 32 + (Math.max(0, ...filled.keys()) + 1) * (BOX.w + BOX.gapX) - BOX.gapX;
    const height = 32 + Math.max(1, ...filled.values()) * (BOX.h + BOX.gapY) - BOX.gapY;
    return { nodes, at, width, height };
  }, [plan.tasks]);

  if (layout.nodes.length === 0) return <p style={{ ...mutedStyle, padding: 16, margin: 0 }}>No tasks yet.</p>;
  return (
    <div data-plan-view="network" data-help-key="gantt.views" style={{ flex: 1, minHeight: 180, overflow: 'auto' }}>
      <div style={{ position: 'relative', width: layout.width, height: layout.height }}>
        <svg aria-hidden width={layout.width} height={layout.height} style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}>
          <defs>
            {(['ok', 'conflict', 'critical'] as const).map((k) => (
              <marker key={k} id={`network-arrow-${k}`} viewBox="0 0 6 6" refX="5" refY="3" markerWidth="6" markerHeight="6" orient="auto">
                <path d="M0 0 L6 3 L0 6 z" fill={k === 'ok' ? 'var(--neutral)' : k === 'conflict' ? 'var(--danger)' : 'var(--warning)'} />
              </marker>
            ))}
          </defs>
          {layout.nodes.flatMap((t) =>
            t.relationships.map((rel) => {
              const from = layout.at.get(rel.task_id);
              const to = layout.at.get(t.id);
              if (!from || !to) return null;
              const x1 = from.x + BOX.w;
              const y1 = from.y + BOX.h / 2;
              const y2 = to.y + BOX.h / 2;
              const mid = (x1 + to.x) / 2;
              const on = plan.tasks.find((x) => x.id === rel.task_id);
              const tone = rel.conflict ? 'conflict' : t.critical && on?.critical ? 'critical' : 'ok';
              return (
                <g key={`${rel.task_id}>${t.id}`}>
                  <path
                    d={`M${x1} ${y1} C${mid} ${y1} ${mid} ${y2} ${to.x} ${y2}`}
                    fill="none"
                    stroke={tone === 'conflict' ? 'var(--danger)' : tone === 'critical' ? 'var(--warning)' : 'var(--neutral)'}
                    strokeWidth={tone === 'critical' ? 2 : 1.25}
                    strokeDasharray={rel.conflict ? '4 3' : undefined}
                    markerEnd={`url(#network-arrow-${tone})`}
                  />
                  {rel.type !== 'FS' && (
                    <text x={mid} y={(y1 + y2) / 2 - 3} textAnchor="middle" fontSize="10" fill="var(--text-muted)">
                      {rel.type}
                    </text>
                  )}
                </g>
              );
            }),
          )}
        </svg>
        {layout.nodes.map((t) => {
          const p = layout.at.get(t.id)!;
          const selected = t.id === selectedTaskId;
          return (
            <button
              key={t.id}
              type="button"
              data-network-node={t.id}
              onClick={() => onSelectTask(t)}
              title={t.title}
              style={{
                position: 'absolute',
                left: p.x,
                top: p.y,
                width: BOX.w,
                height: BOX.h,
                boxSizing: 'border-box',
                padding: '6px 8px',
                textAlign: 'left',
                font: 'inherit',
                cursor: 'pointer',
                borderRadius: 6,
                border: `${t.critical ? 2 : 1}px solid ${t.critical ? 'var(--warning)' : t.late ? 'var(--danger)' : 'var(--border)'}`,
                background: t.done ? 'var(--tint-green, var(--surface-alt))' : 'var(--surface)',
                boxShadow: selected ? '0 0 0 2px var(--tint-blue-border)' : undefined,
                color: 'var(--text)',
              }}
            >
              <div style={{ ...ellipsis, fontSize: 12, fontWeight: 600 }}>
                <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>{t.wbs}</span> {t.title}
              </div>
              <div style={{ ...ellipsis, fontSize: 11, color: 'var(--text-muted)' }}>{t.span_start ? spanLabel(t.span_start, t.span_end, plan.today) : 'Not scheduled'}</div>
              <div style={{ fontSize: 11, color: t.late ? 'var(--danger-strong)' : 'var(--text-muted)' }}>
                {t.progress}%{t.float_days !== null && !t.done ? ` · float ${t.float_days}d` : ''}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
};
