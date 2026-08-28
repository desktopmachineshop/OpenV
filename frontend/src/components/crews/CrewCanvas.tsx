import React, { useEffect, useRef } from 'react';
import cytoscape, { Core } from 'cytoscape';
import { AgentDef, CrewGraph } from '../../api/client';
import { cssVar, useThemeVersion } from '../../theme';

export type CrewFilter = 'employees' | 'agents' | 'all';

export interface CrewCanvasProps {
  graph: CrewGraph;
  view: 'org' | 'network';
  filter: CrewFilter;
  agents: AgentDef[];
  members: { user_id: string; user_name: string }[];
  activeNodeIds: string[];
  connectMode: boolean;
  onSelectNode: (nodeId: string | null) => void;
  onSelectEdge: (edgeId: string | null) => void;
  onMoveNode: (nodeId: string, pos: { x: number; y: number }) => void;
  onConnect: (fromNodeId: string, toNodeId: string) => void;
}

// Claude-provider nodes keep the Anthropic brand terracotta in both themes;
// everything else uses the themed neutral (read at render time, see below).
const providerColor = (provider: string | undefined): string =>
  provider === 'claude-code' || provider === 'anthropic-api' || (provider || '').startsWith('claude')
    ? '#d97757'
    : cssVar('--neutral');

