import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  artifactAPI,
  baselineAPI,
  guidedAPI,
  productProfileAPI,
  Artifact,
  ProductProfile,
} from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { useAppStore } from '../state/store';
import { StepShell } from '../components/wizard/StepShell';
import { ErrorBanner, HelpLink, useConfirm } from '../components/ui';
import { useHelpContext } from '../hooks/useHelp';
import { GuidedChatPanel, GuidedChatPanelHandle, CopilotSuggestion } from '../components/wizard/GuidedChatPanel';
import {
  ASSISTANT_EDITS_FEATURE,
  GATED_REASON,
  applySuggestionsToProject,
  isProjectEditKind,
} from '../components/wizard/applySuggestion';
import { useFeature } from '../hooks/useFeature';
import { useViewport } from '../hooks/useViewport';
import { useVisualViewport } from '../hooks/useVisualViewport';
import {
  PersonaEntry,
  NeedEntry,
  ReqEntry,
  NfrEntry,
  HazardEntry,
  FUNCTIONAL_TEST_CATEGORY,
  SUBSECTION_SPECS,
  subSectionKey,
} from '../components/wizard/wizardEntries';
import { SuggestionDraft, applySuggestionToDraft } from '../components/wizard/wizardSuggestions';
import {
  DraftSpec,
  EntryStep,
  HAZARD_STEP,
  NEED_STEP,
  NFR_STEP,
  PERSONA_STEP,
  REQUIREMENT_STEP,
  REVIEW_STEP,
  SECTION_SPECS,
  STEP_LABELS,
  STUB_STEP,
  isSkippable,
  nudgeEvent,
  planEntryDrafts,
  STEP_HELP_KEYS,
} from './guidedWizard/steps';
import {
  WizardAnswers,
  readWizardAnswers,
  serializeWizardAnswers,
  withEntryStepAnswers,
  withStubAnswers,
} from './guidedWizard/wizardAnswers';
import { useGuidedSession } from './guidedWizard/useGuidedSession';
import { renderFramingStep } from './guidedWizard/FramingStep';
import { renderPersonaStep } from './guidedWizard/PersonaStep';
import { renderNeedsStep } from './guidedWizard/NeedsStep';
import { renderRequirementsStep } from './guidedWizard/RequirementsStep';
import { renderNfrStep } from './guidedWizard/NfrStep';
import { renderHazardStep } from './guidedWizard/HazardStep';
import { renderStubsStep } from './guidedWizard/StubsStep';
import { renderReviewStep } from './guidedWizard/ReviewStep';
import { renderStartScreen } from './guidedWizard/StartScreen';
import { renderCommittedScreen } from './guidedWizard/CommittedScreen';
import { AssistantSheet } from './guidedWizard/AssistantSheet';

// Entry types (PersonaEntry, NeedEntry, ...) live in wizardEntries.ts. Every
// entry carries a stable client-generated id, and cross-references (need ->
// persona, requirement -> need) are by id, never by array index. The steps
// are data in guidedWizard/steps.ts, and the saved answers are typed and
// written by guidedWizard/wizardAnswers.ts. The session is guidedWizard/
// useGuidedSession.ts; each step, the start and committed screens and the
// phone's assistant sheet are views in guidedWizard/, given the state they
// read and its setters. The form's state and its handlers stay here.

// ---------------------------------------------------------------------------

