import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import {
  artifactAPI,
  baselineAPI,
  guidedAPI,
  productProfileAPI,
  Artifact,
  GuidedSession,
} from '../api/client';
import { useAppStore } from '../state/store';
import { StepShell } from '../components/wizard/StepShell';
import { RepeatingCardList } from '../components/wizard/RepeatingCardList';
import { GuidedChatPanel, GuidedChatPanelHandle, CopilotSuggestion } from '../components/wizard/GuidedChatPanel';

// ---------------------------------------------------------------------------
// Types for wizard answers
// ---------------------------------------------------------------------------

interface DraftSpec {
  type: string;
  title: string;
  body: string;
  parent_id?: string;
  attributes?: Record<string, any>;
  links?: { type: string; to_id: string }[];
}

interface PersonaEntry {
  name: string;
  role: string;
  goals: string;
  pains: string;
  artifact_id?: string;
}

interface NeedEntry {
  persona_index: number;
  capability: string;
  outcome: string;
  artifact_id?: string;
}

interface ReqEntry {
  need_index: number;
  text: string;
  fit_criterion: string;
  verification_method: string;
  artifact_id?: string;
}

interface NfrEntry {
  category: string;
  text: string;
  fit_criterion: string;
  verification_method: string;
  artifact_id?: string;
}

interface HazardEntry {
  hazard: string;
  harm: string;
  severity: string;
  artifact_id?: string;
}

const STEP_LABELS = [
  'Product framing',
  'Personas',
  'User needs',
  'Requirements',
  'NFRs & constraints',
  'Hazards',
  'Verification stubs',
  'Review & commit',
];

const VERIFICATION_METHODS = ['inspection', 'analysis', 'demonstration', 'test'];
const NFR_CATEGORIES = ['Performance', 'Reliability', 'Usability', 'Security', 'Maintainability', 'Regulatory'];
const SEVERITIES = ['minor', 'moderate', 'serious', 'critical'];

const helpText: React.CSSProperties = { fontSize: 13, color: 'var(--text-muted)', marginBottom: 16, lineHeight: 1.5 };

// ---------------------------------------------------------------------------

