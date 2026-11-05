import React from 'react';
import type { ArtifactContextAction, QualityRowInfo } from '../../components/ArtifactList';
import type { DropZone } from '../../utils/artifactDrag';
import type { FactsIndex, ModuleFigures } from '../../utils/artifactFacts';
import { ErrorBanner } from '../../components/ui';
import { ArtifactEditor } from '../../components/ArtifactEditor';
import { ArtifactList } from '../../components/ArtifactList';
import { KpiStrip } from './KpiStrip';
import { LayersView } from './LayersView';
import { LevelDrill } from './LevelDrill';
import type { RequirementsView } from './ListToolbar';
import { TableGroup, TableView } from './TableView';
import { TreeDiagramView } from './TreeDiagramView';
import type { FoldTrigger } from './useFoldAll';
import type { Artifact, Setter } from './shared';

// The list pane: the main area on a wide screen, the Tree pane stacked. It
// holds the error banner, the create form, the filter panel (passed in, shown
// while toggled on), the figures, and the artifacts as a table, a tree or
// layers — or, on a phone, as a list, a tree or a drill-down by level — or a
// notice while a baseline loads. With the Table and Tree views on, the table
// is the tree with its columns and the tree is the diagram; a phone keeps its
// list and its outline.
// Props-only (refactor plan F7): the ModuleView shell owns the state, the
// effects, the loads and the handlers.
interface TreePaneProps {
  stacked: boolean;
  phone: boolean;
  stackedPane: 'tree' | 'document' | 'notes';
  view: RequirementsView;
  setView: (view: RequirementsView) => void;
  error: string;
  setError: Setter<string>;
  isBaselineView: boolean;
  isCreating: boolean;
  setIsCreating: Setter<boolean>;
  setPendingCreateContext: Setter<Partial<Artifact> | null>;
  setPendingCreatePlacement: Setter<{ anchorId: string; position: 'before' | 'after' } | null>;
  showFilterPanel: boolean;
  filterPanel: React.ReactNode;
  toolbar: React.ReactNode;
  figures: ModuleFigures;
  facts: FactsIndex;
  depthTrigger: { depth: number; token: number };
  /** The Table and Tree views (REQUIREMENTS_TREE_FEATURE), and their fold-all button. */
  treeViews: boolean;
  foldTrigger: FoldTrigger;
  artifacts: Artifact[];
  projectId: string;
  pendingCreateContext: Partial<Artifact> | null;
  handleCreateArtifact: (data: Partial<Artifact>) => void;
  baselineLoading: boolean;
  filteredArtifacts: Artifact[];
  totalArtifacts: number;
  selectedArtifactId: string | null;
  handleSelectArtifact: (artifactId: string | null) => void;
  handleReorderArtifact: (sourceId: string, targetId: string, mode: 'swap' | DropZone) => void;
  handleArtifactContextMenu: (action: ArtifactContextAction, artifact: Artifact) => void;
  clipboard: Artifact | null;
  qualityScores: Record<string, QualityRowInfo>;
  /** The chip search bar's group-by, which the table draws its rows under (issue #722). */
  tableGroups?: TableGroup[];
  groupedBy?: string;
}

