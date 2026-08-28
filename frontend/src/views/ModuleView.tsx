import React, { useState, useEffect } from 'react';
import { useAppStore } from '../state/store';
import { artifactAPI, linkAPI, attachmentAPI, baselineAPI, projectAPI, Artifact, Link, Attachment, Baseline, ProjectExport } from '../api/client';
import { ArtifactEditor } from '../components/ArtifactEditor';
import { ArtifactList } from '../components/ArtifactList';
import { ArtifactHeader } from '../components/ArtifactHeader';
import { ArtifactDetails } from '../components/ArtifactDetails';
import { ChatterPanel } from '../components/ChatterPanel';
import { HelpSidebar } from '../components/HelpSidebar';
import { Navbar } from '../components/Navbar';

interface ModuleViewProps {
  onSwitchProject?: () => void;
}

export const ModuleView: React.FC<ModuleViewProps> = ({ onSwitchProject }) => {
  const [isCreating, setIsCreating] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [editingArtifact, setEditingArtifact] = useState<Artifact | undefined>();
  const [error, setError] = useState<string>('');
  const [allLinks, setAllLinks] = useState<Link[]>([]);
  const [searchText, setSearchText] = useState<string>('');
  const [searchExact, setSearchExact] = useState<boolean>(false);
  const [filterLogic, setFilterLogic] = useState<'and' | 'or'>('and');
  const [filterRows, setFilterRows] = useState<Array<{ id: string; field: string; value: string; comparator: string }>>([
    { id: 'filter-1', field: 'type', value: '', comparator: 'contains' },
  ]);
  const [filterPresetName, setFilterPresetName] = useState<string>('');
  const [filterPresets, setFilterPresets] = useState<Array<{ name: string; data: string }>>([]);
  const [selectedPreset, setSelectedPreset] = useState<string>('');
  const [showFilterPanel, setShowFilterPanel] = useState<boolean>(false);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [uploadingAttachmentId, setUploadingAttachmentId] = useState<string | null>(null);
  const [baselines, setBaselines] = useState<Baseline[]>([]);
  const [activeBaselineId, setActiveBaselineId] = useState<string>('live');
  const [baselineData, setBaselineData] = useState<ProjectExport | null>(null);
  const [collapseAllToken, setCollapseAllToken] = useState<number>(0);
  const [expandAllToken, setExpandAllToken] = useState<number>(0);
  const [previewVersion, setPreviewVersion] = useState<Artifact | null>(null);
  
  // Resizable columns state
  const [leftColumnWidth, setLeftColumnWidth] = useState<number>(() => {
    const saved = localStorage.getItem('openv-leftColumnWidth');
    return saved ? parseInt(saved) : 400;
  });
  const [rightColumnWidth, setRightColumnWidth] = useState<number>(() => {
    const saved = localStorage.getItem('openv-rightColumnWidth');
    return saved ? parseInt(saved) : 320;
  });
  const [isResizing, setIsResizing] = useState<'left' | 'right' | null>(null);

  const {
    projectId,
    artifacts,
    setArtifacts,
    addArtifact,
    updateArtifact,
    removeArtifact,
    addLink,
    selectedArtifactId,
    setSelectedArtifactId,
    projects,
  } = useAppStore();

  const isBaselineView = activeBaselineId !== 'live';

  const normalizeParentId = (parentId?: string | null): string | null => parentId ?? null;

  const compareArtifacts = (left: Artifact, right: Artifact): number => {
    const leftOrder = left.sort_order ?? 0;
    const rightOrder = right.sort_order ?? 0;
    const leftHasOrder = leftOrder > 0;
    const rightHasOrder = rightOrder > 0;

    if (leftHasOrder && rightHasOrder) {
      return leftOrder - rightOrder;
    }

    if (!leftHasOrder && !rightHasOrder) {
      return new Date(left.created_at).getTime() - new Date(right.created_at).getTime();
    }

    return leftHasOrder ? -1 : 1;
  };

  // Handle column resizing
  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!isResizing) return;
      
      if (isResizing === 'left') {
        const newWidth = Math.max(200, Math.min(800, e.clientX - 20));
        setLeftColumnWidth(newWidth);
      } else if (isResizing === 'right') {
        const newWidth = Math.max(250, Math.min(600, window.innerWidth - e.clientX - 20));
        setRightColumnWidth(newWidth);
      }
    };

    const handleMouseUp = () => {
      if (isResizing) {
        if (isResizing === 'left') {
          localStorage.setItem('openv-leftColumnWidth', leftColumnWidth.toString());
        } else {
          localStorage.setItem('openv-rightColumnWidth', rightColumnWidth.toString());
        }
        setIsResizing(null);
      }
    };

    if (isResizing) {
      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';
    }

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, [isResizing, leftColumnWidth, rightColumnWidth]);

  // Load artifacts on component mount
  useEffect(() => {
    if (projectId) {
      loadArtifacts();
      loadLinks();
      loadBaselines();
      setActiveBaselineId('live');
      setBaselineData(null);
    }
  }, [projectId]);

  // Load attachments when artifact is selected or when editing
  useEffect(() => {
    if (isBaselineView) {
      setAttachments([]);
      return;
    }
    const artifactIdToLoad = editingArtifact?.id || selectedArtifactId;
    if (artifactIdToLoad) {
      loadAttachments(artifactIdToLoad);
    } else {
      setAttachments([]);
    }
  }, [selectedArtifactId, editingArtifact?.id, isBaselineView]);

  const loadArtifacts = async () => {
    try {
      const response = await artifactAPI.list(projectId);
      setArtifacts(response.data || []);
      setError('');
    } catch (error: any) {
      console.error('Failed to load artifacts:', error);
      setArtifacts([]);
      setError(`Failed to load artifacts: ${error.response?.data || error.message}`);
    }
  };

  const loadLinks = async () => {
    try {
      const response = await linkAPI.list(projectId);
      setAllLinks(response.data || []);
    } catch (error: any) {
      console.error('Failed to load links:', error);
      setAllLinks([]);
    }
  };

  const loadBaselines = async () => {
    if (!projectId) return;
    try {
      const response = await baselineAPI.list(projectId);
      setBaselines(response.data || []);
    } catch (error: any) {
      console.error('Failed to load baselines:', error);
      setBaselines([]);
    }
  };

  const loadAttachments = async (artifactId: string) => {
    try {
      const response = await attachmentAPI.listByArtifact(artifactId);
      setAttachments(response.data || []);
    } catch (error: any) {
      console.error('Failed to load attachments:', error);
      setAttachments([]);
    }
  };

  // Handle artifact selection with automatic exit from edit/preview modes
  const handleSelectArtifact = (artifactId: string | null) => {
    // If selecting a different artifact than currently selected
    if (artifactId !== selectedArtifactId) {
      // Exit edit mode if active
      if (isEditing) {
        setIsEditing(false);
        setEditingArtifact(undefined);
      }
      // Exit history/preview mode if active
      if (previewVersion) {
        setPreviewVersion(null);
      }
    }
    // Set the new selected artifact
    setSelectedArtifactId(artifactId);
  };

  const handleUploadAttachment = async (file: File) => {
    if (!selectedArtifactId) {
      setError('No artifact selected');
      return;
    }

    setUploadingAttachmentId(selectedArtifactId);
    try {
      const response = await attachmentAPI.upload(selectedArtifactId, file);
      setAttachments([...attachments, response.data]);
      setError('');
    } catch (error: any) {
      console.error('Failed to upload attachment:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to upload image: ${errorMsg}`);
    } finally {
      setUploadingAttachmentId(null);
    }
  };

  const handleDeleteAttachment = async (attachmentId: string) => {
    try {
      await attachmentAPI.delete(attachmentId);
      setAttachments(attachments.filter((a) => a.id !== attachmentId));
      setError('');
    } catch (error: any) {
      console.error('Failed to delete attachment:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to delete image: ${errorMsg}`);
    }
  };

  const handleCreateArtifact = async (data: Partial<Artifact>) => {
    try {
      const response = await artifactAPI.create({
        project_id: projectId,
        ...data,
      });
      addArtifact(response.data);
      setIsCreating(false);
      setError('');
    } catch (error: any) {
      console.error('Failed to create artifact:', error);
      let errorMsg = 'Unknown error';
      if (error.response?.data) {
        errorMsg = typeof error.response.data === 'string' ? error.response.data : JSON.stringify(error.response.data);
      } else if (error.message) {
        errorMsg = error.message;
      }
      setError(`Failed to create artifact: ${errorMsg}`);
    }
  };

  const handleUpdateArtifact = async (data: Partial<Artifact>) => {
    try {
      if (!editingArtifact) return;
      const response = await artifactAPI.update(editingArtifact.id, data);
      updateArtifact(response.data);
      setIsEditing(false);
      setEditingArtifact(undefined);
      setError('');
    } catch (error: any) {
      console.error('Failed to update artifact:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to update artifact: ${errorMsg}`);
    }
  };

  const handleDeleteArtifact = async (id: string) => {
    if (!window.confirm('Are you sure you want to delete this artifact?')) {
      return;
    }
    try {
      await artifactAPI.delete(id);
      removeArtifact(id);
      if (selectedArtifactId === id) {
        handleSelectArtifact(null);
      }
      setError('');
    } catch (error: any) {
      console.error('Failed to delete artifact:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to delete artifact: ${errorMsg}`);
    }
  };

  const handleEditArtifact = (artifact: Artifact) => {
    // Clear preview mode when entering edit
    if (previewVersion) {
      setPreviewVersion(null);
    }
    setSelectedArtifactId(artifact.id);
    setEditingArtifact(artifact);
    setIsEditing(true);
    setError('');
  };

  const handleCreateLink = async (linkData: Partial<Link>) => {
    try {
      const response = await linkAPI.create(linkData);
      addLink(response.data);
      setAllLinks([...allLinks, response.data]);
      
      // Reload artifacts to get updated link snapshots for both fromID and toID
      await loadArtifacts();
      
      alert('Link created successfully');
      setError('');
    } catch (error: any) {
      console.error('Failed to create link:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to create link: ${errorMsg}`);
    }
  };

  const handleBaselineChange = async (baselineId: string) => {
    setActiveBaselineId(baselineId);
    handleSelectArtifact(null);
    setIsEditing(false);
    setIsCreating(false);
    setEditingArtifact(undefined);
    setPreviewVersion(null);

    if (baselineId === 'live') {
      setBaselineData(null);
      return;
    }

    try {
      const response = await baselineAPI.get(baselineId);
      setBaselineData(response.data);
      setError('');
    } catch (error: any) {
      console.error('Failed to load baseline:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to load baseline: ${errorMsg}`);
    }
  };

  const handleCaptureBaseline = async () => {
    if (!projectId) return;
    const name = window.prompt('Baseline name:', '') || '';
    if (!name.trim()) {
      setError('Baseline name is required');
      return;
    }

    try {
      await baselineAPI.create(projectId, name.trim());
      await loadBaselines();
      setError('');
    } catch (error: any) {
      console.error('Failed to capture baseline:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to capture baseline: ${errorMsg}`);
    }
  };

  const handleDeleteBaseline = async (baselineId: string) => {
    if (baselineId === 'live') return;
    if (!window.confirm('Delete this baseline? This cannot be undone.')) {
      return;
    }

    try {
      await baselineAPI.delete(baselineId);
      await loadBaselines();
      setActiveBaselineId('live');
      setBaselineData(null);
      setError('');
    } catch (error: any) {
      console.error('Failed to delete baseline:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to delete baseline: ${errorMsg}`);
    }
  };

  const handleGenerateReport = async () => {
    if (!projectId) return;
    try {
      await projectAPI.report(projectId, activeBaselineId);
      setError('');
    } catch (error: any) {
      console.error('Failed to generate report:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to generate report: ${errorMsg}`);
    }
  };

  const handleReorderArtifact = async (sourceId: string, targetId: string, mode: 'swap' | 'insert') => {
    if (isBaselineView) return;

    const source = artifacts.find((item) => item.id === sourceId);
    const target = artifacts.find((item) => item.id === targetId);
    if (!source || !target) return;

    if (normalizeParentId(source.parent_id) !== normalizeParentId(target.parent_id)) {
      return;
    }

    const siblings = artifacts
      .filter((item) => normalizeParentId(item.parent_id) === normalizeParentId(source.parent_id))
      .sort(compareArtifacts);

    const sourceIndex = siblings.findIndex((item) => item.id === sourceId);
    const targetIndex = siblings.findIndex((item) => item.id === targetId);
    if (sourceIndex === -1 || targetIndex === -1) return;

    const reordered = [...siblings];
    if (mode === 'swap') {
      const temp = reordered[sourceIndex];
      reordered[sourceIndex] = reordered[targetIndex];
      reordered[targetIndex] = temp;
    } else {
      const [moved] = reordered.splice(sourceIndex, 1);
      const insertIndex = targetIndex > sourceIndex ? targetIndex - 1 : targetIndex;
      reordered.splice(insertIndex, 0, moved);
    }

    const updates = reordered
      .map((artifact, index) => ({
        artifact,
        newOrder: index + 1,
      }))
      .filter(({ artifact, newOrder }) => (artifact.sort_order ?? 0) !== newOrder);

    if (updates.length === 0) {
      return;
    }

    try {
      const responses = await Promise.all(
        updates.map(({ artifact, newOrder }) =>
          artifactAPI.update(artifact.id, {
            parent_id: artifact.parent_id ?? null,
            type: artifact.type,
            title: artifact.title,
            body: artifact.body,
            attributes: artifact.attributes,
            sort_order: newOrder,
          })
        )
      );

      const updatedMap = new Map<string, Artifact>(
        responses.map((response) => [response.data.id, response.data])
      );

      const nextArtifacts = artifacts.map((item) => updatedMap.get(item.id) || item);
      setArtifacts(nextArtifacts);
      setError('');
    } catch (error: any) {
      console.error('Failed to reorder artifacts:', error);
      const errorMsg = error.response?.data || error.message || 'Unknown error';
      setError(`Failed to reorder artifacts: ${errorMsg}`);
    }
  };

  const handleArtifactContextMenu = (action: 'create-before' | 'create-after' | 'create-child', artifact: Artifact) => {
    // Auto-populate form based on action
    if (action === 'create-before' || action === 'create-after') {
      // New sibling - use same parent and type
      const newArtifact: Partial<Artifact> = {
        parent_id: artifact.parent_id ?? null,
        type: artifact.type,
        title: '',
        body: '',
        attributes: {},
      };
      // Store for later use when creating
      (window as any).__pendingArtifactData = newArtifact;
    } else if (action === 'create-child') {
      // New child - this artifact is the parent, inherit type if appropriate
      const newArtifact: Partial<Artifact> = {
        parent_id: artifact.id,
        type: artifact.type,
        title: '',
        body: '',
        attributes: {},
      };
      (window as any).__pendingArtifactData = newArtifact;
    }
    
    // Switch to create mode
    setIsEditing(false);
    setEditingArtifact(undefined);
    setIsCreating(true);
    setError('');
  };

  const activeArtifacts = isBaselineView ? (baselineData?.artifacts || []) : artifacts;
  const activeLinks = isBaselineView ? (baselineData?.links || []) : allLinks;

  const fieldOptions: Array<{ value: string; label: string }> = [
    { value: 'id', label: 'ID' },
    { value: 'project_id', label: 'Project ID' },
    { value: 'parent_id', label: 'Parent ID' },
    { value: 'type', label: 'Type' },
    { value: 'title', label: 'Title' },
    { value: 'body', label: 'Body' },
    { value: 'attributes', label: 'Attributes' },
    { value: 'version', label: 'Version' },
    { value: 'created_at', label: 'Created At' },
    { value: 'updated_at', label: 'Updated At' },
  ];

  // Fields with finite selection options
  const finiteFields = ['type'];

  // Get unique values for a field to populate selection dropdown
  const getFieldUniqueValues = (fieldName: string): string[] => {
    const values = new Set<string>();
    activeArtifacts.forEach((artifact) => {
      const val = getFieldValue(artifact, fieldName);
      if (val) values.add(val);
    });
    return Array.from(values).sort();
  };

  const getFieldValue = (artifact: Artifact, field: string): string => {
    switch (field) {
      case 'id':
        return artifact.id;
      case 'project_id':
        return artifact.project_id;
      case 'parent_id':
        return artifact.parent_id ?? '';
      case 'type':
        return artifact.type;
      case 'title':
        return artifact.title;
      case 'body':
        return artifact.body || '';
      case 'attributes':
        return JSON.stringify(artifact.attributes || {});
      case 'version':
        return String(artifact.version ?? '');
      case 'created_at':
        return artifact.created_at || '';
      case 'updated_at':
        return artifact.updated_at || '';
      default:
        return '';
    }
  };

  const matchesSearch = (artifact: Artifact, query: string): boolean => {
    const normalized = query.trim();
    if (!normalized) return true;

    const haystack = [
      artifact.title,
      artifact.body,
      artifact.type,
      artifact.id,
      artifact.parent_id ?? '',
      JSON.stringify(artifact.attributes || {}),
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();

    if (searchExact) {
      return haystack.includes(normalized.toLowerCase());
    }

    return haystack.includes(normalized.toLowerCase());
  };

  const applyComparator = (fieldValue: string, comparator: string, compareValue: string): boolean => {
    const normalizedField = fieldValue.toLowerCase();
    const normalizedCompare = compareValue.toLowerCase();

    if (comparator === 'equals') {
      return normalizedField === normalizedCompare;
    }

    if (comparator === 'not-equals') {
      return normalizedField !== normalizedCompare;
    }

    if (comparator === 'starts-with') {
      return normalizedField.startsWith(normalizedCompare);
    }

    if (comparator === 'ends-with') {
      return normalizedField.endsWith(normalizedCompare);
    }

    if (comparator === 'not-contains') {
      return !normalizedField.includes(normalizedCompare);
    }

    if (comparator === 'gt' || comparator === 'lt') {
      const fieldNumber = Number(fieldValue);
      const compareNumber = Number(compareValue);
      if (!Number.isNaN(fieldNumber) && !Number.isNaN(compareNumber)) {
        return comparator === 'gt' ? fieldNumber > compareNumber : fieldNumber < compareNumber;
      }

      const fieldDate = Date.parse(fieldValue);
      const compareDate = Date.parse(compareValue);
      if (!Number.isNaN(fieldDate) && !Number.isNaN(compareDate)) {
        return comparator === 'gt' ? fieldDate > compareDate : fieldDate < compareDate;
      }

      return comparator === 'gt'
        ? normalizedField > normalizedCompare
        : normalizedField < normalizedCompare;
    }

    return normalizedField.includes(normalizedCompare);
  };

  const matchesFieldFilters = (artifact: Artifact): boolean => {
    const activeRows = filterRows.filter((row) => row.value.trim() !== '');
    if (activeRows.length === 0) {
      return true;
    }

    const evaluations = activeRows.map((row) => {
      const value = row.value.trim().toLowerCase();
      const fieldValue = getFieldValue(artifact, row.field);
      return applyComparator(fieldValue, row.comparator, value);
    });

    return filterLogic === 'and'
      ? evaluations.every(Boolean)
      : evaluations.some(Boolean);
  };

  const filteredArtifacts = activeArtifacts.filter(
    (artifact) => matchesSearch(artifact, searchText) && matchesFieldFilters(artifact)
  );

  const buildFilterSummary = (): string => {
    const parts: string[] = [];

    if (searchText.trim()) {
      parts.push(searchExact ? `Exact search: "${searchText.trim()}"` : `Search: "${searchText.trim()}"`);
    }

    const activeRows = filterRows.filter((row) => row.value.trim() !== '');
    if (activeRows.length > 0) {
      const rowSummaries = activeRows.map((row) => `${row.field} ${row.comparator} "${row.value.trim()}"`);
      parts.push(`Filters (${filterLogic.toUpperCase()}): ${rowSummaries.join(filterLogic === 'and' ? ' + ' : ' | ')}`);
    }

    if (parts.length === 0) {
      return 'No filters applied';
    }

    return parts.join(' • ');
  };

  useEffect(() => {
    const saved = window.localStorage.getItem('artifactFilterPresets');
    if (!saved) return;
    try {
      const parsed = JSON.parse(saved) as Array<{ name: string; data: string }>;
      setFilterPresets(parsed);
    } catch (error) {
      console.warn('Failed to load filter presets', error);
    }
  }, []);

  const savePresets = (presets: Array<{ name: string; data: string }>) => {
    setFilterPresets(presets);
    window.localStorage.setItem('artifactFilterPresets', JSON.stringify(presets));
  };

  const applyPreset = (presetData: string) => {
    try {
      const parsed = JSON.parse(presetData) as {
        searchText: string;
        searchExact: boolean;
        filterLogic: 'and' | 'or';
        filterRows: Array<{ id: string; field: string; value: string; comparator: string }>;
      };

      setSearchText(parsed.searchText || '');
      setSearchExact(Boolean(parsed.searchExact));
      setFilterLogic(parsed.filterLogic || 'and');
      setFilterRows(
        parsed.filterRows && parsed.filterRows.length > 0
          ? parsed.filterRows
          : [{ id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' }]
      );
    } catch (error) {
      console.warn('Failed to apply preset', error);
    }
  };

  if (!projectId) {
    return (
      <div className="card">
        <h3>No Project Selected</h3>
        <p>Please select a project to get started.</p>
      </div>
    );
  }

  const selectedArtifact = activeArtifacts.find((a) => a.id === selectedArtifactId);
  const detailAttachments = isBaselineView ? [] : attachments;

  return (
    <>
      <HelpSidebar />
      <Navbar
        title={(() => {
          const project = projects.find((p) => p.id === projectId);
          const name = project?.name || 'Unknown';
          const description = project?.description || '';
          return (
            <div style={{ display: 'inline-flex', flexDirection: 'column', alignItems: 'center', gap: '2px' }}>
              <div style={{ fontSize: '18px', fontWeight: 600 }}>{name}</div>
              {description && (
                <div style={{ fontSize: '11px', color: 'var(--text-muted)' }}>{description}</div>
              )}
            </div>
          );
        })()}
        onSwitchProject={onSwitchProject}
        showSwitchButton={true}
        baselineOptions={baselines.map((b) => ({ id: b.id, name: b.name }))}
        selectedBaselineId={activeBaselineId}
        onBaselineChange={handleBaselineChange}
        onCaptureBaseline={handleCaptureBaseline}
        onDeleteBaseline={handleDeleteBaseline}
        onGenerateReport={handleGenerateReport}
      />
      <div style={{ display: 'flex', gap: '0', paddingLeft: '20px', paddingRight: '20px', height: 'calc(100vh - 120px)', overflow: 'hidden' }}>
      <div style={{ width: `${leftColumnWidth}px`, minWidth: '200px', maxWidth: '800px', display: 'flex', flexDirection: 'column', overflowX: 'hidden', overflowY: 'auto', minHeight: 0, paddingRight: '10px' }}>
        {error && (
          <div
            style={{
              padding: '12px',
              marginBottom: '15px',
              backgroundColor: 'rgba(231, 76, 60, 0.2)',
              border: '1px solid var(--danger)',
              borderRadius: '4px',
              color: 'var(--danger)',
              fontSize: '12px',
            }}
          >
            {error}
          </div>
        )}
        {!isBaselineView && (
          <button
            onClick={() => {
              setIsCreating(!isCreating);
              setError('');
            }}
            className="button"
            style={{ width: '100%', marginBottom: '20px' }}
          >
            {isCreating ? 'Cancel' : '+ New Artifact'}
          </button>
        )}

        <div style={{ marginBottom: '20px' }}>
          <label style={{ display: 'block', fontSize: '12px', fontWeight: 'bold', marginBottom: '8px', color: 'var(--text)' }}>
            Filter and Search:
          </label>
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
                  fontSize: '11px',
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
          {showFilterPanel && (
            <div style={{
              marginTop: '12px',
              border: '1px solid var(--border)',
              borderRadius: '6px',
              padding: '10px',
              backgroundColor: 'var(--surface-alt)',
              display: 'flex',
              flexDirection: 'column',
              gap: '10px',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap' }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', color: 'var(--text)' }}>
                  <input
                    type="checkbox"
                    checked={searchExact}
                    onChange={(e) => setSearchExact(e.target.checked)}
                  />
                  Exact match
                </label>
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <span style={{ fontSize: '12px', color: 'var(--text)' }}>Filter logic</span>
                  <select
                    value={filterLogic}
                    onChange={(e) => setFilterLogic(e.target.value === 'or' ? 'or' : 'and')}
                    style={{
                      padding: '6px',
                      borderRadius: '4px',
                      border: '1px solid var(--neutral-mid)',
                      fontSize: '12px',
                    }}
                  >
                    <option value="and">AND</option>
                    <option value="or">OR</option>
                  </select>
                </div>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                {filterRows.map((row) => {
                  const isFiniteField = finiteFields.includes(row.field);
                  const comparatorIsFinite = ['equals', 'not-equals'].includes(row.comparator);
                  const shouldUseSelect = isFiniteField && comparatorIsFinite;
                  const fieldValues = shouldUseSelect ? getFieldUniqueValues(row.field) : [];

                  return (
                    <div key={row.id} style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                      <select
                        value={row.field}
                        onChange={(e) => {
                          const next = filterRows.map((item) =>
                            item.id === row.id ? { ...item, field: e.target.value } : item
                          );
                          setFilterRows(next);
                        }}
                        style={{
                          flex: '0 0 140px',
                          padding: '6px',
                          borderRadius: '4px',
                          border: '1px solid var(--neutral-mid)',
                          fontSize: '12px',
                        }}
                      >
                        {fieldOptions.map((option) => (
                          <option key={option.value} value={option.value}>
                            {option.label}
                          </option>
                        ))}
                      </select>
                      <select
                        value={row.comparator}
                        onChange={(e) => {
                          const next = filterRows.map((item) =>
                            item.id === row.id ? { ...item, comparator: e.target.value } : item
                          );
                          setFilterRows(next);
                        }}
                        style={{
                          flex: '0 0 140px',
                          padding: '6px',
                          borderRadius: '4px',
                          border: '1px solid var(--neutral-mid)',
                          fontSize: '12px',
                        }}
                      >
                        <option value="contains">Contains</option>
                        <option value="not-contains">Not contains</option>
                        <option value="equals">Equals</option>
                        <option value="not-equals">Not equals</option>
                        <option value="starts-with">Starts with</option>
                        <option value="ends-with">Ends with</option>
                        <option value="gt">Greater than</option>
                        <option value="lt">Less than</option>
                      </select>
                      {shouldUseSelect ? (
                        <select
                          value={row.value}
                          onChange={(e) => {
                            const next = filterRows.map((item) =>
                              item.id === row.id ? { ...item, value: e.target.value } : item
                            );
                            setFilterRows(next);
                          }}
                          style={{
                            flex: 1,
                            padding: '6px',
                            borderRadius: '4px',
                            border: '1px solid var(--neutral-mid)',
                            fontSize: '12px',
                          }}
                        >
                          <option value="">-- Select {row.field} --</option>
                          {fieldValues.map((val) => (
                            <option key={val} value={val}>
                              {val}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <input
                          type="text"
                          value={row.value}
                          onChange={(e) => {
                            const next = filterRows.map((item) =>
                              item.id === row.id ? { ...item, value: e.target.value } : item
                            );
                            setFilterRows(next);
                          }}
                          placeholder="Contains..."
                          style={{
                            flex: 1,
                            padding: '6px',
                            borderRadius: '4px',
                            border: '1px solid var(--neutral-mid)',
                            fontSize: '12px',
                          }}
                        />
                      )}
                      <button
                        onClick={() => {
                          const next = filterRows.filter((item) => item.id !== row.id);
                          setFilterRows(
                            next.length > 0
                              ? next
                              : [{ id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' }]
                          );
                        }}
                        className="button-secondary"
                        style={{ padding: '5px 8px', fontSize: '12px' }}
                        title="Remove filter"
                      >
                        Remove
                      </button>
                    </div>
                  );
                })}
                <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap' }}>
                  <button
                    onClick={() => {
                      setFilterRows((prev) => [
                        ...prev,
                        { id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' },
                      ]);
                    }}
                    className="button-secondary"
                    style={{ padding: '6px 10px', fontSize: '12px' }}
                  >
                    + Add filter
                  </button>
                  <button
                    onClick={() => {
                      const name = filterPresetName.trim();
                      if (!name) return;
                      const data = JSON.stringify({
                        searchText,
                        searchExact,
                        filterLogic,
                        filterRows,
                      });
                      const next = filterPresets.filter((preset) => preset.name !== name);
                      next.unshift({ name, data });
                      setFilterPresetName('');
                      setSelectedPreset(name);
                      savePresets(next);
                    }}
                    className="button-secondary"
                    style={{ padding: '6px 10px', fontSize: '12px' }}
                  >
                    Save preset
                  </button>
                  <button
                    onClick={() => {
                      setSearchText('');
                      setSearchExact(false);
                      setFilterLogic('and');
                      setFilterRows([{ id: `filter-${Date.now()}`, field: 'type', value: '', comparator: 'contains' }]);
                    }}
                    className="button-secondary"
                    style={{ padding: '6px 10px', fontSize: '12px' }}
                  >
                    Clear filters
                  </button>
                </div>
                <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', alignItems: 'center' }}>
                  <input
                    type="text"
                    value={filterPresetName}
                    onChange={(e) => setFilterPresetName(e.target.value)}
                    placeholder="Preset name"
                    style={{
                      flex: '0 0 180px',
                      padding: '6px',
                      borderRadius: '4px',
                      border: '1px solid var(--neutral-mid)',
                      fontSize: '12px',
                    }}
                  />
                  <select
                    value={selectedPreset}
                    onChange={(e) => {
                      const value = e.target.value;
                      setSelectedPreset(value);
                      const preset = filterPresets.find((item) => item.name === value);
                      if (preset) {
                        applyPreset(preset.data);
                      }
                    }}
                    style={{
                      flex: '0 0 220px',
                      padding: '6px',
                      borderRadius: '4px',
                      border: '1px solid var(--neutral-mid)',
                      fontSize: '12px',
                    }}
                  >
                    <option value="">Saved presets</option>
                    {filterPresets.map((preset) => (
                      <option key={preset.name} value={preset.name}>
                        {preset.name}
                      </option>
                    ))}
                  </select>
                  <button
                    onClick={() => {
                      if (!selectedPreset) return;
                      const next = filterPresets.filter((preset) => preset.name !== selectedPreset);
                      setSelectedPreset('');
                      savePresets(next);
                    }}
                    className="button-secondary"
                    style={{ padding: '6px 10px', fontSize: '12px' }}
                  >
                    Delete preset
                  </button>
                </div>
              </div>
            </div>
          )}
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
            onSave={handleCreateArtifact}
            onCancel={() => {
              setIsCreating(false);
              setError('');
            }}
          />
        )}

        <ArtifactList
          artifacts={filteredArtifacts}
          allArtifacts={artifacts}
          selectedId={selectedArtifactId || undefined}
          onSelect={handleSelectArtifact}
          onReorder={handleReorderArtifact}
          onContextMenuAction={handleArtifactContextMenu}
          defaultCollapsed
          collapseAllTrigger={collapseAllToken}
          expandAllTrigger={expandAllToken}
          readOnly={isBaselineView}
        />      </div>

      {/* Resize handle for left column */}
      <div
        onMouseDown={() => setIsResizing('left')}
        style={{
          width: '10px',
          cursor: 'col-resize',
          backgroundColor: isResizing === 'left' ? 'var(--accent)' : 'transparent',
          borderLeft: '1px solid var(--border)',
          borderRight: '1px solid var(--border)',
          transition: 'background-color 0.2s',
          flexShrink: 0,
        }}
        onMouseEnter={(e) => {
          if (!isResizing) {
            e.currentTarget.style.backgroundColor = 'var(--neutral-soft)';
          }
        }}
        onMouseLeave={(e) => {
          if (!isResizing) {
            e.currentTarget.style.backgroundColor = 'transparent';
          }
        }}
      />

      <div style={{ display: 'flex', flex: 1, gap: '0', minWidth: 0, overflow: 'hidden' }}>
        <div style={{ flex: 1, minWidth: 0, overflow: 'auto', display: 'flex', flexDirection: 'column', paddingLeft: '10px', paddingRight: selectedArtifact ? '5px' : '10px' }}>
        {!isBaselineView && isEditing && editingArtifact && (
          <ArtifactEditor
            artifact={editingArtifact}
            artifacts={artifacts}
            onSave={handleUpdateArtifact}
            onCancel={() => {
              setIsEditing(false);
              setEditingArtifact(undefined);
            }}
            attachments={attachments}
            onUploadAttachment={handleUploadAttachment}
            onDeleteAttachment={handleDeleteAttachment}
            isUploadLoading={uploadingAttachmentId === editingArtifact.id}
            links={allLinks}
            onCreateLink={handleCreateLink}
            onDeleteLink={(linkId) => {
              // Link deletion is now handled in edit mode
              // The backend will process actual link deletion
            }}
          />
        )}

        {selectedArtifact && !isEditing && (
          <>
            <ArtifactHeader
              artifact={selectedArtifact}
              onEdit={handleEditArtifact}
              onDelete={handleDeleteArtifact}
              onRestore={(restored) => {
                updateArtifact(restored);
                handleSelectArtifact(restored.id);
              }}
              previewVersion={previewVersion}
              onPreviewChange={setPreviewVersion}
            />
            <ArtifactDetails 
              artifact={selectedArtifact} 
              links={activeLinks} 
              artifacts={activeArtifacts}
              attachments={detailAttachments}
              onDeleteAttachment={isBaselineView ? undefined : handleDeleteAttachment}
              onSelectArtifact={handleSelectArtifact}
              previewVersion={previewVersion}
              onClosePreview={() => setPreviewVersion(null)}
            />
          </>
        )}

        {!selectedArtifact && !isEditing && (
          <div className="card">
            <h3>No Artifact Selected</h3>
            <p>Select an artifact from the list to view details.</p>
          </div>
        )}
        </div>

        {selectedArtifact && (
          <>
            {/* Resize handle for right column */}
            <div
              onMouseDown={() => setIsResizing('right')}
              style={{
                width: '10px',
                cursor: 'col-resize',
                backgroundColor: isResizing === 'right' ? 'var(--accent)' : 'transparent',
                borderLeft: '1px solid var(--border)',
                borderRight: '1px solid var(--border)',
                transition: 'background-color 0.2s',
                flexShrink: 0,
              }}
              onMouseEnter={(e) => {
                if (!isResizing) {
                  e.currentTarget.style.backgroundColor = 'var(--neutral-soft)';
                }
              }}
              onMouseLeave={(e) => {
                if (!isResizing) {
                  e.currentTarget.style.backgroundColor = 'transparent';
                }
              }}
            />
            <div style={{ width: `${rightColumnWidth}px`, minWidth: '250px', maxWidth: '600px', overflow: 'hidden' }}>
              <ChatterPanel
                key={`chatter-${selectedArtifact.id}-v${selectedArtifact.version}`}
                artifactId={selectedArtifact.id}
                isOpen={true}
                onToggle={() => {}}
              />
            </div>
          </>
        )}
      </div>
      </div>
    </>
  );
};

export default ModuleView;
