import React, { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  evidenceAPI,
  EvidenceBundle,
  EvidenceBundleInput,
  EvidenceFile,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { ErrorBanner, Modal, useConfirm } from '../components/ui';
import { useViewport } from '../hooks/useViewport';
import { formatConditions, humanBytes, parseConditions } from '../utils/evidence';

/**
 * Evidence: the capture sessions behind physical and manual test results.
 *
 * A bundle lives here rather than inside a run because one session commonly
 * answers several test cases, and may still stand when the campaign is run
 * again. This view is the "what have we actually measured?" side; the citing
 * happens in the run.
 */

/** A capture date is a date, not a timestamp: nobody records the second a
 *  90-minute sweep began, and showing one implies a precision that is not
 *  there. */
const captureDate = (iso?: string | null): string =>
  iso ? new Date(iso).toLocaleDateString() : 'Date not recorded';

const emptyDraft: EvidenceBundleInput & { conditionsText: string } = {
  title: '',
  summary: '',
  captured_at: '',
  captured_by: '',
  conditionsText: '',
};

export const EvidenceView: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const { isCompact } = useViewport();
  const confirm = useConfirm();

  const [bundles, setBundles] = useState<EvidenceBundle[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState<EvidenceBundle | null>(null);
  const [creating, setCreating] = useState(false);
  const [open, setOpen] = useState<EvidenceBundle | null>(null);

  const load = useCallback(async () => {
    if (!projectId) return;
    try {
      const res = await evidenceAPI.list(projectId);
      setBundles(res.data || []);
      setError('');
    } catch (err: any) {
      setError(`Failed to load evidence: ${apiErrorMessage(err)}`);
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    load();
  }, [load]);

  const openBundle = async (bundle: EvidenceBundle) => {
    try {
      const res = await evidenceAPI.get(bundle.id);
      setOpen(res.data);
      setError('');
    } catch (err: any) {
      setError(`Failed to open ${bundle.ref}: ${apiErrorMessage(err)}`);
    }
  };

  const removeBundle = async (bundle: EvidenceBundle) => {
    // Deleting evidence is the one destructive act here, and the warning names
    // what depends on it: the results keep their recorded outcome but lose
    // what backed it.
    let detail = '';
    try {
      const res = await evidenceAPI.get(bundle.id);
      const cites = res.data.citations || [];
      if (cites.length) {
        const names = cites
          .map((c) => c.test_case_ref || c.test_case_title || 'a result')
          .join(', ');
        detail = ` ${cites.length} recorded ${
          cites.length === 1 ? 'result cites' : 'results cite'
        } it (${names}); they will keep their outcome but lose the evidence behind it.`;
      }
    } catch {
      // The confirmation is worth showing even if the count could not be read.
    }
    const ok = await confirm({
      title: `Delete ${bundle.ref}`,
      message: `Delete "${bundle.title}" and its files?${detail} This cannot be undone.`,
      confirmLabel: 'Delete forever',
      danger: true,
    });
    if (!ok) return;
    try {
      await evidenceAPI.remove(bundle.id);
      setOpen(null);
      await load();
    } catch (err: any) {
      setError(`Failed to delete ${bundle.ref}: ${apiErrorMessage(err)}`);
    }
  };

  if (!projectId) return null;

  return (
    <div style={{ padding: isCompact ? 12 : 24 }}>
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: 12,
          alignItems: 'baseline',
          justifyContent: 'space-between',
          marginBottom: 4,
        }}
      >
        <h2 style={{ margin: 0 }}>Evidence</h2>
        <button className="button" onClick={() => setCreating(true)}>
          Record a capture
        </button>
      </div>
      <p style={{ color: 'var(--text-muted)', marginTop: 4, marginBottom: 16, maxWidth: 640 }}>
        What a physical or manual test produced. One capture can be cited by
        every test case it covers — record it once here, then cite it from each
        result in the run.
      </p>

      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />

      {loading ? (
        <p style={{ color: 'var(--text-muted)' }}>Loading…</p>
      ) : bundles.length === 0 ? (
        <div className="card" style={{ padding: 32, textAlign: 'center', color: 'var(--text-muted)' }}>
          <p style={{ marginTop: 0 }}>No evidence recorded yet.</p>
          <p style={{ margin: 0, fontSize: 14 }}>
            A capture can be a dataset, a photograph of the rig, or simply a
            written account of what was observed.
          </p>
        </div>
      ) : (
        <div style={{ display: 'grid', gap: 12 }}>
          {bundles.map((bundle) => (
            <div
              key={bundle.id}
              className="card"
              style={{ padding: 16, cursor: 'pointer' }}
              onClick={() => openBundle(bundle)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  openBundle(bundle);
                }
              }}
              role="button"
              tabIndex={0}
            >
              <div
                style={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  gap: 8,
                  alignItems: 'baseline',
                  justifyContent: 'space-between',
                }}
              >
                <div style={{ display: 'flex', gap: 8, alignItems: 'baseline', flexWrap: 'wrap' }}>
                  <code style={{ color: 'var(--text-muted)', fontSize: 13 }}>{bundle.ref}</code>
                  <strong>{bundle.title}</strong>
                </div>
                <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>
                  {captureDate(bundle.captured_at)}
                </span>
              </div>
              {bundle.summary && (
                <p
                  style={{
                    margin: '8px 0 0',
                    color: 'var(--text-muted)',
                    fontSize: 14,
                    whiteSpace: 'pre-wrap',
                  }}
                >
                  {bundle.summary.length > 220
                    ? `${bundle.summary.slice(0, 220)}…`
                    : bundle.summary}
                </p>
              )}
              <div
                style={{
                  marginTop: 10,
                  display: 'flex',
                  gap: 12,
                  flexWrap: 'wrap',
                  color: 'var(--text-muted)',
                  fontSize: 13,
                }}
              >
                <span>
                  {bundle.file_count === 0
                    ? 'Written account, no files'
                    : `${bundle.file_count} file${bundle.file_count === 1 ? '' : 's'} · ${humanBytes(
                        bundle.total_size
                      )}`}
                </span>
                {bundle.captured_by && <span>Captured by {bundle.captured_by}</span>}
              </div>
            </div>
          ))}
        </div>
      )}

      {(creating || editing) && (
        <BundleForm
          projectId={projectId}
          bundle={editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
          onSaved={async (saved) => {
            setCreating(false);
            setEditing(null);
            await load();
            await openBundle(saved);
          }}
        />
      )}

      {open && (
        <BundleDetail
          bundle={open}
          onClose={() => setOpen(null)}
          onEdit={() => {
            setEditing(open);
            setOpen(null);
          }}
          onDelete={() => removeBundle(open)}
          onChanged={async () => {
            const res = await evidenceAPI.get(open.id);
            setOpen(res.data);
            await load();
          }}
        />
      )}
    </div>
  );
};