export const GuidedWizard: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;
  const confirm = useConfirm();

  const chatRef = useRef<GuidedChatPanelHandle>(null);
  // Phones and tablets: the assistant is a bottom sheet behind a floating
  // button instead of a column beside the steps. It stays mounted while the
  // sheet is closed so its conversation (and the ref the steps nudge) live on.
  const viewport = useViewport();
  const compact = viewport.isCompact;
  const [assistantOpen, setAssistantOpen] = useState(false);
  // The sheet sits above the on-screen keyboard: its height and bottom
  // offset follow the visual viewport, since position: fixed anchors to the
  // layout viewport, which the keyboard does not shrink on iOS.
  const visual = useVisualViewport();
  const [baselineCreated, setBaselineCreated] = useState(false);

  // Step 1
  const [vision, setVision] = useState('');
  const [problem, setProblem] = useState('');
  const [targetUsers, setTargetUsers] = useState('');
  // Step 2
  const [personas, setPersonas] = useState<PersonaEntry[]>([]);
  // Step 3
  const [needs, setNeeds] = useState<NeedEntry[]>([]);
  // Step 4
  const [requirements, setRequirements] = useState<ReqEntry[]>([]);
  // Step 5
  const [nfrs, setNfrs] = useState<NfrEntry[]>([]);
  const [openNfrCategories, setOpenNfrCategories] = useState<Record<string, boolean>>({});
  // Step 6
  const [hazards, setHazards] = useState<HazardEntry[]>([]);
  const [openHazardCategories, setOpenHazardCategories] = useState<Record<string, boolean>>({});
  // Heading artifacts materialized entries live under, keyed by section
  // ("personas", …) or hazard sub-section ("hazards:Safety"). Persisted in
  // the session answers so re-runs reuse the same headings.
  const [sectionIds, setSectionIds] = useState<Record<string, string>>({});
  // Step 7 — keyed by requirement artifact id
  const [stubSelected, setStubSelected] = useState<Record<string, boolean>>({});
  const [stubCreated, setStubCreated] = useState<Record<string, string>>({});
  // Step 8
  const [draftArtifacts, setDraftArtifacts] = useState<Artifact[]>([]);
  const [draftsLoading, setDraftsLoading] = useState(false);
  // Copilot suggestions already applied ("<messageId>:<segmentIndex>" keys),
  // persisted in the session answers so Apply buttons stay disabled after a
  // remount and a re-click can never duplicate entries.
  const [appliedSuggestions, setAppliedSuggestions] = useState<Record<string, boolean>>({});
  // Whether the assistant's project cards (a new artifact by place, an
  // edit, a move) may be applied from here; gated by channel like any other
  // new feature.
  const projectEditsOn = useFeature(ASSISTANT_EDITS_FEATURE);

  const hydrateFromAnswers = useCallback((answers: Record<string, any>) => {
    // Entries are normalized to id-based ones; sessions saved before stable
    // ids existed (index-based persona_index/need_index references) are
    // migrated here.
    const read = readWizardAnswers(answers);
    setVision(read.vision);
    setProblem(read.problem);
    setTargetUsers(read.targetUsers);
    setPersonas(read.entries.personas);
    setNeeds(read.entries.needs);
    setRequirements(read.entries.requirements);
    setNfrs(read.entries.nfrs);
    setHazards(read.entries.hazards);
    setAppliedSuggestions(read.applied);
    setStubSelected(read.stubs.selected);
    setStubCreated(read.stubs.created);
    setSectionIds(read.sectionIds);
  }, []);

  const {
    session,
    setSession,
    latestCommitted,
    setLatestCommitted,
    loading,
    error,
    setError,
    busy,
    setBusy,
    step,
    setStep,
    maxReached,
    committed,
    setCommitted,
    startSession,
    modifySession,
    saveStep,
    goTo,
    goBack: handleBack,
    abandonSession,
    handleCommit,
  } = useGuidedSession({
    projectId,
    hydrateFromAnswers,
    fillFraming: (profile: ProductProfile) => {
      setVision(profile.vision || '');
      setProblem(profile.problem_statement || '');
      setTargetUsers(profile.target_users || '');
    },
  });

  const buildAnswers = (): WizardAnswers =>
    serializeWizardAnswers(
      {
        vision,
        problem,
        targetUsers,
        personas,
        needs,
        requirements,
        nfrs,
        hazards,
        openNfr: openNfrCategories,
        openHazard: openHazardCategories,
      },
      appliedSuggestions,
      session,
      { selected: stubSelected, created: stubCreated }
    );

  // Apply a batch of suggestions in order against one working copy, then
  // commit every touched section in a single pass and persist the result
  // (entries + applied-suggestion keys) to the session, so applied state
  // survives navigating away and back. Returns one result per suggestion
  // (null = applied, string = reason it was skipped).
  const handleApplySuggestions = async (
    items: { suggestion: CopilotSuggestion; key: string }[]
  ): Promise<(string | null)[]> => {
    // A card that changes the project — an artifact by place, an edit, a
    // move — goes to the project, exactly as it does from the notes panel:
    // the wizard sits on top of a project and its locked entries are
    // artifacts. Wizard-shaped cards fill the form as before. One result per
    // card, in the order given.
    const projectItems = items.filter((i) => isProjectEditKind(i.suggestion.kind) && !appliedSuggestions[i.key]);
    const projectResults = new Map<string, string | null>();
    if (projectItems.length > 0) {
      if (!projectId) {
        projectItems.forEach((i) => projectResults.set(i.key, 'No project is open.'));
      } else if (!projectEditsOn) {
        projectItems.forEach((i) => projectResults.set(i.key, GATED_REASON));
      } else {
        let artifacts: Artifact[] = [];
        try {
          artifacts = (await artifactAPI.list(projectId)).data || [];
        } catch {
          projectItems.forEach((i) => projectResults.set(i.key, 'The project could not be read, so nothing was changed.'));
        }
        if (projectResults.size === 0) {
          const out = await applySuggestionsToProject(
            { projectId, artifacts, onChanged: () => void loadDrafts() },
            projectItems
          );
          projectItems.forEach((i, n) => projectResults.set(i.key, out[n]));
        }
      }
    }
    const d: SuggestionDraft = {
      vision,
      problem,
      targetUsers,
      personas: personas.map((p) => ({ ...p })),
      needs: needs.map((n) => ({ ...n })),
      requirements: requirements.map((r) => ({ ...r })),
      nfrs: nfrs.map((n) => ({ ...n })),
      hazards: hazards.map((h) => ({ ...h })),
      openNfr: { ...openNfrCategories },
      openHazard: { ...openHazardCategories },
    };
    // A suggestion already applied in this session is a no-op success —
    // re-clicking (or a stale button after a remount) can never duplicate.
    const results = items.map(({ suggestion, key }) => {
      if (appliedSuggestions[key]) return null;
      // null is a project card applied; only a card with no result at all
      // failed to apply.
      if (isProjectEditKind(suggestion.kind)) {
        return projectResults.has(key) ? (projectResults.get(key) as string | null) : 'The change could not be applied.';
      }
      return applySuggestionToDraft(d, suggestion);
    });
    setVision(d.vision);
    setProblem(d.problem);
    setTargetUsers(d.targetUsers);
    setPersonas(d.personas);
    setNeeds(d.needs);
    setRequirements(d.requirements);
    setNfrs(d.nfrs);
    setHazards(d.hazards);
    setOpenNfrCategories(d.openNfr);
    setOpenHazardCategories(d.openHazard);
    const nextApplied = { ...appliedSuggestions };
    let newlyApplied = false;
    results.forEach((r, i) => {
      if (r === null && !nextApplied[items[i].key]) {
        nextApplied[items[i].key] = true;
        newlyApplied = true;
      }
    });
    if (newlyApplied) {
      setAppliedSuggestions(nextApplied);
      if (session) {
        const answers = serializeWizardAnswers(d, nextApplied, session, { selected: stubSelected, created: stubCreated });
        // Keep the session's saved step: don't regress current_step when a
        // suggestion is applied while revisiting an earlier step.
        const stepToSave = Math.max(session.current_step || 0, step) || 1;
        guidedAPI
          .saveStep(session.id, stepToSave, answers)
          .then((res) => setSession(res.data))
          .catch(() => {
            setError('Suggestion applied in the wizard, but saving the session failed — it may not survive a reload.');
          });
      }
    }
    return results;
  };

  const persist = (nextStep: number, answersOverride?: WizardAnswers) =>
    saveStep(nextStep, answersOverride || buildAnswers());

  const loadDrafts = useCallback(async () => {
    if (!projectId) return;
    setDraftsLoading(true);
    try {
      const res = await artifactAPI.list(projectId);
      setDraftArtifacts((res.data || []).filter((a) => a.attributes?.status === 'draft'));
    } catch (err: any) {
      setError(`Failed to load draft artifacts: ${apiErrorMessage(err)}`);
    } finally {
      setDraftsLoading(false);
    }
    // setError is useGuidedSession's state setter, which React keeps stable,
    // so the array stays [projectId], as it was in this component.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  useEffect(() => {
    if (step === REVIEW_STEP && session) loadDrafts();
  }, [step, session, loadDrafts]);

  // The help panel follows the step: the start page, then each step's topic.
  const helpKey = !session ? 'guided.start' : committed ? 'guided.step.review' : STEP_HELP_KEYS[step - 1];
  useHelpContext(helpKey);

  // -------------------------------------------------------------------------
  // Per-step Next handlers (materialize + save + advance)
  // -------------------------------------------------------------------------

  // Create any missing section headings and return the updated key→artifact
  // map. Sub-sections ("nfrs:Performance", "hazards:Safety") nest under their
  // step's heading, which is created on demand too.
  const ensureSections = async (keys: string[]): Promise<Record<string, string>> => {
    const map = { ...sectionIds };
    if (!session) return map;
    const create = async (title: string, sort: number, parentId?: string): Promise<string> => {
      const res = await guidedAPI.materializeDrafts(session.id, [
        { type: 'heading', title, body: '', sort_order: sort, ...(parentId ? { parent_id: parentId } : {}) },
      ]);
      return (res.data.artifact_ids || [])[0];
    };
    for (const key of keys) {
      if (map[key]) continue;
      const colon = key.indexOf(':');
      const parentKey = colon === -1 ? '' : key.slice(0, colon);
      const spec = SUBSECTION_SPECS[parentKey];
      if (spec) {
        const cat = key.slice(colon + 1);
        if (!map[parentKey]) {
          map[parentKey] = await create(SECTION_SPECS[parentKey].title, SECTION_SPECS[parentKey].sort);
        }
        map[key] = await create(spec.title(cat), 10 * (spec.categories.indexOf(cat) + 1), map[parentKey]);
      } else if (SECTION_SPECS[key]) {
        map[key] = await create(SECTION_SPECS[key].title, SECTION_SPECS[key].sort);
      }
    }
    setSectionIds(map);
    return map;
  };

  // Next on an entry step (2 to 6): materialise each entry that has no
  // artifact yet under its section, then save the step's entries and their
  // ids over the answers. The headings come first, one call each, then the
  // drafts in one call.
  const saveEntryStep = async <E extends { artifact_id?: string }>(
    d: EntryStep<E>,
    entries: E[],
    setEntries: (next: E[]) => void
  ) => {
    if (!session) return;
    const updated = entries.slice();
    const plan = planEntryDrafts(d, updated, { personas, needs });
    let sections = sectionIds;
    if (plan.drafts.length > 0) {
      sections = await ensureSections(Array.from(new Set(plan.sectionKeys)));
      plan.drafts.forEach((draft, j) => (draft.parent_id = sections[plan.sectionKeys[j]]));
      const res = await guidedAPI.materializeDrafts(session.id, plan.drafts);
      (res.data.artifact_ids || []).forEach((id, j) => {
        const i = plan.entryIndex[j];
        if (i !== undefined) updated[i] = { ...updated[i], artifact_id: id };
      });
      setEntries(updated);
    }
    await persist(d.step + 1, withEntryStepAnswers(buildAnswers(), d, updated, sections));
    goTo(d.step + 1);
  };

  const handleNext = async () => {
    if (!session || !projectId) return;
    setBusy(true);
    setError('');
    // Nudge the copilot immediately — before the save round-trips — so its
    // thinking indicator appears the moment the user advances.
    if (step <= 7) {
      chatRef.current?.nudge(step + 1, nudgeEvent('saved', step));
    }
    try {
      if (step === 1) {
        await productProfileAPI.update(projectId, {
          vision,
          problem_statement: problem,
          target_users: targetUsers,
        });
        await persist(2);
        goTo(2);
      } else if (step === PERSONA_STEP.step) {
        await saveEntryStep(PERSONA_STEP, personas, setPersonas);
      } else if (step === NEED_STEP.step) {
        await saveEntryStep(NEED_STEP, needs, setNeeds);
      } else if (step === REQUIREMENT_STEP.step) {
        await saveEntryStep(REQUIREMENT_STEP, requirements, setRequirements);
      } else if (step === NFR_STEP.step) {
        await saveEntryStep(NFR_STEP, nfrs, setNfrs);
      } else if (step === HAZARD_STEP.step) {
        await saveEntryStep(HAZARD_STEP, hazards, setHazards);
      } else if (step === STUB_STEP.step) {
        const candidates = testCandidates();
        const toCreate = candidates.filter(
          (c) => (stubSelected[c.id] ?? true) && !stubCreated[c.id]
        );
        let createdMap = { ...stubCreated };
        let sections = sectionIds;
        if (toCreate.length > 0) {
          // A stub sits beside the kind of requirement it verifies, so the
          // verification module reads in the same shape as the requirements.
          const keys = toCreate.map((c) =>
            subSectionKey('tests', c.category || FUNCTIONAL_TEST_CATEGORY)
          );
          sections = await ensureSections(Array.from(new Set(keys)));
          const drafts: DraftSpec[] = toCreate.map((c, j) => ({
            type: STUB_STEP.artifactType,
            ...STUB_STEP.template(c),
            parent_id: sections[keys[j]],
            links: [{ type: 'verifies', to_id: c.id }],
          }));
          const res = await guidedAPI.materializeDrafts(session.id, drafts);
          (res.data.artifact_ids || []).forEach((id, j) => {
            createdMap = { ...createdMap, [toCreate[j].id]: id };
          });
          setStubCreated(createdMap);
        }
        await persist(8, withStubAnswers(buildAnswers(), sections, { selected: stubSelected, created: createdMap }));
        goTo(8);
      }
    } catch (err: any) {
      setError(`Failed to save step: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const handleSkip = async () => {
    if (!session) return;
    setBusy(true);
    setError('');
    chatRef.current?.nudge(step + 1, nudgeEvent('skipped', step));
    try {
      await persist(step + 1);
      goTo(step + 1);
    } catch (err: any) {
      setError(`Failed to skip step: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  // Escape hatch: drop the current in-progress session and return to the
  // Start/Modify landing. Materialized drafts stay in the project.
  const handleAbandon = async () => {
    if (!session) return;
    const ok = await confirm({
      title: 'Abandon guided session',
      message:
        'Abandon this guided session? Draft artifacts already created are kept, but unsaved entries are discarded.',
      confirmLabel: 'Abandon',
      danger: true,
    });
    if (!ok) return;
    await abandonSession();
  };

  const handleCreateBaseline = async () => {
    if (!projectId) {
      // Returning silently here is what made this button look broken: the
      // click produced no baseline, no error and no spinner, so there was
      // nothing to tell a person whether it had worked.
      setError('No project is open yet, so there is nothing to baseline.');
      return;
    }
    setBusy(true);
    try {
      await baselineAPI.create(projectId, 'Initial requirements');
      setBaselineCreated(true);
      setError('');
    } catch (err: any) {
      setError(`Failed to create baseline: ${apiErrorMessage(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const discardDraft = async (artifact: Artifact) => {
    try {
      await artifactAPI.delete(artifact.id);
      setDraftArtifacts(draftArtifacts.filter((a) => a.id !== artifact.id));
    } catch (err: any) {
      setError(`Failed to discard draft: ${apiErrorMessage(err)}`);
    }
  };

  /**
   * Requirements (functional + NFR) with verification method 'test' that were
   * materialized. An NFR's category rides along as its own field so the picker
   * can show which quality attribute a line belongs to without the stub title
   * inheriting it — the title has to match the requirement it verifies.
   */
  const testCandidates = (): { id: string; title: string; category?: string }[] => {
    const out: { id: string; title: string; category?: string }[] = [];
    requirements.forEach((r) => {
      if (r.artifact_id && r.verification_method === 'test' && r.text.trim()) {
        out.push({ id: r.artifact_id, title: r.text.trim() });
      }
    });
    nfrs.forEach((n) => {
      if (n.artifact_id && n.verification_method === 'test' && n.text.trim()) {
        out.push({ id: n.artifact_id, title: n.text.trim(), category: n.category });
      }
    });
    return out;
  };

  // -------------------------------------------------------------------------
  // Rendering
  // -------------------------------------------------------------------------

  if (loading) {
    return <div style={{ padding: 32, color: 'var(--text-muted)' }}>Loading guided definition…</div>;
  }

  if (!session) {
    return renderStartScreen({ latestCommitted, error, setError, busy, modifySession, startSession });
  }

  if (committed) {
    return renderCommittedScreen({
      baselineCreated,
      error,
      setError,
      busy,
      handleCreateBaseline,
      onModify: () => {
        const committedSession = session;
        setCommitted(false);
        setLatestCommitted(committedSession);
        setSession(null);
        if (committedSession) modifySession(committedSession);
      },
    });
  }

  const renderStepContent = () => {
    switch (step) {
      case 1:
        return renderFramingStep({ vision, problem, targetUsers, setVision, setProblem, setTargetUsers });
      case 2:
        return renderPersonaStep({ personas, setPersonas });
      case 3:
        return renderNeedsStep({ personas, needs, setNeeds });
      case 4:
        return renderRequirementsStep({ personas, needs, requirements, setRequirements });
      case 5:
        return renderNfrStep({ nfrs, setNfrs, openNfrCategories, setOpenNfrCategories });
      case 6:
        return renderHazardStep({ hazards, setHazards, openHazardCategories, setOpenHazardCategories });
      case 7:
        return renderStubsStep({ candidates: testCandidates(), stubSelected, stubCreated, setStubSelected });
      case 8:
        return renderReviewStep({ draftArtifacts, draftsLoading, busy, discardDraft, handleBack, handleCommit });
      default:
        return null;
    }
  };

  return (
    <div style={{ padding: compact ? 12 : 24, maxWidth: 1560, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16, gap: 10, flexWrap: 'wrap' }}>
        <h2 style={{ color: 'var(--text)', margin: 0, fontSize: compact ? 20 : undefined }}>
          Guided requirements definition
          <HelpLink helpKey={helpKey || 'guided'} label="this step" />
        </h2>
        <button
          onClick={handleAbandon}
          disabled={busy}
          title="Drop this session and return to the start page (created drafts are kept)"
          style={{
            background: 'none',
            border: '1px solid var(--danger)',
            color: 'var(--danger)',
            borderRadius: 4,
            padding: '6px 12px',
            fontSize: 12,
            cursor: 'pointer',
            width: 'auto',
            whiteSpace: 'nowrap',
          }}
        >
          Abandon session
        </button>
      </div>
      <ErrorBanner message={error} onDismiss={() => setError('')} style={{ marginBottom: 16 }} />
      <div style={{ display: 'flex', gap: 20, alignItems: 'flex-start' }}>
        <div style={{ flex: 1, minWidth: 0, paddingBottom: compact ? 72 : 0 }}>
          <StepShell
            steps={STEP_LABELS}
            current={step}
            maxReached={maxReached}
            compact={compact}
            onSelectStep={(s) => setStep(s)}
            onBack={handleBack}
            onNext={handleNext}
            backDisabled={step === 1}
            busy={busy}
            hideNav={step === REVIEW_STEP}
            extraAction={
              isSkippable(step) ? (
                <button className="button-secondary" onClick={handleSkip} disabled={busy}>
                  Skip
                </button>
              ) : undefined
            }
          >
            {renderStepContent()}
          </StepShell>
        </div>
        {compact ? (
          <AssistantSheet
            assistantOpen={assistantOpen}
            setAssistantOpen={setAssistantOpen}
            visual={visual}
            chatRef={chatRef}
            sessionId={session.id}
            step={step}
            getState={buildAnswers}
            applied={appliedSuggestions}
            onApplySuggestions={handleApplySuggestions}
          />
        ) : (
          <GuidedChatPanel
            ref={chatRef}
            sessionId={session.id}
            step={step}
            getState={buildAnswers}
            applied={appliedSuggestions}
            onApplySuggestions={handleApplySuggestions}
          />
        )}
      </div>
    </div>
  );
};
