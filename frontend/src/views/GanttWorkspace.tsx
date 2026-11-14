// The workspace's Plan page: every project's plan on one chart. Each
// project the reader can see has a line with its top-level tasks and its
// milestones, on a time scale they all share, so the projects can be read
// against each other; the links between their tasks are listed beneath.
// It is the chart a project's Gantt shows as "All projects", on a page of
// its own that belongs to no project.
import React, { useState } from 'react';
import { Navigate } from 'react-router-dom';
import { useAppStore } from '../state/store';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { GANTT_PLAN_FEATURE, WORKSPACE_PLAN_FEATURE } from '../features';
import { Navbar } from '../components/Navbar';
import { PanelHeader, SegmentedControl, buttonStyle, panelStyle } from '../components/ui';
import { PlanMaster } from '../components/plan/PlanLines';
import { PlanWorkspaceViews, WORKSPACE_VIEWS, type WorkspaceView } from '../components/plan/PlanWorkspaceViews';
import { ZOOMS, type Zoom } from '../components/plan/ganttScale';

/**
 * Gated on workspace-plan and on the Gantt plan itself: it waits for the
 * gates, and without either sends a member to the projects list, as the
 * workspace's Runs page does.
 */
export const GanttWorkspace: React.FC = () => {
  const gatesSettled = useAppStore((s) => s.features !== null || s.featuresFailed);
  const noWorkspace = useAppStore((s) => s.orgsLoaded && !s.activeOrgId);
  const activeOrgId = useAppStore((s) => s.activeOrgId);
  const plan = useFeature(GANTT_PLAN_FEATURE);
  const page = useFeature(WORKSPACE_PLAN_FEATURE);
  const on = plan && page;
  const [zoom, setZoom] = useState<Zoom>('months');
  const [showCritical, setShowCritical] = useState(true);
  const [todaySignal, setTodaySignal] = useState(0);
  // The line per project, or one of the views a project's Gantt has, over every project.
  const [view, setView] = useState<WorkspaceView>('lines');
  const { isPhone } = useViewport();
  if ((gatesSettled && !on) || noWorkspace) return <Navigate to="/projects" replace />;
  return (
    <div className="app-shell" style={{ background: 'var(--bg-app)', display: 'flex', flexDirection: 'column' }}>
      <Navbar title="Workspace plan" showWorkspaceControls />
      {on ? (
        <div className="ov-gutter" style={{ padding: 16, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          <h2 className="sr-only">Workspace plan</h2>
          <section aria-label="Plan of every project" data-help-key="gantt.master" style={{ ...panelStyle, flex: 1, minHeight: 320, minWidth: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
            <PanelHeader
              title="All projects"
              action={
                <span style={{ display: 'inline-flex', gap: 8, alignItems: 'center', flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                  <select aria-label="View" data-help-key="gantt.views" value={view} onChange={(e) => setView(e.target.value as WorkspaceView)} style={{ width: 'auto', height: isPhone ? 40 : 28, padding: '0 8px', fontSize: isPhone ? 16 : 12 }}>
                    {WORKSPACE_VIEWS.map((v) => (
                      <option key={v.value} value={v.value}>
                        {v.label}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    aria-pressed={showCritical}
                    onClick={() => setShowCritical((v) => !v)}
                    title="Mark each project's critical path, which is the top row of its line"
                    style={{
                      ...buttonStyle('secondary', { compact: !isPhone }),
                      ...(showCritical ? { background: 'var(--tint-yellow)', borderColor: 'var(--tint-yellow-border)', color: 'var(--warning-text)' } : {}),
                    }}
                  >
                    Critical path
                  </button>
                  <button type="button" onClick={() => setTodaySignal((n) => n + 1)} style={buttonStyle('secondary', { compact: !isPhone })}>
                    Today
                  </button>
                  <SegmentedControl aria-label="Zoom" options={ZOOMS} value={zoom} onChange={setZoom} style={{ fontSize: 12 }} />
                </span>
              }
            />
            {/* A workspace switch loads the other workspace's plans. */}
            {view === 'lines' ? (
              <PlanMaster key={activeOrgId || ''} currentProjectId="" zoom={zoom} showCritical={showCritical} todaySignal={todaySignal} />
            ) : (
              <PlanWorkspaceViews key={activeOrgId || ''} view={view} zoom={zoom} showCritical={showCritical} todaySignal={todaySignal} />
            )}
          </section>
        </div>
      ) : (
        <p style={{ padding: 20, textAlign: 'center', color: 'var(--text-muted)' }}>Loading…</p>
      )}
    </div>
  );
};
