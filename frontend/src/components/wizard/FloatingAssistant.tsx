import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { useAppStore } from '../../state/store';
import { useFeature } from '../../hooks/useFeature';
import { useViewport } from '../../hooks/useViewport';
import { useVisualViewport } from '../../hooks/useVisualViewport';
import { lastProject } from '../../appShortcuts';
import { FloatingAssistantChat } from './FloatingAssistantChat';
import { assistantRoute } from './floatingAssistantRoute';
import { FLOATING_ASSISTANT_FEATURE, useAssistantStore } from './assistantStore';
import { isAssistantShortcut, loadPanelLayout, savePanelLayout, useResizeFromCorner } from './floatingAssistantLayout';

const iconButton: React.CSSProperties = {
  width: 32,
  height: 32,
  border: 'none',
  background: 'none',
  borderRadius: 6,
  color: 'var(--text-muted)',
  cursor: 'pointer',
  fontSize: 15,
  flexShrink: 0,
};

/**
 * The V&V Assistant as a floating chat (#726): a button in the bottom
 * corner of every project page and the workspace Home, opening a panel
 * docked over the page that can be resized, expanded, moved to the other
 * corner and minimised back to the button. It is mounted above the routes,
 * so the conversation and its stream live on while the reader moves
 * between pages; minimised, the panel stays mounted but hidden (from screen
 * readers too). On a phone it opens as a bottom sheet.
 *
 * Alt+A (Option+A on a Mac) opens it and puts the cursor in the message
 * box; Esc minimises it and gives the focus back to the button.
 */