export const CrewCanvas: React.FC<CrewCanvasProps> = ({
  graph,
  view,
  filter,
  agents,
  members,
  activeNodeIds,
  connectMode,
  onSelectNode,
  onSelectEdge,
  onMoveNode,
  onConnect,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const cyRef = useRef<Core | null>(null);
  const pendingSourceRef = useRef<string | null>(null);

  // Keep latest callbacks / mode without rebuilding the graph.
  const callbacksRef = useRef({ onSelectNode, onSelectEdge, onMoveNode, onConnect, connectMode });
  callbacksRef.current = { onSelectNode, onSelectEdge, onMoveNode, onConnect, connectMode };

  // Cytoscape paints to a canvas, so CSS variables aren't resolved live: read
  // the current token values at build time and rebuild when the theme changes.
  const themeVersion = useThemeVersion();

  // Build / rebuild the cytoscape instance when the graph, view, filter, or
  // theme changes.
  useEffect(() => {
    if (!containerRef.current) return;

    const theme = {
      text: cssVar('--text'),
      textMuted: cssVar('--text-muted'),
      surface: cssVar('--surface'),
      surfaceAlt: cssVar('--surface-alt'),
      neutral: cssVar('--neutral'),
      accent: cssVar('--accent'),
      success: cssVar('--success'),
      warning: cssVar('--warning'),
      purple: cssVar('--purple'),
      tintBlue: cssVar('--tint-blue'),
      tintPurple: cssVar('--tint-purple'),
    };

    const agentById: Record<string, AgentDef> = {};
    agents.forEach((a) => {
      agentById[a.id] = a;
    });
    const memberName: Record<string, string> = {};
    members.forEach((m) => {
      memberName[m.user_id] = m.user_name;
    });

    // Employees | Agents | All filter — only matching nodes are drawn.
    const visibleNodes = graph.nodes.filter(
      (n) =>
        filter === 'all' ||
        (filter === 'agents' && n.node_type === 'agent') ||
        (filter === 'employees' && n.node_type === 'human')
    );
    const visibleIds = new Set(visibleNodes.map((n) => n.id));

    const edges = (
      view === 'org' ? graph.edges.filter((e) => e.edge_type === 'delegates-to') : graph.edges
    ).filter((e) => visibleIds.has(e.from_node_id) && visibleIds.has(e.to_node_id));

    const allHavePositions =
      visibleNodes.length > 0 &&
      visibleNodes.every(
        (n) =>
          n.position &&
          n.position[view] &&
          typeof n.position[view].x === 'number' &&
          typeof n.position[view].y === 'number'
      );

    // Org view groups nodes into labelled department containers
    // (cytoscape compound nodes) — computed over visible nodes only.
    const departments =
      view === 'org'
        ? Array.from(
            new Set(visibleNodes.map((n) => (n.department || '').trim()).filter(Boolean))
          )
        : [];

    const elements = [
      ...departments.map((dept) => ({
        group: 'nodes',
        data: { id: `dept:${dept}`, label: dept },
        classes: 'department',
        grabbable: false,
        selectable: false,
      })),
      ...visibleNodes.map((n) => {
        const fallbackName =
          n.node_type === 'human'
            ? n.user_name || (n.user_id ? memberName[n.user_id] : '') || 'person'
            : (n.agent_id ? agentById[n.agent_id]?.name : '') || 'node';
        const classes = [
          n.node_type === 'human' ? 'human' : '',
          // Entry emphasis only ever applies to agent nodes.
          n.node_type === 'agent' && n.id === graph.team.entry_node_id ? 'entry' : '',
        ]
          .filter(Boolean)
          .join(' ');
        return {
          group: 'nodes',
          data: {
            id: n.id,
            label: n.label || fallbackName,
            nodeType: n.node_type,
            borderColor:
              n.node_type === 'human'
                ? theme.purple
                : providerColor(n.agent_id ? agentById[n.agent_id]?.provider : undefined),
            ...(view === 'org' && (n.department || '').trim()
              ? { parent: `dept:${(n.department || '').trim()}` }
              : {}),
          },
          classes,
          ...(allHavePositions
            ? { position: { x: n.position[view].x, y: n.position[view].y } }
            : {}),
        };
      }),
      ...edges.map((e) => ({
        group: 'edges',
        data: {
          id: e.id,
          source: e.from_node_id,
          target: e.to_node_id,
          type: e.edge_type,
        },
      })),
    ];

    const cy = cytoscape({
      container: containerRef.current,
      elements,
      style: [
        {
          selector: 'node',
          style: {
            // Org view uses traditional sharp-cornered org-chart boxes;
            // network view keeps the softer rounded look.
            shape: view === 'org' ? 'rectangle' : 'round-rectangle',
            width: 'label',
            height: 30,
            padding: '10px',
            label: 'data(label)',
            'text-valign': 'center',
            'text-halign': 'center',
            'font-size': 12,
            color: theme.text,
            'background-color': theme.surface,
            'border-width': 2,
            'border-color': view === 'org' ? theme.text : 'data(borderColor)',
          },
        },
        {
          selector: 'node.department',
          style: {
            shape: 'round-rectangle',
            'background-color': theme.surfaceAlt,
            'background-opacity': 0.6,
            'border-width': 1.5,
            'border-color': theme.neutral,
            'border-style': 'solid',
            label: 'data(label)',
            'text-valign': 'top',
            'text-halign': 'center',
            'text-margin-y': -8,
            'font-size': 13,
            'font-weight': 'bold',
            color: theme.textMuted,
            'text-transform': 'uppercase',
            padding: '24px',
          },
        },
        {
          // Human crew members render distinctly from agents: softer purple
          // pill on the org chart, an ellipse on the network view.
          selector: 'node.human',
          style:
            view === 'org'
              ? {
                  shape: 'round-rectangle',
                  'corner-radius': 14,
                  'background-color': theme.tintPurple,
                  'border-color': theme.purple,
                }
              : {
                  shape: 'ellipse',
                  'background-color': theme.tintPurple,
                  'border-color': theme.purple,
                },
        },
        {
          selector: 'node.entry',
          style: {
            'border-width': 4,
            'font-weight': 'bold',
          },
        },
        {
          selector: 'node.active',
          style: {
            'underlay-color': theme.accent,
            'underlay-opacity': 0.35,
            'underlay-padding': 6,
          },
        },
        {
          selector: 'node.connect-source',
          style: {
            'background-color': theme.tintBlue,
            'border-color': theme.accent,
            'border-width': 3,
          },
        },
        {
          selector: 'node:selected',
          style: {
            'background-color': theme.tintBlue,
          },
        },
        {
          selector: 'edge',
          style: {
            width: 2,
            // Traditional right-angled connector lines on the org chart.
            'curve-style': view === 'org' ? 'taxi' : 'bezier',
            ...(view === 'org'
              ? { 'taxi-direction': 'downward', 'taxi-turn': 20, 'taxi-turn-min-distance': 8 }
              : {}),
            'target-arrow-shape': view === 'org' ? 'none' : 'triangle',
            'arrow-scale': 1.1,
          },
        },
        {
          selector: 'edge[type="delegates-to"]',
          style: {
            'line-style': 'solid',
            'line-color': theme.accent,
            'target-arrow-color': theme.accent,
          },
        },
        {
          selector: 'edge[type="hands-off-to"]',
          style: {
            'line-style': 'dashed',
            'line-color': theme.success,
            'target-arrow-color': theme.success,
          },
        },
        {
          selector: 'edge[type="reviews"]',
          style: {
            'line-style': 'dotted',
            'line-color': theme.warning,
            'target-arrow-color': theme.warning,
          },
        },
        {
          selector: 'edge:selected',
          style: {
            width: 4,
          },
        },
      ],
      layout: allHavePositions
        ? { name: 'preset' }
        : view === 'org'
        ? { name: 'breadthfirst', directed: true, spacingFactor: 1.6, grid: true }
        : { name: 'cose', animate: false },
      wheelSensitivity: 0.2,
    });

    cy.on('tap', 'node', (evt: any) => {
      const id = evt.target.id();
      if (id.startsWith('dept:')) return; // department containers aren't selectable
      const cbs = callbacksRef.current;
      if (cbs.connectMode) {
        if (!pendingSourceRef.current) {
          pendingSourceRef.current = id;
          evt.target.addClass('connect-source');
        } else if (pendingSourceRef.current !== id) {
          const from = pendingSourceRef.current;
          pendingSourceRef.current = null;
          cy.nodes().removeClass('connect-source');
          cbs.onConnect(from, id);
        } else {
          pendingSourceRef.current = null;
          cy.nodes().removeClass('connect-source');
        }
        return;
      }
      cbs.onSelectNode(id);
    });

    cy.on('tap', 'edge', (evt: any) => {
      callbacksRef.current.onSelectEdge(evt.target.id());
    });

    cy.on('tap', (evt: any) => {
      if (evt.target === cy) {
        callbacksRef.current.onSelectNode(null);
        callbacksRef.current.onSelectEdge(null);
      }
    });

    cy.on('dragfree', 'node', (evt: any) => {
      const id = evt.target.id();
      if (id.startsWith('dept:')) return;
      const pos = evt.target.position();
      callbacksRef.current.onMoveNode(id, {
        x: Math.round(pos.x),
        y: Math.round(pos.y),
      });
    });

    cyRef.current = cy;
    return () => {
      cy.destroy();
      cyRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graph, view, filter, agents, members, themeVersion]);

  // Reflect active runs without rebuilding. Live-status dots apply to agent
  // nodes only — human crew members never run.
  useEffect(() => {
    const cy = cyRef.current;
    if (!cy) return;
    cy.nodes().removeClass('active');
    activeNodeIds.forEach((id: string) => {
      const node = cy.getElementById(id);
      if (node && node.length > 0 && !node.hasClass('human')) node.addClass('active');
    });
  }, [activeNodeIds, graph, view, filter]);

  // Clear pending connect source when leaving connect mode.
  useEffect(() => {
    if (!connectMode) {
      pendingSourceRef.current = null;
      cyRef.current?.nodes().removeClass('connect-source');
    }
  }, [connectMode]);

  return (
    <div
      ref={containerRef}
      style={{
        width: '100%',
        height: '100%',
        minHeight: 420,
        background: 'var(--surface)',
        borderRadius: 4,
        border: '1px solid var(--border)',
        cursor: connectMode ? 'crosshair' : 'default',
      }}
    />
  );
};
