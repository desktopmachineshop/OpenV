import React, { useCallback, useEffect, useState } from 'react';
import { Org, OrgMember, orgsAPI, planAPI, type WorkspacePlanCalendar } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { ErrorBanner, buttonStyle, mutedStyle } from '../ui';
import { WorkingDays } from '../plan/WorkingDays';
import { spanLabel } from '../plan/ganttScale';

interface OrgPlanCalendarTabProps {
  org: Org;
  isAdmin: boolean;
  /** The signed-in member, who may record their own time off. */
  currentUserId?: string;
}

const inputStyle: React.CSSProperties = { padding: '5px 8px', minHeight: 32, boxSizing: 'border-box', width: 'auto' };

// The workspace's plan calendar: the days of the week every project's plan
// works, and the days each member is away. A plan counts durations and
// float in the working days, and reports a task that asks for someone
// while they are away.
export const OrgPlanCalendarTab: React.FC<OrgPlanCalendarTabProps> = ({ org, isAdmin, currentUserId }) => {
  const [calendar, setCalendar] = useState<WorkspacePlanCalendar | null>(null);
  const [members, setMembers] = useState<OrgMember[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [who, setWho] = useState(isAdmin ? '' : currentUserId || '');
  const [start, setStart] = useState('');
  const [end, setEnd] = useState('');
  const [name, setName] = useState('');

  const load = useCallback(async () => {
    try {
      const [cal, people] = await Promise.all([planAPI.workspaceCalendar(org.id), orgsAPI.members.list(org.id)]);
      setCalendar(cal.data);
      setMembers(people.data || []);
    } catch (err) {
      setError(apiErrorMessage(err, 'Failed to load the plan calendar'));
    }
  }, [org.id]);
  useEffect(() => {
    void load();
  }, [load]);

  const run = async (fn: () => Promise<unknown>, fallback: string) => {
    setBusy(true);
    setError('');
    try {
      await fn();
      await load();
    } catch (err) {
      setError(apiErrorMessage(err, fallback));
    } finally {
      setBusy(false);
    }
  };
  const nameOf = (id: string) => members.find((m) => m.user_id === id)?.user_name || members.find((m) => m.user_id === id)?.user_email || id;
  const today = new Date().toISOString().slice(0, 10);
  if (!calendar) return <ErrorBanner message={error} onDismiss={() => setError('')} />;
  return (
    <div data-help-key="gantt.working-days" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <ErrorBanner message={error} onDismiss={() => setError('')} />
      <section>
        <h3 style={{ margin: '0 0 6px', fontSize: 15 }}>Working days</h3>
        <WorkingDays days={calendar.working_days} busy={busy} onChange={isAdmin ? (days) => run(() => planAPI.setWorkspaceWorkingDays(org.id, days), 'Failed to set the working days') : undefined} />
        <p style={{ ...mutedStyle, margin: '6px 0 0' }}>
          Every project's plan in this workspace works these days: a task's duration and float count only them, and the Gantt shades the rest. Holidays the whole business takes are set on each project's plan, under Calendar.
          {isAdmin ? '' : ' A workspace admin sets the working days.'}
        </p>
      </section>
      <section>
        <h3 style={{ margin: '0 0 6px', fontSize: 15 }}>Time off</h3>
        <p style={{ ...mutedStyle, margin: '0 0 8px' }}>
          The days a member is away, on every project. A plan reports a task that asks for someone while they are away, as it does anyone asked for more than they can give.
          {isAdmin ? '' : ' You can record your own; a workspace admin records anyone’s.'}
        </p>
        {calendar.time_off.length === 0 ? (
          <span style={mutedStyle}>No time off recorded.</span>
        ) : (
          <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
            {calendar.time_off.map((t) => (
              <li key={t.id} data-time-off={t.id} style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13 }}>
                <b style={{ minWidth: 160 }}>{nameOf(t.assignee_id)}</b>
                <span>{spanLabel(t.start, t.end, today)}</span>
                <span style={{ ...mutedStyle, flex: 1 }}>{t.name}</span>
                {(isAdmin || t.assignee_id === currentUserId) && (
                  <button type="button" disabled={busy} aria-label={`Remove the time off of ${nameOf(t.assignee_id)}`} onClick={() => run(() => planAPI.removeTimeOff(org.id, t.id), 'Failed to remove the time off')} style={buttonStyle('ghost', { compact: true, disabled: busy })}>
                    ✕
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
        <form
          data-add-time-off
          onSubmit={(e) => {
            e.preventDefault();
            if (!who || !start) return;
            void run(async () => {
              await planAPI.addTimeOff(org.id, { assignee_id: who, start, end: end || start, name: name.trim() });
              setStart('');
              setEnd('');
              setName('');
            }, 'Failed to record the time off');
          }}
          style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap', marginTop: 10 }}
        >
          <select value={who} disabled={!isAdmin} onChange={(e) => setWho(e.target.value)} aria-label="Who is away" style={inputStyle}>
            <option value="">Who…</option>
            {members.map((m) => (
              <option key={m.user_id} value={m.user_id}>
                {m.user_name || m.user_email}
              </option>
            ))}
          </select>
          <input type="date" value={start} onChange={(e) => setStart(e.target.value)} aria-label="First day away" style={inputStyle} />
          <span style={mutedStyle}>to</span>
          <input type="date" value={end} min={start} onChange={(e) => setEnd(e.target.value)} aria-label="Last day away" style={inputStyle} />
          <input value={name} maxLength={80} onChange={(e) => setName(e.target.value)} placeholder="What for (optional)" aria-label="What the time off is for" style={{ ...inputStyle, flex: '1 1 160px' }} />
          <button type="submit" disabled={busy || !who || !start} style={buttonStyle('secondary', { disabled: busy || !who || !start })}>
            Add time off
          </button>
        </form>
      </section>
    </div>
  );
};
