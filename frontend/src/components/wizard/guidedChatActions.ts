// The guided chat's canned commands and the turn another page asks it to
// start, beside GuidedChatPanel.tsx so the panel stays within its size budget.
import { useEffect, useRef } from 'react';
import type { ApplyTarget } from './GuidedChatPanel';

// Canned assistant commands, sent as ordinary chat messages so they read
// naturally in the transcript and the reply flows through the normal turn.
export type QuickAction = { label: string; title: string; message: string };

const QUICK_ACTIONS: QuickAction[] = [
  {
    label: 'Review step',
    title: 'Quality-check the entries on the current step',
    message:
      'Review this step: quality-check each entry I have filled in — clarity, specificity, testability — and call out the weak ones with concrete improvements (use replace suggestions where useful).',
  },
  {
    label: 'Gap analysis',
    title: 'What is missing, here and across the definition',
    message:
      'Do a gap analysis: given everything entered so far, what is missing on this step and across the definition as a whole? Propose the most important missing entries as suggestions.',
  },
  {
    label: 'Conflict check',
    title: 'Find contradictions, duplicates and inconsistencies',
    message:
      'Check for conflicts: contradictions, duplicates, or inconsistencies anywhere in what I have entered so far. List each one, say which entries clash, and how you would resolve it.',
  },
  {
    label: 'Draft for me',
    title: 'Propose entries for the current step',
    message:
      'Draft entries for this step based on everything you know about the product so far, as suggestion blocks I can add or apply.',
  },
];

// Beside an artifact there is no step to review and nothing to draft into,
// so the notes panel offers what a reader of one artifact actually wants.
const NOTES_QUICK_ACTIONS: QuickAction[] = [
  {
    label: 'Review this',
    title: 'Quality-check the artifact on screen',
    message:
      'Review the artifact I am reading: is it clear, specific and testable? Quote the wording that weakens it and say how you would rewrite it.',
  },
  {
    label: 'How would I verify it?',
    title: 'Propose verification for this artifact',
    message:
      'How would I verify the artifact I am reading? Propose a verification method and the test cases that would demonstrate it, and say what evidence each would produce.',
  },
  {
    label: 'What is missing?',
    title: 'Gaps around this artifact',
    message:
      'What is missing around the artifact I am reading — unstated hazards, absent non-functional requirements, needs or requirements it should trace to but does not?',
  },
];

// Beside the project as a whole (the floating assistant on a page with no
// artifact open): neither a wizard step nor one artifact to talk about.
const PROJECT_QUICK_ACTIONS: QuickAction[] = [
  {
    label: 'Gap analysis',
    title: 'What is missing across the project',
    message:
      'Do a gap analysis of this project: which needs have no requirement, which requirements have no test or design item, and which hazards have no mitigation? Propose the most important missing entries as suggestions.',
  },
  {
    label: 'Conflict check',
    title: 'Find contradictions and duplicates',
    message:
      'Check this project for conflicts: contradictions, duplicates or inconsistencies between its requirements. List each one, say which artifacts clash, and how you would resolve it.',
  },
  {
    label: 'Draft tests',
    title: 'Propose test cases for unverified requirements',
    message:
      'Find the requirements in this project that have no test case verifying them, and draft test cases for the most important ones as suggestions.',
  },
];

/** The quick actions for where the chat sits: an artifact, the project, or a wizard step. */
export const quickActionsFor = (artifactId: string | undefined, applyTarget: ApplyTarget, step: number): QuickAction[] =>
  artifactId ? NOTES_QUICK_ACTIONS : applyTarget === 'project' && !step ? PROJECT_QUICK_ACTIONS : QUICK_ACTIONS;

/**
 * Send a turn another page asked for (openAssistant, #726) once the
 * transcript is in, so it lands after the history it follows. Each request
 * is sent at most once, and reported through onSent so the caller can
 * forget it.
 */
export const useAutoSend = (
  ready: boolean,
  request: { id: number; message: string } | null,
  send: (message: string) => unknown,
  onSent?: (id: number) => void
) => {
  const sentRef = useRef<Set<number>>(new Set());
  const sendRef = useRef(send);
  sendRef.current = send;
  const requestRef = useRef(request);
  requestRef.current = request;
  const requestId = request?.id;
  useEffect(() => {
    const r = requestRef.current;
    if (!ready || !r || sentRef.current.has(r.id)) return;
    sentRef.current.add(r.id);
    void sendRef.current(r.message);
    onSent?.(r.id);
  }, [ready, requestId, onSent]);
};