interface BundleFormProps {
  projectId: string;
  bundle: EvidenceBundle | null;
  onClose: () => void;
  onSaved: (saved: EvidenceBundle) => void | Promise<void>;
}

const BundleForm: React.FC<BundleFormProps> = ({ projectId, bundle, onClose, onSaved }) => {
  const [draft, setDraft] = useState(() =>
    bundle
      ? {
          title: bundle.title,
          summary: bundle.summary,
          // <input type="date"> wants YYYY-MM-DD and nothing else.
          captured_at: bundle.captured_at ? bundle.captured_at.slice(0, 10) : '',
          captured_by: bundle.captured_by,
          conditionsText: formatConditions(bundle.conditions),
        }
      : { ...emptyDraft }
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const save = async () => {
    if (!draft.title.trim()) {
      setError('A title is required.');
      return;
    }
    setBusy(true);
    try {
      const payload: EvidenceBundleInput = {
        title: draft.title.trim(),
        summary: draft.summary?.trim() || '',
        // A date with no time is midnight UTC; the field means a day, so that
        // is exactly what it should mean.
        captured_at: draft.captured_at ? new Date(`${draft.captured_at}T00:00:00Z`).toISOString() : null,
        captured_by: draft.captured_by?.trim() || '',
        conditions: parseConditions(draft.conditionsText),
      };
      const res = bundle
        ? await evidenceAPI.update(bundle.id, payload)
        : await evidenceAPI.create(projectId, payload);
      await onSaved(res.data);
    } catch (err: any) {
      setError(`Failed to save: ${apiErrorMessage(err)}`);
      setBusy(false);
    }
  };

  const field = { width: '100%', marginBottom: 12 } as React.CSSProperties;

  return (
    <Modal title={bundle ? `Edit ${bundle.ref}` : 'Record a capture'} width={560} onClose={onClose}>
      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 12 }} />
      <label style={{ display: 'block', marginBottom: 4, fontSize: 13 }} htmlFor="evidence-title">
        What was done
      </label>
      <input
        id="evidence-title"
        className="input"
        style={field}
        value={draft.title}
        placeholder="Noise sweep, 90 minutes, all load conditions"
        onChange={(e) => setDraft({ ...draft, title: e.target.value })}
      />

      <label style={{ display: 'block', marginBottom: 4, fontSize: 13 }} htmlFor="evidence-summary">
        What was observed
      </label>
      <textarea
        id="evidence-summary"
        className="input"
        style={{ ...field, minHeight: 96, resize: 'vertical' }}
        value={draft.summary}
        placeholder="A written account. This alone is enough for an inspection or a demonstration — files are optional."
        onChange={(e) => setDraft({ ...draft, summary: e.target.value })}
      />

      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
        <div style={{ flex: '1 1 180px' }}>
          <label style={{ display: 'block', marginBottom: 4, fontSize: 13 }} htmlFor="evidence-date">
            When
          </label>
          <input
            id="evidence-date"
            className="input"
            type="date"
            style={field}
            value={draft.captured_at || ''}
            onChange={(e) => setDraft({ ...draft, captured_at: e.target.value })}
          />
        </div>
        <div style={{ flex: '1 1 180px' }}>
          <label style={{ display: 'block', marginBottom: 4, fontSize: 13 }} htmlFor="evidence-by">
            Who
          </label>
          <input
            id="evidence-by"
            className="input"
            style={field}
            value={draft.captured_by}
            placeholder="J. Patel, acoustics lab"
            onChange={(e) => setDraft({ ...draft, captured_by: e.target.value })}
          />
        </div>
      </div>

      <label style={{ display: 'block', marginBottom: 4, fontSize: 13 }} htmlFor="evidence-conditions">
        Conditions
      </label>
      <textarea
        id="evidence-conditions"
        className="input"
        style={{ ...field, minHeight: 72, resize: 'vertical', fontFamily: 'monospace', fontSize: 13 }}
        value={draft.conditionsText}
        placeholder={'rig: anechoic chamber 2\nambient: 21.5 C\ncalibrated: 2026-09-01'}
        onChange={(e) => setDraft({ ...draft, conditionsText: e.target.value })}
      />
      <p style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: -4 }}>
        One per line, as <code>name: value</code>.
      </p>

      <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
        <button className="button-secondary" onClick={onClose} disabled={busy}>
          Cancel
        </button>
        <button className="button" onClick={save} disabled={busy}>
          {busy ? 'Saving…' : 'Save'}
        </button>
      </div>
    </Modal>
  );
};

