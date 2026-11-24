import React, { useState } from 'react';
import { interviewsAPI, Interview } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { usePrompt } from '../components/ui';
import { EMAIL_INVITES_FEATURE } from '../features';
import { useFeature } from '../hooks/useFeature';

/**
 * Email invite on an interview's row (email-invites): asks for an address,
 * makes an invite named by it, and has OpenV email the link there. A server
 * with no email set up still makes the invite and shows its link to send by
 * hand. Hidden while the feature is off and once the interview is closed,
 * when an invite would be refused.
 */
export const InterviewEmailInvite: React.FC<{ interview: Interview; onError: (message: string) => void }> = ({
  interview,
  onError,
}) => {
  const on = useFeature(EMAIL_INVITES_FEATURE);
  const prompt = usePrompt();
  const [sent, setSent] = useState(false);
  if (!on || interview.status === 'closed') return null;

  const send = async (e: React.MouseEvent) => {
    e.stopPropagation();
    const email = await prompt({
      title: 'Email an invite',
      message: `OpenV emails a link to “${interview.name}”. They answer the AI interviewer in their own words, with no account needed.`,
      label: 'Email address',
      placeholder: 'name@example.com',
      confirmLabel: 'Send invite',
    });
    if (!email || !email.trim()) return;
    try {
      const res = await interviewsAPI.createInvite(interview.id, '', email.trim());
      if (res.data.emailed) {
        setSent(true);
        setTimeout(() => setSent(false), 2500);
        return;
      }
      await prompt({
        title: 'This server cannot send email',
        message: `The invite for ${email.trim()} is ready, but email is not set up here. Copy the link and send it yourself.`,
        defaultValue: `${window.location.origin}${res.data.path}`,
        confirmLabel: 'Done',
      });
    } catch (err: any) {
      onError(`Failed to email the invite: ${apiErrorMessage(err)}`);
    }
  };

  return (
    <button
      className="button-secondary"
      style={{ padding: '5px 10px', fontSize: 12 }}
      onClick={send}
      data-help-key="interviews.email-invite"
    >
      {sent ? '✓ Emailed' : 'Email invite'}
    </button>
  );
};
