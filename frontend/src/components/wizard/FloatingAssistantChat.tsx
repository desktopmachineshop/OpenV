import React, { useCallback, useEffect, useRef, useState } from 'react';
import { GuidedChatPanel } from './GuidedChatPanel';
import { openAssistantSession } from './assistantSession';
import { useProjectSuggestions } from './useProjectSuggestions';
import { AssistantContext, AssistantRequest, notifyArtifactsChanged } from './assistantStore';

interface FloatingAssistantChatProps {
  projectId: string;
  /** The context the panel shows; null when the reader removed it. */
  context: AssistantContext | null;
  request: AssistantRequest | null;
  onRequestSent: (id: number) => void;
}

const notice: React.CSSProperties = { padding: 12, fontSize: 13, lineHeight: 1.5, color: 'var(--text-muted)' };

/**
 * The floating panel's conversation: the project's one V&V Assistant
 * transcript (the wizard's and the Notes panel's, assistantSession.ts),
 * with the page's context sent with each turn. A page with an artifact open
 * sends the artifact, which the assistant reads in full; any other page is
 * named at the head of the first message sent from it, so the assistant
 * knows where the question was asked without each message repeating it.
 */
export const FloatingAssistantChat: React.FC<FloatingAssistantChatProps> = ({
  projectId,
  context,
  request,
  onRequestSent,
}) => {
  const [session, setSession] = useState<{ projectId: string; id: string } | null>(null);
  const [reason, setReason] = useState('');
  const { applied, applySuggestions } = useProjectSuggestions(projectId, notifyArtifactsChanged);
  // The page the last message named, so moving to another page names it once.
  const namedRef = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setSession(null);
    setReason('');
    namedRef.current = null;
    (async () => {
      const result = await openAssistantSession(projectId);
      if (cancelled) return;
      if ('id' in result) setSession({ projectId, id: result.id });
      else setReason(result.reason);
    })();
    return () => {
      cancelled = true;
    };
  }, [projectId]);

  const contextRef = useRef(context);
  contextRef.current = context;
  const decorate = useCallback((content: string) => {
    const ctx = contextRef.current;
    if (!ctx || ctx.artifactId || namedRef.current === ctx.label) return content;
    namedRef.current = ctx.label;
    // A message that already says what it is about (a gap list) needs no
    // heading on top of it.
    if (content.startsWith('Drafting test cases')) return content;
    return `Looking at: ${ctx.label}\n\n${content}`;
  }, []);

  if (reason) {
    return (
      <div role="status" style={notice}>
        <div style={{ fontWeight: 700, color: 'var(--text)', marginBottom: 4 }}>The V&amp;V Assistant can’t start here</div>
        {reason}
      </div>
    );
  }
  if (!session || session.projectId !== projectId) {
    return <div style={notice}>Opening the conversation…</div>;
  }
  return (
    <GuidedChatPanel
      sessionId={session.id}
      artifactId={context?.artifactId}
      embedded
      hideHeader
      applied={applied}
      applyTarget="project"
      onApplySuggestions={applySuggestions}
      autoSend={request}
      onAutoSent={onRequestSent}
      decorateMessage={decorate}
    />
  );
};