export const FloatingAssistant: React.FC = () => {
  const enabled = useFeature(FLOATING_ASSISTANT_FEATURE);
  const location = useLocation();
  const route = useMemo(() => assistantRoute(location.pathname), [location.pathname]);
  const viewport = useViewport();
  const visual = useVisualViewport();
  const projects = useAppStore((s) => s.projects);
  const { panel, setPanel, request, takeRequest, dismissContext, override, page, dismissed } = useAssistantStore();
  const [layout, setLayout] = useState(loadPanelLayout);
  const [homeProject, setHomeProject] = useState('');
  const panelRef = useRef<HTMLDivElement>(null);
  const launcherRef = useRef<HTMLButtonElement>(null);
  const startResize = useResizeFromCorner(layout, (next) => {
    setLayout(next);
    savePanelLayout(next);
  });

  // On the workspace Home there is no project in the URL: the conversation
  // is the one of the project chosen in the panel, the last one opened first.
  const homeChoice =
    homeProject && projects.some((p) => p.id === homeProject)
      ? homeProject
      : projects.find((p) => p.id === lastProject())?.id || projects[0]?.id || '';
  const projectId = route.projectId || homeChoice;
  const open = panel !== 'closed';
  const phone = viewport.isPhone;
  const shown = enabled && route.show;
  // What the reader is looking at: what a caller passed, else what the page
  // registered, else the page's name; nothing once the reader removed it.
  const offered = override || page.context || (route.pageLabel ? { label: route.pageLabel } : null);
  const context = offered && offered.label !== dismissed ? offered : null;

  // The keyboard way in, from anywhere on a page that shows the assistant.
  useEffect(() => {
    if (!shown) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (!isAssistantShortcut(e)) return;
      e.preventDefault();
      if (panel === 'closed') setPanel('open');
      else panelRef.current?.querySelector<HTMLTextAreaElement>('textarea')?.focus();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [shown, panel, setPanel]);

  // Opening moves the focus into the panel, to the message box once the
  // conversation has drawn it.
  useEffect(() => {
    if (!open) return undefined;
    const t = window.setTimeout(() => {
      const box = panelRef.current?.querySelector<HTMLTextAreaElement>('textarea');
      (box || panelRef.current)?.focus();
    }, 50);
    return () => window.clearTimeout(t);
  }, [open]);

  if (!shown) return null;

  const minimise = () => {
    setPanel('closed');
    window.setTimeout(() => launcherRef.current?.focus(), 0);
  };
  const left = layout.side === 'left';
  const expanded = panel === 'expanded';
  const bottomOffset = 'calc(16px + var(--ov-tabbar-height, 0px) + env(safe-area-inset-bottom, 0px))';

  const panelStyle: React.CSSProperties = phone
    ? {
        left: 0,
        right: 0,
        bottom: visual.keyboardInset,
        height: Math.round(visual.height * (visual.keyboardInset > 0 ? 0.95 : 0.75)),
        borderRadius: '12px 12px 0 0',
      }
    : {
        [left ? 'left' : 'right']: 16,
        bottom: 16,
        width: expanded ? 'min(760px, calc(100vw - 32px))' : layout.width,
        height: expanded ? 'calc(100vh - 32px)' : layout.height,
        maxWidth: 'calc(100vw - 32px)',
        maxHeight: 'calc(100vh - 32px)',
        borderRadius: 10,
      };

  return (
    <>
      {phone && open && (
        <div onClick={minimise} style={{ position: 'fixed', inset: 0, background: 'var(--overlay)', zIndex: 949 }} />
      )}
      <div
        ref={panelRef}
        role="dialog"
        aria-label="V&V Assistant"
        aria-modal={phone && open ? true : undefined}
        hidden={!open}
        tabIndex={-1}
        data-help-pane="guided.assistant"
        data-testid="floating-assistant"
        onKeyDown={(e) => {
          if (e.key === 'Escape' && !e.defaultPrevented) {
            e.stopPropagation();
            minimise();
          }
        }}
        className={phone ? 'safe-area-bottom' : undefined}
        style={{
          position: 'fixed',
          display: open ? 'flex' : 'none',
          flexDirection: 'column',
          background: 'var(--surface)',
          border: '1px solid var(--border)',
          boxShadow: '0 8px 28px rgba(0,0,0,0.25)',
          zIndex: 950,
          overflow: 'hidden',
          ...panelStyle,
        }}
      >
        {!phone && !expanded && (
          <div
            role="separator"
            aria-label="Resize the assistant"
            title="Drag to resize"
            onMouseDown={startResize}
            style={{
              position: 'absolute',
              top: 0,
              [left ? 'right' : 'left']: 0,
              width: 14,
              height: 14,
              cursor: left ? 'nesw-resize' : 'nwse-resize',
              zIndex: 1,
            }}
          />
        )}
        <div style={{ display: 'flex', alignItems: 'center', gap: 4, padding: '6px 6px 6px 14px', borderBottom: '1px solid var(--border)' }}>
          <div style={{ flex: 1, minWidth: 0, fontWeight: 700, fontSize: 14, color: 'var(--text)' }}>
            <span aria-hidden="true" style={{ color: 'var(--purple-strong)' }}>✦ </span>V&amp;V Assistant
          </div>
          {!phone && (
            <>
              <button
                type="button"
                style={iconButton}
                aria-label={left ? 'Move to the right' : 'Move to the left'}
                title={left ? 'Move to the right' : 'Move to the left'}
                onClick={() => {
                  const next = { ...layout, side: left ? ('right' as const) : ('left' as const) };
                  setLayout(next);
                  savePanelLayout(next);
                }}
              >
                ⇆
              </button>
              <button
                type="button"
                style={iconButton}
                aria-label={expanded ? 'Restore size' : 'Expand'}
                title={expanded ? 'Restore size' : 'Expand'}
                onClick={() => setPanel(expanded ? 'open' : 'expanded')}
              >
                {expanded ? '⤡' : '⤢'}
              </button>
            </>
          )}
          <button type="button" style={iconButton} aria-label="Minimise" title="Minimise (Esc)" onClick={minimise}>
            —
          </button>
        </div>
        {!route.projectId && projects.length > 0 && (
          <label style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 14px', fontSize: 12, color: 'var(--text-muted)', borderBottom: '1px solid var(--border-soft)' }}>
            Project
            <select
              value={homeChoice}
              onChange={(e) => setHomeProject(e.target.value)}
              style={{ flex: 1, minWidth: 0, fontSize: 12 }}
            >
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
        )}
        {context && <ContextLine label={context.label} onRemove={() => dismissContext(context.label)} />}
        <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
          {projectId ? (
            <FloatingAssistantChat
              projectId={projectId}
              context={context}
              request={request}
              onRequestSent={takeRequest}
            />
          ) : (
            <div style={{ padding: 12, fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.5 }}>
              The V&amp;V Assistant works inside a project. Create or open a project to chat with it.
            </div>
          )}
        </div>
      </div>
      {!open && (
        <button
          ref={launcherRef}
          type="button"
          onClick={() => setPanel('open')}
          aria-label="Open the V&V Assistant"
          title="Open the V&V Assistant (Alt+A)"
          aria-keyshortcuts="Alt+A"
          data-help-key="guided.assistant"
          style={{
            position: 'fixed',
            [left ? 'left' : 'right']: 16,
            bottom: bottomOffset,
            minHeight: 44,
            padding: '0 16px',
            borderRadius: 22,
            border: 'none',
            background: 'var(--accent)',
            color: 'var(--accent-fg)',
            fontSize: 14,
            fontWeight: 600,
            boxShadow: '0 4px 12px rgba(0,0,0,0.25)',
            cursor: 'pointer',
            zIndex: 940,
          }}
        >
          <span aria-hidden="true">✦</span> Assistant
        </button>
      )}
    </>
  );
};

/** "Looking at: REQ-73 Change impact analysis", with ✕ to stop sending it. */
const ContextLine: React.FC<{ label: string; onRemove: () => void }> = ({ label, onRemove }) => {
  return (
    <div
      data-testid="assistant-context"
      style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '4px 6px 4px 14px', fontSize: 12, color: 'var(--text-muted)', borderBottom: '1px solid var(--border-soft)' }}
    >
      <span style={{ flex: 1, minWidth: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
        Looking at: <b style={{ color: 'var(--text)', fontWeight: 600 }}>{label}</b>
      </span>
      <button
        type="button"
        onClick={onRemove}
        aria-label={`Stop sending ${label} as context`}
        title="Don’t send this as context"
        style={{ ...iconButton, width: 28, height: 28, fontSize: 12 }}
      >
        ✕
      </button>
    </div>
  );
};