interface BundleDetailProps {
  bundle: EvidenceBundle;
  onClose: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onChanged: () => void | Promise<void>;
}

const BundleDetail: React.FC<BundleDetailProps> = ({
  bundle,
  onClose,
  onEdit,
  onDelete,
  onChanged,
}) => {
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');
  const confirm = useConfirm();

  const upload = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    setUploading(true);
    setError('');
    try {
      // One at a time: a rejection (too large, workspace full) then names the
      // file it was about instead of failing the whole selection anonymously.
      for (const file of Array.from(files)) {
        await evidenceAPI.uploadFile(bundle.id, file);
      }
      await onChanged();
    } catch (err: any) {
      setError(`Upload failed: ${apiErrorMessage(err)}`);
    } finally {
      setUploading(false);
    }
  };

  const removeFile = async (file: EvidenceFile) => {
    const ok = await confirm({
      title: 'Delete file',
      message: `Delete "${file.filename}" from ${bundle.ref}? This cannot be undone.`,
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    try {
      await evidenceAPI.deleteFile(file.id);
      await onChanged();
    } catch (err: any) {
      setError(`Failed to delete the file: ${apiErrorMessage(err)}`);
    }
  };

  const conditions = Object.entries(bundle.conditions || {});

  return (
    <Modal title={`${bundle.ref} — ${bundle.title}`} width={640} onClose={onClose}>
      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 12 }} />

      <div style={{ color: 'var(--text-muted)', fontSize: 13, marginBottom: 12 }}>
        {captureDate(bundle.captured_at)}
        {bundle.captured_by ? ` · ${bundle.captured_by}` : ''}
      </div>

      {bundle.summary && (
        <p style={{ whiteSpace: 'pre-wrap', marginTop: 0 }}>{bundle.summary}</p>
      )}

      {conditions.length > 0 && (
        <>
          <h4 style={{ marginBottom: 6 }}>Conditions</h4>
          <dl style={{ margin: 0, display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px' }}>
            {conditions.map(([k, v]) => (
              <React.Fragment key={k}>
                <dt style={{ color: 'var(--text-muted)', fontSize: 13 }}>{k}</dt>
                <dd style={{ margin: 0, fontSize: 13 }}>{String(v)}</dd>
              </React.Fragment>
            ))}
          </dl>
        </>
      )}

      <h4 style={{ marginBottom: 6, marginTop: 20 }}>Files</h4>
      {(bundle.files || []).length === 0 ? (
        <p style={{ color: 'var(--text-muted)', fontSize: 13, marginTop: 0 }}>
          None. A written account on its own is valid evidence for an
          inspection or a demonstration.
        </p>
      ) : (
        <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
          {(bundle.files || []).map((file) => (
            <li
              key={file.id}
              style={{
                display: 'flex',
                gap: 8,
                alignItems: 'center',
                flexWrap: 'wrap',
                padding: '6px 0',
                borderBottom: '1px solid var(--border)',
              }}
            >
              <a
                href={evidenceAPI.downloadUrl(file.id)}
                style={{ flex: '1 1 200px', wordBreak: 'break-all' }}
              >
                {file.filename}
              </a>
              <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>
                {humanBytes(file.file_size)}
              </span>
              <button
                className="button-secondary"
                style={{ minHeight: 32, padding: '4px 10px', fontSize: 12 }}
                onClick={() => removeFile(file)}
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
      )}

      <label
        className="button-secondary"
        style={{ display: 'inline-block', marginTop: 12, cursor: 'pointer' }}
      >
        {uploading ? 'Uploading…' : 'Add files'}
        <input
          type="file"
          multiple
          style={{ display: 'none' }}
          disabled={uploading}
          onChange={(e) => {
            upload(e.target.files);
            e.target.value = '';
          }}
        />
      </label>

      <h4 style={{ marginBottom: 6, marginTop: 20 }}>Cited by</h4>
      {(bundle.citations || []).length === 0 ? (
        <p style={{ color: 'var(--text-muted)', fontSize: 13, marginTop: 0 }}>
          Nothing yet. Cite this capture from a result in a test run.
        </p>
      ) : (
        <ul style={{ margin: 0, paddingLeft: 18, fontSize: 14 }}>
          {(bundle.citations || []).map((c) => (
            <li key={c.id} style={{ marginBottom: 4 }}>
              {c.test_case_ref ? <code>{c.test_case_ref}</code> : null} {c.test_case_title}
              {c.run_name ? (
                <span style={{ color: 'var(--text-muted)' }}> — {c.run_name}</span>
              ) : null}
              {c.note ? <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>{c.note}</div> : null}
            </li>
          ))}
        </ul>
      )}

      <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 20 }}>
        <button className="button-secondary" onClick={onDelete}>
          Delete capture
        </button>
        <button className="button-secondary" onClick={onEdit}>
          Edit
        </button>
        <button className="button" onClick={onClose}>
          Done
        </button>
      </div>
    </Modal>
  );
};