export const TreePane: React.FC<TreePaneProps> = (p) => {
  const quality = p.isBaselineView ? undefined : p.qualityScores;
  const list = () => {
    if (p.isBaselineView && p.baselineLoading) {
      return (
        <div role="status" style={{ padding: '24px 16px', textAlign: 'center', color: 'var(--text-muted)', fontSize: 14 }}>
          Loading baseline…
          <div style={{ fontSize: 13, marginTop: 4 }}>A baseline holds the whole project, so this can take a moment.</div>
        </div>
      );
    }
    if (p.treeViews && !(p.phone && p.view === 'table')) return treeViewList();
    if (p.view === 'layers' && p.phone) {
      return (
        <LevelDrill
          artifacts={p.filteredArtifacts}
          facts={p.facts}
          onOpen={(id) => p.handleSelectArtifact(id)}
          onBack={() => p.setView('table')}
        />
      );
    }
    if (p.view === 'layers') {
      return (
        <LayersView
          artifacts={p.filteredArtifacts}
          selectedId={p.selectedArtifactId}
          onSelect={(id) => p.handleSelectArtifact(id)}
          facts={p.facts}
        />
      );
    }
    if (p.view === 'table') {
      return (
        <TableView
          artifacts={p.filteredArtifacts}
          allArtifacts={p.artifacts}
          total={p.totalArtifacts}
          selectedId={p.selectedArtifactId}
          onSelect={(id) => p.handleSelectArtifact(id)}
          facts={p.facts}
          qualityScores={quality}
          readOnly={p.isBaselineView}
          canPaste={!!p.clipboard}
          onReorder={p.handleReorderArtifact}
          onContextMenuAction={p.handleArtifactContextMenu}
          phone={p.phone}
          groups={p.tableGroups}
          groupedBy={p.groupedBy}
        />
      );
    }
    return (
      <ArtifactList
        artifacts={p.filteredArtifacts}
        allArtifacts={p.artifacts}
        selectedId={p.selectedArtifactId || undefined}
        onSelect={p.handleSelectArtifact}
        onReorder={p.handleReorderArtifact}
        onContextMenuAction={p.handleArtifactContextMenu}
        canPaste={!!p.clipboard}
        defaultCollapsed
        depthTrigger={p.depthTrigger}
        readOnly={p.isBaselineView}
        qualityScores={quality}
        hideHeading
        facts={p.facts}
        compact={p.stacked}
      />
    );
  };

  // The Table and Tree views: the hierarchy with every column, the diagram,
  // or (a phone's Tree) the outline.
  const treeViewList = () => {
    if (p.view === 'tree' && !p.phone) {
      return (
        <TreeDiagramView
          artifacts={p.filteredArtifacts}
          selectedId={p.selectedArtifactId}
          onSelect={(id) => p.handleSelectArtifact(id)}
          facts={p.facts}
          foldTrigger={p.foldTrigger}
        />
      );
    }
    const outline = p.phone;
    const trigger = p.foldTrigger.token ? { depth: p.foldTrigger.folded ? 1 : Infinity, token: p.foldTrigger.token } : undefined;
    return (
      <ArtifactList
        artifacts={p.filteredArtifacts}
        allArtifacts={p.artifacts}
        selectedId={p.selectedArtifactId || undefined}
        onSelect={p.handleSelectArtifact}
        onReorder={p.handleReorderArtifact}
        onContextMenuAction={p.handleArtifactContextMenu}
        canPaste={!!p.clipboard}
        defaultCollapsed={outline}
        depthTrigger={trigger}
        readOnly={p.isBaselineView}
        qualityScores={quality}
        hideHeading
        facts={p.facts}
        compact={outline}
        groups={p.view === 'table' ? p.tableGroups : undefined}
      />
    );
  };

  return (
    <div
      style={{
        flex: 1,
        minWidth: 0,
        minHeight: 0,
        display: p.stacked && p.stackedPane !== 'tree' ? 'none' : 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
      }}
    >
      {p.toolbar}
      <div
        style={{
          flex: 1,
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 10,
          padding: p.phone ? (p.view === 'table' || p.view === 'layers' ? 0 : '10px 12px 0') : p.stacked ? '10px 12px 0' : '12px 16px 16px 20px',
          overflow: 'hidden',
        }}
      >
        <ErrorBanner message={p.error} onDismiss={() => p.setError('')} />
        {p.showFilterPanel && <div style={{ flexShrink: 0, maxHeight: '50%', overflow: 'auto' }}>{p.filterPanel}</div>}
        {p.isCreating && !p.isBaselineView && (
          <div style={{ flexShrink: 0, maxWidth: 720, maxHeight: p.stacked ? undefined : '60%', overflow: 'auto' }}>
            <ArtifactEditor
              artifacts={p.artifacts}
              projectId={p.projectId}
              initialData={p.pendingCreateContext ?? undefined}
              onSave={p.handleCreateArtifact}
              onCancel={() => {
                p.setIsCreating(false);
                p.setPendingCreateContext(null);
                p.setPendingCreatePlacement(null);
                p.setError('');
              }}
            />
          </div>
        )}
        {!p.stacked && <KpiStrip figures={p.figures} />}
        {list()}
      </div>
    </div>
  );
};