export const GuidedWizard: React.FC = () => {
  const params = useParams<{ projectId: string }>();
  const storeProjectId = useAppStore((s) => s.projectId);
  const projectId = params.projectId || storeProjectId;

  const [session, setSession] = useState<GuidedSession | null>(null);
  const [latestCommitted, setLatestCommitted] = useState<GuidedSession | null>(null);
  const chatRef = useRef<GuidedChatPanelHandle>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [step, setStep] = useState(1);
  const [maxReached, setMaxReached] = useState(1);
  const [committed, setCommitted] = useState(false);
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
  // Step 7 — keyed by requirement artifact id
  const [stubSelected, setStubSelected] = useState<Record<string, boolean>>({});
  const [stubCreated, setStubCreated] = useState<Record<string, string>>({});
  // Step 8
  const [draftArtifacts, setDraftArtifacts] = useState<Artifact[]>([]);
  const [draftsLoading, setDraftsLoading] = useState(false);

  const hydrateFromAnswers = useCallback((answers: Record<string, any>) => {
    const s1 = answers.step_1 || {};
    setVision(s1.vision || '');
    setProblem(s1.problem_statement || '');
    setTargetUsers(s1.target_users || '');
    setPersonas((answers.step_2?.personas as PersonaEntry[]) || []);
    setNeeds((answers.step_3?.needs as NeedEntry[]) || []);
    setRequirements((answers.step_4?.requirements as ReqEntry[]) || []);
    setNfrs((answers.step_5?.nfrs as NfrEntry[]) || []);
    setHazards((answers.step_6?.hazards as HazardEntry[]) || []);
    setStubSelected((answers.step_7?.selected as Record<string, boolean>) || {});
    setStubCreated((answers.step_7?.created as Record<string, string>) || {});
  }, []);

  useEffect(() => {
    if (!projectId) return;
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const res = await guidedAPI.list(projectId);
        if (cancelled) return;
        const sessions = res.data || [];
        const active = sessions.find((s) => s.status === 'in-progress' || s.status === 'in_progress');
        // Sessions arrive newest first, so this is the most recent commit.
        setLatestCommitted(sessions.find((s) => s.status === 'committed') || null);
        if (active) {
          setSession(active);
          const current = Math.min(Math.max(active.current_step || 1, 1), 8);
          setStep(current);
          setMaxReached(current);
          hydrateFromAnswers(active.answers || {});
        }
        setError('');
      } catch (err: any) {
        if (!cancelled) setError(`Failed to load guided sessions: ${err.response?.data || err.message}`);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [projectId, hydrateFromAnswers]);

  const startSession = async () => {
    if (!projectId) return;
    setBusy(true);
    try {
      const res = await guidedAPI.start(projectId);
      setSession(res.data);
      setStep(1);
      setMaxReached(1);
      hydrateFromAnswers(res.data.answers || {});
      // Pre-fill step 1 from the product profile if available.
      try {
        const profile = await productProfileAPI.get(projectId);
        if (!res.data.answers?.step_1) {
          setVision(profile.data.vision || '');
          setProblem(profile.data.problem_statement || '');
          setTargetUsers(profile.data.target_users || '');
        }
      } catch {
        // profile optional
      }
      setError('');
    } catch (err: any) {
      setError(`Failed to start session: ${err.response?.data || err.message}`);
    } finally {
      setBusy(false);
    }
  };

  // Reopen a committed definition: a new in-progress session seeded with the
  // committed answers. Entries that already materialized keep their artifact
  // links (shown locked); anything added becomes a fresh draft to commit.
  const modifySession = async (from: GuidedSession) => {
    if (!projectId) return;
    setBusy(true);
    try {
      const res = await guidedAPI.start(projectId);
      const seeded = await guidedAPI.saveStep(res.data.id, 1, from.answers || {});
      setSession(seeded.data);
      hydrateFromAnswers(seeded.data.answers || {});
      setStep(1);
      setMaxReached(STEP_LABELS.length);
      setError('');
    } catch (err: any) {
      setError(`Failed to reopen definition: ${err.response?.data || err.message}`);
    } finally {
      setBusy(false);
    }
  };

  // Fuzzy-match a suggestion's "replaces" target against existing entries:
  // exact (case-insensitive) first, then substring either way.
  const matchEntry = <T,>(items: T[], key: (t: T) => string, target: string): number => {
    const t = target.trim().toLowerCase();
    if (!t) return -1;
    const exact = items.findIndex((it) => key(it).trim().toLowerCase() === t);
    if (exact >= 0) return exact;
    return items.findIndex((it) => {
      const k = key(it).trim().toLowerCase();
      return !!k && (k.includes(t) || t.includes(k));
    });
  };

  // Working copy of every suggestion-editable wizard section, so a batch of
  // suggestions applies in order against ONE snapshot (later entries can
  // reference earlier ones) and commits with a single state update per list.
  interface SuggestionDraft {
    vision: string;
    problem: string;
    targetUsers: string;
    personas: PersonaEntry[];
    needs: NeedEntry[];
    requirements: ReqEntry[];
    nfrs: NfrEntry[];
    hazards: HazardEntry[];
    openNfr: Record<string, boolean>;
  }

  // Insert or replace one copilot suggestion in the draft. Returns null on
  // success, or a human-readable reason it could not apply.
  const applySuggestionToDraft = (d: SuggestionDraft, s: CopilotSuggestion): string | null => {
    const verificationMethod = (v: any, fallback: string) =>
      VERIFICATION_METHODS.includes(String(v)) ? String(v) : fallback;

    switch (s.kind) {
      case 'framing': {
        const text = String(s.text || '').trim();
        if (!text) return 'The suggestion has no text.';
        switch (s.field) {
          case 'vision':
            d.vision = text;
            return null;
          case 'problem_statement':
            d.problem = text;
            return null;
          case 'target_users':
            d.targetUsers = text;
            return null;
          default:
            return `Unknown framing field "${s.field}".`;
        }
      }
      case 'persona': {
        const name = String(s.name || '').trim();
        if (!name) return 'The persona suggestion has no name.';
        if (s.replaces) {
          const i = matchEntry(d.personas, (p) => p.name, String(s.replaces));
          if (i < 0) return `No persona matching "${s.replaces}" to replace.`;
          if (d.personas[i].artifact_id) return 'That persona is already saved as an artifact and cannot be replaced here.';
          d.personas[i] = {
            ...d.personas[i],
            name,
            role: s.role !== undefined ? String(s.role) : d.personas[i].role,
            goals: s.goals !== undefined ? String(s.goals) : d.personas[i].goals,
            pains: s.pains !== undefined ? String(s.pains) : d.personas[i].pains,
          };
          return null;
        }
        d.personas.push({ name, role: String(s.role || ''), goals: String(s.goals || ''), pains: String(s.pains || '') });
        return null;
      }
      case 'need': {
        if (s.replaces) {
          const i = matchEntry(d.needs, (n) => n.capability, String(s.replaces));
          if (i < 0) return `No user need matching "${s.replaces}" to replace.`;
          if (d.needs[i].artifact_id) return 'That need is already saved as an artifact and cannot be replaced here.';
          let personaIndex = d.needs[i].persona_index;
          if (s.persona) {
            const hit = matchEntry(d.personas, (p) => p.name, String(s.persona));
            if (hit >= 0) personaIndex = hit;
          }
          d.needs[i] = {
            ...d.needs[i],
            persona_index: personaIndex,
            capability: s.capability !== undefined ? String(s.capability) : d.needs[i].capability,
            outcome: s.outcome !== undefined ? String(s.outcome) : d.needs[i].outcome,
          };
          return null;
        }
        const usable = d.personas.map((p, i) => ({ p, i })).filter(({ p }) => p.name.trim());
        if (usable.length === 0) return 'Add a persona first — user needs attach to a persona.';
        const wanted = String(s.persona || '').trim().toLowerCase();
        const hit = usable.find(({ p }) => p.name.trim().toLowerCase() === wanted);
        d.needs.push({
          persona_index: (hit || usable[0]).i,
          capability: String(s.capability || ''),
          outcome: String(s.outcome || ''),
        });
        return null;
      }
      case 'requirement': {
        if (s.replaces) {
          const i = matchEntry(d.requirements, (r) => r.text, String(s.replaces));
          if (i < 0) return `No requirement matching "${s.replaces}" to replace.`;
          if (d.requirements[i].artifact_id) return 'That requirement is already saved as an artifact and cannot be replaced here.';
          let needIndex = d.requirements[i].need_index;
          if (s.need) {
            const hit = matchEntry(d.needs, (n) => n.capability, String(s.need));
            if (hit >= 0) needIndex = hit;
          }
          d.requirements[i] = {
            ...d.requirements[i],
            need_index: needIndex,
            text: s.text !== undefined ? String(s.text) : d.requirements[i].text,
            fit_criterion: s.fit_criterion !== undefined ? String(s.fit_criterion) : d.requirements[i].fit_criterion,
            verification_method: verificationMethod(s.verification_method, d.requirements[i].verification_method),
          };
          return null;
        }
        const usable = d.needs.map((n, i) => ({ n, i })).filter(({ n }) => n.capability.trim());
        if (usable.length === 0) return 'Add a user need first — requirements derive from needs.';
        const wanted = String(s.need || '').trim().toLowerCase();
        let needIndex = usable[0].i;
        if (wanted) {
          const hit = usable.find(({ n }) => {
            const cap = n.capability.trim().toLowerCase();
            return cap === wanted || cap.includes(wanted) || wanted.includes(cap);
          });
          if (hit) needIndex = hit.i;
        }
        d.requirements.push({
          need_index: needIndex,
          text: String(s.text || 'The system shall '),
          fit_criterion: String(s.fit_criterion || ''),
          verification_method: verificationMethod(s.verification_method, 'test'),
        });
        return null;
      }
      case 'nfr': {
        const category =
          NFR_CATEGORIES.find((c) => c.toLowerCase() === String(s.category || '').trim().toLowerCase()) ||
          NFR_CATEGORIES[0];
        if (s.replaces) {
          const i = matchEntry(d.nfrs, (n) => n.text, String(s.replaces));
          if (i < 0) return `No NFR matching "${s.replaces}" to replace.`;
          if (d.nfrs[i].artifact_id) return 'That NFR is already saved as an artifact and cannot be replaced here.';
          const nextCategory = s.category !== undefined ? category : d.nfrs[i].category;
          d.nfrs[i] = {
            ...d.nfrs[i],
            category: nextCategory,
            text: s.text !== undefined ? String(s.text) : d.nfrs[i].text,
            fit_criterion: s.fit_criterion !== undefined ? String(s.fit_criterion) : d.nfrs[i].fit_criterion,
            verification_method: verificationMethod(s.verification_method, d.nfrs[i].verification_method),
          };
          d.openNfr[nextCategory] = true;
          return null;
        }
        if (!String(s.text || '').trim()) return 'The NFR suggestion has no text.';
        d.nfrs.push({
          category,
          text: String(s.text),
          fit_criterion: String(s.fit_criterion || ''),
          verification_method: verificationMethod(s.verification_method, 'test'),
        });
        d.openNfr[category] = true;
        return null;
      }
      case 'hazard': {
        if (s.replaces) {
          const i = matchEntry(d.hazards, (h) => h.hazard, String(s.replaces));
          if (i < 0) return `No hazard matching "${s.replaces}" to replace.`;
          if (d.hazards[i].artifact_id) return 'That hazard is already saved as an artifact and cannot be replaced here.';
          d.hazards[i] = {
            ...d.hazards[i],
            hazard: s.hazard !== undefined ? String(s.hazard) : d.hazards[i].hazard,
            harm: s.harm !== undefined ? String(s.harm) : d.hazards[i].harm,
            severity: SEVERITIES.includes(String(s.severity)) ? String(s.severity) : d.hazards[i].severity,
          };
          return null;
        }
        if (!String(s.hazard || '').trim()) return 'The hazard suggestion has no description.';
        d.hazards.push({
          hazard: String(s.hazard),
          harm: String(s.harm || ''),
          severity: SEVERITIES.includes(String(s.severity)) ? String(s.severity) : 'moderate',
        });
        return null;
      }
      default:
        return `Unknown suggestion kind "${s.kind}".`;
    }
  };

  // Apply a batch of suggestions in order against one working copy, then
  // commit every touched section in a single pass. Returns one result per
  // suggestion (null = applied, string = reason it was skipped).
  const handleAddSuggestions = (list: CopilotSuggestion[]): (string | null)[] => {
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
    };
    const results = list.map((s) => applySuggestionToDraft(d, s));
    setVision(d.vision);
    setProblem(d.problem);
    setTargetUsers(d.targetUsers);
    setPersonas(d.personas);
    setNeeds(d.needs);
    setRequirements(d.requirements);
    setNfrs(d.nfrs);
    setHazards(d.hazards);
    setOpenNfrCategories(d.openNfr);
    return results;
  };

  const buildAnswers = (): Record<string, any> => ({
    ...(session?.answers || {}),
    step_1: { vision, problem_statement: problem, target_users: targetUsers },
    step_2: { personas },
    step_2_ids: personas.map((p) => p.artifact_id).filter(Boolean),
    step_3: { needs },
    step_3_ids: needs.map((n) => n.artifact_id).filter(Boolean),
    step_4: { requirements },
    step_4_ids: requirements.map((r) => r.artifact_id).filter(Boolean),
    step_5: { nfrs },
    step_5_ids: nfrs.map((n) => n.artifact_id).filter(Boolean),
    step_6: { hazards },
    step_7: { selected: stubSelected, created: stubCreated },
  });

  const persist = async (nextStep: number, answersOverride?: Record<string, any>) => {
    if (!session) return;
    const answers = answersOverride || buildAnswers();
    const res = await guidedAPI.saveStep(session.id, nextStep, answers);
    setSession(res.data);
  };

  const loadDrafts = useCallback(async () => {
    if (!projectId) return;
    setDraftsLoading(true);
    try {
      const res = await artifactAPI.list(projectId);
      setDraftArtifacts((res.data || []).filter((a) => a.attributes?.status === 'draft'));
    } catch (err: any) {
      setError(`Failed to load draft artifacts: ${err.response?.data || err.message}`);
    } finally {
      setDraftsLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    if (step === 8 && session) loadDrafts();
  }, [step, session, loadDrafts]);

  // -------------------------------------------------------------------------
  // Per-step Next handlers (materialize + save + advance)
  // -------------------------------------------------------------------------

  const goTo = (nextStep: number) => {
    setStep(nextStep);
    setMaxReached((m) => Math.max(m, nextStep));
  };

  const handleNext = async () => {
    if (!session || !projectId) return;
    setBusy(true);
    setError('');
    // Nudge the copilot immediately — before the save round-trips — so its
    // thinking indicator appears the moment the user advances.
    if (step <= 7) {
      chatRef.current?.nudge(
        step + 1,
        `saved step ${step} ("${STEP_LABELS[step - 1]}") and moved on to step ${step + 1} ("${STEP_LABELS[step]}")`
      );
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
      } else if (step === 2) {
        const updated = personas.slice();
        const newIdx: number[] = [];
        const drafts: DraftSpec[] = [];
        updated.forEach((p, i) => {
          if (!p.artifact_id && p.name.trim()) {
            newIdx.push(i);
            drafts.push({
              type: 'persona',
              title: p.name.trim(),
              body: `**Role:** ${p.role}\n\n**Goals:**\n${p.goals}\n\n**Pain points:**\n${p.pains}`,
            });
          }
        });
        if (drafts.length > 0) {
          const res = await guidedAPI.materializeDrafts(session.id, drafts);
          (res.data.artifact_ids || []).forEach((id, j) => {
            if (newIdx[j] !== undefined) updated[newIdx[j]] = { ...updated[newIdx[j]], artifact_id: id };
          });
          setPersonas(updated);
        }
        const answers = buildAnswers();
        answers.step_2 = { personas: updated };
        answers.step_2_ids = updated.map((p) => p.artifact_id).filter(Boolean);
        await persist(3, answers);
        goTo(3);
      } else if (step === 3) {
        const updated = needs.slice();
        const newIdx: number[] = [];
        const drafts: DraftSpec[] = [];
        updated.forEach((n, i) => {
          if (!n.artifact_id && n.capability.trim()) {
            const persona = personas[n.persona_index];
            const personaName = persona?.name || 'a user';
            const sentence = `As ${personaName}, I need ${n.capability.trim()} so that ${n.outcome.trim() || '…'}`;
            newIdx.push(i);
            drafts.push({
              type: 'user-need',
              title: sentence.length > 120 ? `${sentence.slice(0, 117)}…` : sentence,
              body: sentence,
              links: persona?.artifact_id ? [{ type: 'relates-to', to_id: persona.artifact_id }] : [],
            });
          }
        });
        if (drafts.length > 0) {
          const res = await guidedAPI.materializeDrafts(session.id, drafts);
          (res.data.artifact_ids || []).forEach((id, j) => {
            if (newIdx[j] !== undefined) updated[newIdx[j]] = { ...updated[newIdx[j]], artifact_id: id };
          });
          setNeeds(updated);
        }
        const answers = buildAnswers();
        answers.step_3 = { needs: updated };
        answers.step_3_ids = updated.map((n) => n.artifact_id).filter(Boolean);
        await persist(4, answers);
        goTo(4);
      } else if (step === 4) {
        const updated = requirements.slice();
        const newIdx: number[] = [];
        const drafts: DraftSpec[] = [];
        updated.forEach((r, i) => {
          if (!r.artifact_id && r.text.trim()) {
            const need = needs[r.need_index];
            newIdx.push(i);
            drafts.push({
              type: 'requirement',
              title: r.text.trim().length > 120 ? `${r.text.trim().slice(0, 117)}…` : r.text.trim(),
              body: `${r.text.trim()}${r.fit_criterion.trim() ? `\n\n**Fit criterion:** ${r.fit_criterion.trim()}` : ''}`,
              attributes: { verification_method: r.verification_method },
              links: need?.artifact_id ? [{ type: 'derives-from', to_id: need.artifact_id }] : [],
            });
          }
        });
        if (drafts.length > 0) {
          const res = await guidedAPI.materializeDrafts(session.id, drafts);
          (res.data.artifact_ids || []).forEach((id, j) => {
            if (newIdx[j] !== undefined) updated[newIdx[j]] = { ...updated[newIdx[j]], artifact_id: id };
          });
          setRequirements(updated);
        }
        const answers = buildAnswers();
        answers.step_4 = { requirements: updated };
        answers.step_4_ids = updated.map((r) => r.artifact_id).filter(Boolean);
        await persist(5, answers);
        goTo(5);
      } else if (step === 5) {
        const updated = nfrs.slice();
        const newIdx: number[] = [];
        const drafts: DraftSpec[] = [];
        updated.forEach((n, i) => {
          if (!n.artifact_id && n.text.trim()) {
            newIdx.push(i);
            drafts.push({
              type: 'requirement',
              title: `[${n.category}] ${n.text.trim().length > 100 ? `${n.text.trim().slice(0, 97)}…` : n.text.trim()}`,
              body: `${n.text.trim()}${n.fit_criterion.trim() ? `\n\n**Fit criterion:** ${n.fit_criterion.trim()}` : ''}`,
              attributes: { verification_method: n.verification_method, category: n.category },
            });
          }
        });
        if (drafts.length > 0) {
          const res = await guidedAPI.materializeDrafts(session.id, drafts);
          (res.data.artifact_ids || []).forEach((id, j) => {
            if (newIdx[j] !== undefined) updated[newIdx[j]] = { ...updated[newIdx[j]], artifact_id: id };
          });
          setNfrs(updated);
        }
        const answers = buildAnswers();
        answers.step_5 = { nfrs: updated };
        answers.step_5_ids = updated.map((n) => n.artifact_id).filter(Boolean);
        await persist(6, answers);
        goTo(6);
      } else if (step === 6) {
        const updated = hazards.slice();
        const newIdx: number[] = [];
        const drafts: DraftSpec[] = [];
        updated.forEach((h, i) => {
          if (!h.artifact_id && h.hazard.trim()) {
            newIdx.push(i);
            drafts.push({
              type: 'hazard',
              title: h.hazard.trim(),
              body: `**Potential harm:** ${h.harm}\n\n**Severity:** ${h.severity}`,
              attributes: { severity: h.severity },
            });
          }
        });
        if (drafts.length > 0) {
          const res = await guidedAPI.materializeDrafts(session.id, drafts);
          (res.data.artifact_ids || []).forEach((id, j) => {
            if (newIdx[j] !== undefined) updated[newIdx[j]] = { ...updated[newIdx[j]], artifact_id: id };
          });
          setHazards(updated);
        }
        const answers = buildAnswers();
        answers.step_6 = { hazards: updated };
        await persist(7, answers);
        goTo(7);
      } else if (step === 7) {
        const candidates = testCandidates();
        const toCreate = candidates.filter(
          (c) => (stubSelected[c.id] ?? true) && !stubCreated[c.id]
        );
        let createdMap = { ...stubCreated };
        if (toCreate.length > 0) {
          const drafts: DraftSpec[] = toCreate.map((c) => ({
            type: 'test-case',
            title: `Verify: ${c.title}`,
            body: `Test case stub for requirement: ${c.title}\n\nDefine steps, preconditions and expected results.`,
            links: [{ type: 'verifies', to_id: c.id }],
          }));
          const res = await guidedAPI.materializeDrafts(session.id, drafts);
          (res.data.artifact_ids || []).forEach((id, j) => {
            createdMap = { ...createdMap, [toCreate[j].id]: id };
          });
          setStubCreated(createdMap);
        }
        const answers = buildAnswers();
        answers.step_7 = { selected: stubSelected, created: createdMap };
        await persist(8, answers);
        goTo(8);
      }
    } catch (err: any) {
      setError(`Failed to save step: ${err.response?.data || err.message}`);
    } finally {
      setBusy(false);
    }
  };

  const handleSkip = async () => {
    if (!session) return;
    setBusy(true);
    setError('');
    chatRef.current?.nudge(
      step + 1,
      `skipped step ${step} ("${STEP_LABELS[step - 1]}") and moved on to step ${step + 1} ("${STEP_LABELS[step]}")`
    );
    try {
      await persist(step + 1);
      goTo(step + 1);
    } catch (err: any) {
      setError(`Failed to skip step: ${err.response?.data || err.message}`);
    } finally {
      setBusy(false);
    }
  };

  const handleBack = () => {
    if (step > 1) setStep(step - 1);
  };

  // Escape hatch: drop the current in-progress session and return to the
  // Start/Modify landing. Materialized drafts stay in the project.
  const handleAbandon = async () => {
    if (!session) return;
    if (
      !window.confirm(
        'Abandon this guided session? Draft artifacts already created are kept, but unsaved entries are discarded.'
      )
    )
      return;
    setBusy(true);
    try {
      await guidedAPI.abandon(session.id);
      setSession(null);
      setStep(1);
      setMaxReached(1);
      setError('');
    } catch (err: any) {
      setError(`Failed to abandon session: ${err.response?.data || err.message}`);
    } finally {
      setBusy(false);
    }
  };

  const handleCommit = async () => {
    if (!session) return;
    setBusy(true);
    setError('');
    try {
      await guidedAPI.commit(session.id);
      setCommitted(true);
    } catch (err: any) {
      setError(`Failed to commit: ${err.response?.data || err.message}`);
    } finally {
      setBusy(false);
    }
  };

  const handleCreateBaseline = async () => {
    if (!projectId) return;
    setBusy(true);
    try {
      await baselineAPI.create(projectId, 'Initial requirements');
      setBaselineCreated(true);
      setError('');
    } catch (err: any) {
      setError(`Failed to create baseline: ${err.response?.data || err.message}`);
    } finally {
      setBusy(false);
    }
  };

  const discardDraft = async (artifact: Artifact) => {
    try {
      await artifactAPI.delete(artifact.id);
      setDraftArtifacts(draftArtifacts.filter((a) => a.id !== artifact.id));
    } catch (err: any) {
      setError(`Failed to discard draft: ${err.response?.data || err.message}`);
    }
  };

  /** Requirements (functional + NFR) with verification method 'test' that were materialized. */
  const testCandidates = (): { id: string; title: string }[] => {
    const out: { id: string; title: string }[] = [];
    requirements.forEach((r) => {
      if (r.artifact_id && r.verification_method === 'test' && r.text.trim()) {
        out.push({ id: r.artifact_id, title: r.text.trim() });
      }
    });
    nfrs.forEach((n) => {
      if (n.artifact_id && n.verification_method === 'test' && n.text.trim()) {
        out.push({ id: n.artifact_id, title: `[${n.category}] ${n.text.trim()}` });
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
    return (
      <div style={{ padding: 24, maxWidth: 700, margin: '0 auto' }}>
        <div className="card" style={{ textAlign: 'center', padding: 40 }}>
          <h2 style={{ color: 'var(--text)', marginBottom: 12 }}>Guided requirements definition</h2>
          {latestCommitted ? (
            <p style={{ color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.6 }}>
              A guided definition has already been completed for this project. Reopen it to refine or
              extend the committed set — existing entries stay linked to their artifacts, and anything
              you add becomes new draft artifacts to review and commit.
            </p>
          ) : (
            <p style={{ color: 'var(--text-muted)', marginBottom: 24, lineHeight: 1.6 }}>
              A step-by-step flow that walks you from product framing through personas, user needs,
              requirements, hazards and verification stubs — creating traceable draft artifacts as you go.
              You review and commit everything at the end.
            </p>
          )}
          {error && <div style={{ color: 'var(--danger)', marginBottom: 16, fontSize: 13 }}>{error}</div>}
          {latestCommitted ? (
            <div style={{ display: 'flex', gap: 12, justifyContent: 'center' }}>
              <button
                className="button"
                style={{ background: 'var(--accent)' }}
                onClick={() => modifySession(latestCommitted)}
                disabled={busy}
              >
                {busy ? 'Opening…' : 'Modify guided definition'}
              </button>
              <button className="button-secondary" onClick={startSession} disabled={busy}>
                Start over from scratch
              </button>
            </div>
          ) : (
            <button className="button" style={{ background: 'var(--accent)' }} onClick={startSession} disabled={busy}>
              {busy ? 'Starting…' : 'Start guided definition'}
            </button>
          )}
        </div>
      </div>
    );
  }

  if (committed) {
    return (
      <div style={{ padding: 24, maxWidth: 700, margin: '0 auto' }}>
        <div className="card" style={{ textAlign: 'center', padding: 40 }}>
          <div style={{ fontSize: 40, marginBottom: 10 }}>✓</div>
          <h2 style={{ color: 'var(--success)', marginBottom: 12 }}>Requirements committed</h2>
          <p style={{ color: 'var(--text-muted)', marginBottom: 24 }}>
            All draft artifacts are now live. You can baseline this initial set to keep an immutable
            snapshot for traceability.
          </p>
          {error && <div style={{ color: 'var(--danger)', marginBottom: 16, fontSize: 13 }}>{error}</div>}
          <div style={{ display: 'flex', gap: 12, justifyContent: 'center' }}>
            {baselineCreated ? (
              <span style={{ color: 'var(--success)', fontSize: 14, alignSelf: 'center' }}>
                ✓ Baseline "Initial requirements" created
              </span>
            ) : (
              <button className="button" onClick={handleCreateBaseline} disabled={busy}>
                {busy ? 'Creating…' : 'Create baseline'}
              </button>
            )}
            <Link to="../requirements" className="button-secondary" style={{ textDecoration: 'none' }}>
              Go to Requirements
            </Link>
            <button
              className="button-secondary"
              onClick={() => {
                const committedSession = session;
                setCommitted(false);
                setLatestCommitted(committedSession);
                setSession(null);
                if (committedSession) modifySession(committedSession);
              }}
              disabled={busy}
            >
              Modify guided definition
            </button>
          </div>
        </div>
      </div>
    );
  }

  const renderStepContent = () => {
    switch (step) {
      case 1:
        return (
          <div className="card">
            <h3>Product framing</h3>
            <div style={helpText}>
              Frame the product in a few sentences. This is saved to the product profile and shown on the
              overview page.
            </div>
            <div className="form-group">
              <label>Vision</label>
              <textarea rows={3} value={vision} onChange={(e) => setVision(e.target.value)} placeholder="What does the world look like once this product succeeds?" />
            </div>
            <div className="form-group">
              <label>Problem statement</label>
              <textarea rows={3} value={problem} onChange={(e) => setProblem(e.target.value)} placeholder="What problem are we solving, for whom, and why now?" />
            </div>
            <div className="form-group">
              <label>Target users</label>
              <textarea rows={3} value={targetUsers} onChange={(e) => setTargetUsers(e.target.value)} placeholder="Who are the primary users and buyers?" />
            </div>
          </div>
        );
      case 2:
        return (
          <div>
            <div className="card" style={{ marginBottom: 12 }}>
              <h3>Personas</h3>
              <div style={{ ...helpText, marginBottom: 0 }}>
                Describe the key people who will use the product. Each persona becomes a draft artifact
                that user needs link back to. Personas already created are marked with a green dot.
              </div>
            </div>
            <RepeatingCardList<PersonaEntry>
              items={personas}
              onChange={setPersonas}
              makeNew={() => ({ name: '', role: '', goals: '', pains: '' })}
              addLabel="Add persona"
              emptyText="No personas yet — add the first one."
              renderItem={(p, i, update) => (
                <div>
                  <div style={{ display: 'flex', gap: 10, marginBottom: 10, alignItems: 'center' }}>
                    {p.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                    <input
                      value={p.name}
                      onChange={(e) => update({ name: e.target.value })}
                      placeholder="Persona name (e.g. Maya the Machinist)"
                      style={{ fontWeight: 600 }}
                      disabled={!!p.artifact_id}
                    />
                  </div>
                  <div className="form-group">
                    <label style={{ fontSize: 12 }}>Role</label>
                    <input value={p.role} onChange={(e) => update({ role: e.target.value })} placeholder="Their job / context" disabled={!!p.artifact_id} />
                  </div>
                  <div className="form-group">
                    <label style={{ fontSize: 12 }}>Goals</label>
                    <textarea rows={2} value={p.goals} onChange={(e) => update({ goals: e.target.value })} placeholder="What are they trying to achieve?" disabled={!!p.artifact_id} />
                  </div>
                  <div className="form-group" style={{ marginBottom: 0 }}>
                    <label style={{ fontSize: 12 }}>Pain points</label>
                    <textarea rows={2} value={p.pains} onChange={(e) => update({ pains: e.target.value })} placeholder="What frustrates them today?" disabled={!!p.artifact_id} />
                  </div>
                </div>
              )}
              canRemove={(p) => !p.artifact_id}
            />
          </div>
        );
      case 3: {
        const usablePersonas = personas.filter((p) => p.name.trim());
        return (
          <div>
            <div className="card" style={{ marginBottom: 12 }}>
              <h3>User needs</h3>
              <div style={{ ...helpText, marginBottom: 0 }}>
                For each persona, capture needs as "As <em>persona</em>, I need <em>capability</em> so
                that <em>outcome</em>". Each need becomes a draft artifact linked to its persona.
              </div>
            </div>
            {usablePersonas.length === 0 && (
              <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>
                No personas defined — go back to step 2 to add at least one persona.
              </div>
            )}
            {personas.map((p, pi) =>
              p.name.trim() ? (
                <div key={pi} className="card" style={{ marginBottom: 12 }}>
                  <div style={{ fontWeight: 600, color: 'var(--text)', marginBottom: 10 }}>{p.name}</div>
                  {needs.map((n, ni) =>
                    n.persona_index === pi ? (
                      <div key={ni} style={{ border: '1px solid var(--border-soft)', borderRadius: 4, padding: 10, marginBottom: 8 }}>
                        <div style={{ display: 'flex', gap: 8, marginBottom: 6, alignItems: 'center' }}>
                          {n.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                          <span style={{ fontSize: 13, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>I need</span>
                          <input
                            value={n.capability}
                            onChange={(e) => setNeeds(needs.map((x, j) => (j === ni ? { ...x, capability: e.target.value } : x)))}
                            placeholder="capability"
                            style={{ padding: '6px 8px', fontSize: 13 }}
                            disabled={!!n.artifact_id}
                          />
                          <span style={{ fontSize: 13, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>so that</span>
                          <input
                            value={n.outcome}
                            onChange={(e) => setNeeds(needs.map((x, j) => (j === ni ? { ...x, outcome: e.target.value } : x)))}
                            placeholder="outcome"
                            style={{ padding: '6px 8px', fontSize: 13 }}
                            disabled={!!n.artifact_id}
                          />
                          {!n.artifact_id && (
                            <button
                              onClick={() => setNeeds(needs.filter((_, j) => j !== ni))}
                              style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                              title="Remove need"
                            >
                              ✕
                            </button>
                          )}
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--neutral)', fontStyle: 'italic' }}>
                          As {p.name}, I need {n.capability || '…'} so that {n.outcome || '…'}
                        </div>
                      </div>
                    ) : null
                  )}
                  <button
                    className="button-secondary"
                    style={{ padding: '6px 12px', fontSize: 13 }}
                    onClick={() => setNeeds([...needs, { persona_index: pi, capability: '', outcome: '' }])}
                  >
                    + Add need for {p.name}
                  </button>
                </div>
              ) : null
            )}
          </div>
        );
      }
      case 4: {
        const usableNeeds = needs.filter((n) => n.capability.trim());
        return (
          <div>
            <div className="card" style={{ marginBottom: 12 }}>
              <h3>Requirements</h3>
              <div style={{ ...helpText, marginBottom: 0 }}>
                Turn each user need into one or more testable "The system shall …" requirements with a fit
                criterion and a verification method.
              </div>
            </div>
            {usableNeeds.length === 0 && (
              <div style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>
                No user needs defined — go back to step 3.
              </div>
            )}
            {needs.map((n, ni) => {
              if (!n.capability.trim()) return null;
              const personaName = personas[n.persona_index]?.name || 'a user';
              return (
                <div key={ni} className="card" style={{ marginBottom: 12 }}>
                  <div style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 10 }}>
                    Need: <em>As {personaName}, I need {n.capability} so that {n.outcome || '…'}</em>
                  </div>
                  {requirements.map((r, ri) =>
                    r.need_index === ni ? (
                      <div key={ri} style={{ border: '1px solid var(--border-soft)', borderRadius: 4, padding: 10, marginBottom: 8 }}>
                        <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
                          {r.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                          <input
                            value={r.text}
                            onChange={(e) => setRequirements(requirements.map((x, j) => (j === ri ? { ...x, text: e.target.value } : x)))}
                            placeholder="The system shall …"
                            style={{ padding: '6px 8px', fontSize: 13 }}
                            disabled={!!r.artifact_id}
                          />
                          {!r.artifact_id && (
                            <button
                              onClick={() => setRequirements(requirements.filter((_, j) => j !== ri))}
                              style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                              title="Remove requirement"
                            >
                              ✕
                            </button>
                          )}
                        </div>
                        <div style={{ display: 'flex', gap: 8 }}>
                          <textarea
                            rows={2}
                            value={r.fit_criterion}
                            onChange={(e) => setRequirements(requirements.map((x, j) => (j === ri ? { ...x, fit_criterion: e.target.value } : x)))}
                            placeholder="Fit criterion — how do we know it is met?"
                            style={{ fontSize: 13, minHeight: 50, flex: 1 }}
                            disabled={!!r.artifact_id}
                          />
                          <div style={{ width: 170 }}>
                            <label style={{ fontSize: 11, color: 'var(--text-muted)' }}>Verification method</label>
                            <select
                              value={r.verification_method}
                              onChange={(e) => setRequirements(requirements.map((x, j) => (j === ri ? { ...x, verification_method: e.target.value } : x)))}
                              style={{ padding: '6px 8px', fontSize: 13 }}
                              disabled={!!r.artifact_id}
                            >
                              {VERIFICATION_METHODS.map((m) => (
                                <option key={m} value={m}>{m}</option>
                              ))}
                            </select>
                          </div>
                        </div>
                      </div>
                    ) : null
                  )}
                  <button
                    className="button-secondary"
                    style={{ padding: '6px 12px', fontSize: 13 }}
                    onClick={() =>
                      setRequirements([
                        ...requirements,
                        { need_index: ni, text: 'The system shall ', fit_criterion: '', verification_method: 'test' },
                      ])
                    }
                  >
                    + Add requirement
                  </button>
                </div>
              );
            })}
          </div>
        );
      }
      case 5:
        return (
          <div>
            <div className="card" style={{ marginBottom: 12 }}>
              <h3>Non-functional requirements &amp; constraints</h3>
              <div style={{ ...helpText, marginBottom: 0 }}>
                Expand the quality attributes that matter for this product and add requirements under each.
              </div>
            </div>
            {NFR_CATEGORIES.map((cat) => {
              const catEntries = nfrs
                .map((n, i) => ({ n, i }))
                .filter(({ n }) => n.category === cat);
              const open = openNfrCategories[cat] ?? catEntries.length > 0;
              return (
                <div key={cat} className="card" style={{ marginBottom: 10, padding: 14 }}>
                  <div
                    style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: 'pointer' }}
                    onClick={() => setOpenNfrCategories({ ...openNfrCategories, [cat]: !open })}
                  >
                    <input
                      type="checkbox"
                      checked={open}
                      onChange={() => setOpenNfrCategories({ ...openNfrCategories, [cat]: !open })}
                      style={{ width: 'auto' }}
                      onClick={(e) => e.stopPropagation()}
                    />
                    <span style={{ fontWeight: 600, color: 'var(--text)', flex: 1 }}>{cat}</span>
                    <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                      {catEntries.length > 0 ? `${catEntries.length} requirement${catEntries.length > 1 ? 's' : ''}` : ''}
                    </span>
                  </div>
                  {open && (
                    <div style={{ marginTop: 10 }}>
                      {catEntries.map(({ n, i }) => (
                        <div key={i} style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
                          {n.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                          <input
                            value={n.text}
                            onChange={(e) => setNfrs(nfrs.map((x, j) => (j === i ? { ...x, text: e.target.value } : x)))}
                            placeholder={`The system shall … (${cat.toLowerCase()})`}
                            style={{ padding: '6px 8px', fontSize: 13 }}
                            disabled={!!n.artifact_id}
                          />
                          <select
                            value={n.verification_method}
                            onChange={(e) => setNfrs(nfrs.map((x, j) => (j === i ? { ...x, verification_method: e.target.value } : x)))}
                            style={{ padding: '6px 8px', fontSize: 13, width: 150 }}
                            disabled={!!n.artifact_id}
                          >
                            {VERIFICATION_METHODS.map((m) => (
                              <option key={m} value={m}>{m}</option>
                            ))}
                          </select>
                          {!n.artifact_id && (
                            <button
                              onClick={() => setNfrs(nfrs.filter((_, j) => j !== i))}
                              style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', padding: 4 }}
                              title="Remove"
                            >
                              ✕
                            </button>
                          )}
                        </div>
                      ))}
                      <button
                        className="button-secondary"
                        style={{ padding: '5px 10px', fontSize: 12 }}
                        onClick={() =>
                          setNfrs([...nfrs, { category: cat, text: '', fit_criterion: '', verification_method: 'test' }])
                        }
                      >
                        + Add {cat.toLowerCase()} requirement
                      </button>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        );
      case 6:
        return (
          <div>
            <div className="card" style={{ marginBottom: 12 }}>
              <h3>Hazards (optional)</h3>
              <div style={{ ...helpText, marginBottom: 0 }}>
                If this product could cause harm when it fails or is misused, capture hazards here. Skip
                this step if it does not apply.
              </div>
            </div>
            <RepeatingCardList<HazardEntry>
              items={hazards}
              onChange={setHazards}
              makeNew={() => ({ hazard: '', harm: '', severity: 'moderate' })}
              addLabel="Add hazard"
              emptyText="No hazards recorded — use Skip if not applicable."
              renderItem={(h, i, update) => (
                <div style={{ display: 'flex', gap: 8, alignItems: 'center', paddingRight: 24 }}>
                  {h.artifact_id && <span title="Draft created" style={{ color: 'var(--success)' }}>●</span>}
                  <input value={h.hazard} onChange={(e) => update({ hazard: e.target.value })} placeholder="Hazard (what could go wrong)" style={{ padding: '6px 8px', fontSize: 13 }} disabled={!!h.artifact_id} />
                  <input value={h.harm} onChange={(e) => update({ harm: e.target.value })} placeholder="Potential harm" style={{ padding: '6px 8px', fontSize: 13 }} disabled={!!h.artifact_id} />
                  <select
                    value={h.severity}
                    onChange={(e) => update({ severity: e.target.value })}
                    style={{ padding: '6px 8px', fontSize: 13, width: 130 }}
                    disabled={!!h.artifact_id}
                  >
                    {SEVERITIES.map((s) => (
                      <option key={s} value={s}>{s}</option>
                    ))}
                  </select>
                </div>
              )}
              canRemove={(h) => !h.artifact_id}
            />
          </div>
        );
      case 7: {
        const candidates = testCandidates();
        return (
          <div className="card">
            <h3>Verification stubs (optional)</h3>
            <div style={helpText}>
              Requirements with verification method <strong>test</strong> are listed below. Checked
              requirements get a draft test case ("Verify: …") linked with a <em>verifies</em> relationship.
            </div>
            {candidates.length === 0 ? (
              <div style={{ color: 'var(--neutral)', fontSize: 13 }}>
                No requirements with method "test" were created — nothing to stub. Use Skip or Next.
              </div>
            ) : (
              candidates.map((c) => (
                <label
                  key={c.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 10,
                    padding: '8px 10px',
                    border: '1px solid var(--border-soft)',
                    borderRadius: 4,
                    marginBottom: 6,
                    cursor: stubCreated[c.id] ? 'default' : 'pointer',
                    fontWeight: 400,
                  }}
                >
                  <input
                    type="checkbox"
                    style={{ width: 'auto' }}
                    checked={stubCreated[c.id] ? true : stubSelected[c.id] ?? true}
                    disabled={!!stubCreated[c.id]}
                    onChange={(e) => setStubSelected({ ...stubSelected, [c.id]: e.target.checked })}
                  />
                  <span style={{ flex: 1, fontSize: 13, color: 'var(--text)' }}>{c.title}</span>
                  {stubCreated[c.id] && <span style={{ fontSize: 12, color: 'var(--success)' }}>✓ stub created</span>}
                </label>
              ))
            )}
          </div>
        );
      }
      case 8: {
        const grouped: Record<string, Artifact[]> = {};
        draftArtifacts.forEach((a) => {
          (grouped[a.type] = grouped[a.type] || []).push(a);
        });
        return (
          <div className="card">
            <h3>Review &amp; commit</h3>
            <div style={helpText}>
              These draft artifacts were created during this flow. Discard any you don't want, then commit
              to make them all live.
            </div>
            {draftsLoading ? (
              <div style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading drafts…</div>
            ) : draftArtifacts.length === 0 ? (
              <div style={{ color: 'var(--neutral)', fontSize: 13, marginBottom: 12 }}>No draft artifacts found.</div>
            ) : (
              Object.keys(grouped)
                .sort()
                .map((type) => (
                  <div key={type} style={{ marginBottom: 16 }}>
                    <div style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', marginBottom: 6 }}>
                      {type} ({grouped[type].length})
                    </div>
                    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                      <tbody>
                        {grouped[type].map((a) => (
                          <tr key={a.id} style={{ borderBottom: '1px solid var(--surface-hover)' }}>
                            <td style={{ padding: '6px 8px', fontSize: 13, color: 'var(--text)' }}>{a.title}</td>
                            <td style={{ padding: '6px 8px', width: 80, textAlign: 'right' }}>
                              <button
                                onClick={() => discardDraft(a)}
                                style={{ background: 'none', border: 'none', color: 'var(--danger)', cursor: 'pointer', width: 'auto', fontSize: 12, padding: 2 }}
                              >
                                Discard
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ))
            )}
            <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 16 }}>
              <button className="button-secondary" onClick={handleBack} disabled={busy}>
                ← Back
              </button>
              <button className="button" onClick={handleCommit} disabled={busy || draftArtifacts.length === 0}>
                {busy ? 'Committing…' : `Commit ${draftArtifacts.length} artifact${draftArtifacts.length === 1 ? '' : 's'}`}
              </button>
            </div>
          </div>
        );
      }
      default:
        return null;
    }
  };

  return (
    <div style={{ padding: 24, maxWidth: 1560, margin: '0 auto' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <h2 style={{ color: 'var(--text)', margin: 0 }}>Guided requirements definition</h2>
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
      {error && (
        <div
          style={{
            background: 'var(--tint-red)',
            border: '1px solid var(--danger)',
            color: 'var(--danger-strong)',
            padding: '10px 14px',
            borderRadius: 4,
            marginBottom: 16,
            fontSize: 13,
          }}
        >
          {error}
        </div>
      )}
      <div style={{ display: 'flex', gap: 20, alignItems: 'flex-start' }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <StepShell
            steps={STEP_LABELS}
            current={step}
            maxReached={maxReached}
            onSelectStep={(s) => setStep(s)}
            onBack={handleBack}
            onNext={handleNext}
            backDisabled={step === 1}
            busy={busy}
            hideNav={step === 8}
            extraAction={
              step === 6 || step === 7 ? (
                <button className="button-secondary" onClick={handleSkip} disabled={busy}>
                  Skip
                </button>
              ) : undefined
            }
          >
            {renderStepContent()}
          </StepShell>
        </div>
        <GuidedChatPanel
          ref={chatRef}
          sessionId={session.id}
          step={step}
          getState={buildAnswers}
          onAddSuggestions={handleAddSuggestions}
        />
      </div>
    </div>
  );
};
