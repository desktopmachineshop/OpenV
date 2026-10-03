import React from 'react';
import type { ArtifactContextAction, QualityRowInfo } from '../../components/ArtifactList';
import type { DropZone } from '../../utils/artifactDrag';
import { ErrorBanner } from '../../components/ui';
import { ArtifactEditor } from '../../components/ArtifactEditor';
import { ArtifactList } from '../../components/ArtifactList';
import type { Artifact, Setter } from './shared';

// The tree pane: the left column on a wide screen, the Tree pane stacked. It
// holds the error banner, the create button and form, the search box with the
// filter panel (passed in, shown while toggled on), collapse and expand all,
// and the artifact tree, or a notice while a baseline loads.
// Props-only (refactor plan F7): the ModuleView shell owns the state, the
// effects, the loads and the handlers.
interface TreePaneProps {
  stacked: boolean;
  stackedPane: 'tree' | 'document' | 'notes';
  leftColumnDrawn: number;
  error: string;
  setError: Setter<string>;
  isBaselineView: boolean;
  isCreating: boolean;
  setIsCreating: Setter<boolean>;
  setPendingCreateContext: Setter<Partial<Artifact> | null>;
  setPendingCreatePlacement: Setter<{ anchorId: string; position: 'before' | 'after' } | null>;
  searchText: string;
  setSearchText: Setter<string>;
  buildFilterSummary: () => string;
  setShowFilterPanel: Setter<boolean>;
  showFilterPanel: boolean;
  filterPanel: React.ReactNode;
  setCollapseAllToken: Setter<number>;
  setExpandAllToken: Setter<number>;
  artifacts: Artifact[];
  projectId: string;
  pendingCreateContext: Partial<Artifact> | null;
  handleCreateArtifact: (data: Partial<Artifact>) => void;
  baselineLoading: boolean;
  filteredArtifacts: Artifact[];
  selectedArtifactId: string | null;
  handleSelectArtifact: (artifactId: string | null) => void;
  handleReorderArtifact: (sourceId: string, targetId: string, mode: 'swap' | DropZone) => void;
  handleArtifactContextMenu: (action: ArtifactContextAction, artifact: Artifact) => void;
  clipboard: Artifact | null;
  collapseAllToken: number;
  expandAllToken: number;
  qualityScores: Record<string, QualityRowInfo>;
}

export const TreePane: React.FC<TreePaneProps> = ({
  stacked,
  stackedPane,
  leftColumnDrawn,
  error,
  setError,
  isBaselineView,
  isCreating,
  setIsCreating,
  setPendingCreateContext,
  setPendingCreatePlacement,
  searchText,
  setSearchText,
  buildFilterSummary,
  setShowFilterPanel,
  showFilterPanel,
  filterPanel,
  setCollapseAllToken,
  setExpandAllToken,
  artifacts,
  projectId,
  pendingCreateContext,
  handleCreateArtifact,
  baselineLoading,
  filteredArtifacts,
  selectedArtifactId,
  handleSelectArtifact,
  handleReorderArtifact,
  handleArtifactContextMenu,
  clipboard,
  collapseAllToken,
  expandAllToken,
  qualityScores,
}) => (
  <div style={stacked
    ? { flex: 1, minWidth: 0, display: stackedPane === 'tree' ? 'flex' : 'none', flexDirection: 'column', overflowX: 'hidden', overflowY: 'hidden', minHeight: 0 }
    : { width: `${leftColumnDrawn}px`, minWidth: '200px', maxWidth: '800px', display: 'flex', flexDirection: 'column', overflowX: 'hidden', overflowY: 'hidden', minHeight: 0, paddingRight: '10px' }}>
    <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 15 }} />
    {!isBaselineView && (
      <button
        onClick={() => {
          setIsCreating(!isCreating);
          // A manual open (or cancel) always starts from a blank form, and
          // without the placement a context-menu create had asked for.
          setPendingCreateContext(null);
          setPendingCreatePlacement(null);
          setError('');
        }}
        className="button"
        style={{ width: '100%', marginBottom: stacked ? '10px' : '20px' }}
      >
        {isCreating ? 'Cancel' : '+ New Artifact'}
      </button>
    )}

    <div style={{ marginBottom: stacked ? '10px' : '20px' }}>
      {!stacked && (
      <label style={{ display: 'block', fontSize: '12px', fontWeight: 'bold', marginBottom: '8px', color: 'var(--text)' }}>
        Filter and Search:
      </label>
      )}
      <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
        <div style={{ position: 'relative', flex: 1 }}>
          <input
            type="text"
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            placeholder="Search..."
            style={{
              width: '100%',
              padding: '8px 160px 8px 8px',
              borderRadius: '4px',
              border: '1px solid var(--neutral-mid)',
              fontSize: '14px',
              backgroundColor: 'var(--surface)',
            }}
          />
          <div
            style={{
              position: 'absolute',
              top: '50%',
              right: '10px',
              transform: 'translateY(-50%)',
              fontSize: '12px',
              color: 'var(--text-muted)',
              maxWidth: '140px',
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              pointerEvents: 'none',
            }}
            title={buildFilterSummary()}
          >
            {buildFilterSummary()}
          </div>
        </div>
        <button
          onClick={() => setShowFilterPanel((prev) => !prev)}
          className="button-secondary"
          style={{ padding: '6px 10px', fontSize: '14px' }}
          title="Toggle filters"
        >
          ⚙
        </button>
      </div>
      {showFilterPanel && filterPanel}
      <div style={{ marginTop: '10px', display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
        <button
          onClick={() => {
            setCollapseAllToken((prev) => prev + 1);
          }}
          className="button-secondary"
          style={{ padding: '6px 10px', fontSize: '12px' }}
        >
          Collapse all
        </button>
        <button
          onClick={() => {
            setExpandAllToken((prev) => prev + 1);
          }}
          className="button-secondary"
          style={{ padding: '6px 10px', fontSize: '12px' }}
        >
          Expand all
        </button>
      </div>
    </div>

    {isCreating && !isBaselineView && (
      <ArtifactEditor
        artifacts={artifacts}
        projectId={projectId}
        initialData={pendingCreateContext ?? undefined}
        onSave={handleCreateArtifact}
        onCancel={() => {
          setIsCreating(false);
          setPendingCreateContext(null);
          setPendingCreatePlacement(null);
          setError('');
        }}
      />
    )}

    {isBaselineView && baselineLoading ? (
      <div
        role="status"
        style={{
          padding: '24px 16px',
          textAlign: 'center',
          color: 'var(--text-muted)',
          fontSize: 14,
        }}
      >
        Loading baseline…
        <div style={{ fontSize: 13, marginTop: 4 }}>
          A baseline holds the whole project, so this can take a moment.
        </div>
      </div>
    ) : (
      <ArtifactList
        artifacts={filteredArtifacts}
        allArtifacts={artifacts}
        selectedId={selectedArtifactId || undefined}
        onSelect={handleSelectArtifact}
        onReorder={handleReorderArtifact}
        onContextMenuAction={handleArtifactContextMenu}
        canPaste={!!clipboard}
        defaultCollapsed
        collapseAllTrigger={collapseAllToken}
        expandAllTrigger={expandAllToken}
        readOnly={isBaselineView}
        qualityScores={isBaselineView ? undefined : qualityScores}
        hideHeading={stacked}
      />
    )}
  </div>
);
