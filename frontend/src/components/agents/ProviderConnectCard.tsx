import React, { useCallback, useEffect, useRef, useState } from 'react';
import { providerLoginsAPI, ProviderLogin } from '../../api/client';
import { apiErrorMessage } from '../../api/errors';
import { useViewport } from '../../hooks/useViewport';

interface Props {
  provider: string;
  loggedIn: boolean;
  onComplete: () => void;
  // Where the sign-in executes: 'workspace' (shared worker, default) or
  // 'user' (only the caller's personal runner — credential lands on their
  // own machine).
  target?: 'workspace' | 'user';
  // Optional heading override (default "Subscription sign-in").
  title?: string;
}

/**
 * What the worker is asking the member to paste back. Two CLIs ask for two
 * different things: most hand over a short code, but a loopback flow
 * (Codex) asks for the whole redirected address. The distinction decides the
 * keyboard a phone opens — a URL keyboard for an address, plain text for a
 * code — so the worker reports it as `paste_kind`, which is the flow it is
 * driving rather than a guess.
 *
 * A worker older than that field says nothing, and the only clue left is the
 * prose it wrote for the member; sniffing that is the fallback, never the
 * first answer.
 */
export const pasteKind = (login: ProviderLogin | null | undefined): 'url' | 'text' => {
  if (!login) return 'text';
  if (login.paste_kind) return login.paste_kind === 'url' ? 'url' : 'text';
  return /address|\burl\b/i.test(login.detail || '') ? 'url' : 'text';
};

// ProviderConnectCard drives a CLI provider sign-in from the UI: it creates
// a login request, the host worker runs the vendor CLI's own login flow, and
// this component relays the auth URL (and paste-back code where the CLI
// needs one). Credentials are stored by the CLI on the host, never in OpenV.
//
// The relay is a phone job by design (REQ-108): the sign-in link opens in
// the phone's own browser, and what comes back is pasted here — usually from
// a password manager — so the field takes a paste, submits with the
// keyboard's send key, and offers a Paste button where the clipboard API is
// available.
export const ProviderConnectCard: React.FC<Props> = ({
  provider,
  loggedIn,
  onComplete,
  target = 'workspace',
  title = 'Subscription sign-in',
}) => {
  const { isPhone } = useViewport();
  const [login, setLogin] = useState<ProviderLogin | null>(null);
  const [code, setCode] = useState('');
  const [codeSent, setCodeSent] = useState(false);
  const [error, setError] = useState('');
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // The detail shown when the last code was submitted, or null when nothing
  // is in flight. The worker answers a refused code by re-issuing
  // awaiting_code with new wording ("… — paste the code again, in full"), so
  // a detail that has moved on is the signal that the field is wanted again.
  const sentDetailRef = useRef<string | null>(null);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  useEffect(() => stopPolling, [stopPolling]);

  const poll = useCallback(
    (id: string) => {
      stopPolling();
      pollRef.current = setInterval(async () => {
        try {
          const res = await providerLoginsAPI.get(id);
          setLogin(res.data);
          // Still waiting on a paste, but no longer for the reason it was
          // waiting when the member sent one: the worker has come back with
          // something to say about that code. Hand the field back rather
          // than leaving a disabled "Code sent…" as the last word.
          if (
            res.data.status === 'awaiting_code' &&
            sentDetailRef.current !== null &&
            res.data.detail !== sentDetailRef.current
          ) {
            sentDetailRef.current = null;
            setCodeSent(false);
          }
          if (['completed', 'failed', 'cancelled'].includes(res.data.status)) {
            stopPolling();
            if (res.data.status === 'completed') onComplete();
          }
        } catch {
          // transient; keep polling
        }
      }, 2000);
    },
    [onComplete, stopPolling]
  );

  const start = async () => {
    setError('');
    setCode('');
    setCodeSent(false);
    sentDetailRef.current = null;
    try {
      const res = await providerLoginsAPI.start(provider, target);
      setLogin(res.data);
      poll(res.data.id);
    } catch (err: any) {
      setError(apiErrorMessage(err));
    }
  };

  const submitCode = async () => {
    if (!login || !code.trim()) return;
    try {
      await providerLoginsAPI.submitCode(login.id, code.trim());
      sentDetailRef.current = login.detail;
      setCodeSent(true);
    } catch (err: any) {
      setError(apiErrorMessage(err));
    }
  };

  // A phone's keyboard sends with its own key, not with a button the
  // keyboard is covering — except mid-composition, where an IME's Enter
  // commits the candidate being typed and means nothing to this field.
  const onFieldKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
      e.preventDefault();
      submitCode();
    }
  };

  // Reading the clipboard needs a user gesture and a permission the browser
  // may refuse; when it is missing or refused the field is still there to
  // paste into by hand, so the failure is silent.
  const clipboardReadable =
    typeof navigator !== 'undefined' && !!navigator.clipboard && typeof navigator.clipboard.readText === 'function';

  const pasteFromClipboard = async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text && text.trim()) setCode(text.trim());
    } catch {
      // No permission, or an empty clipboard: leave the field to the member.
    }
  };

  const cancel = async () => {
    if (!login) return;
    try {
      const res = await providerLoginsAPI.cancel(login.id);
      setLogin(res.data);
    } finally {
      stopPolling();
    }
  };

  const active = login && ['pending', 'claimed', 'url_ready', 'awaiting_code'].includes(login.status);

  const statusLine = () => {
    if (!login) return null;
    switch (login.status) {
      case 'pending':
        return target === 'user'
          ? 'Waiting for your personal runner to pick this up…'
          : 'Waiting for the worker (agentd) to pick this up…';
      case 'claimed':
        return 'Starting the CLI sign-in on the worker host…';
      case 'url_ready':
      case 'awaiting_code':
        return login.detail;
      case 'completed':
        return 'Signed in successfully.';
      case 'cancelled':
        return 'Sign-in cancelled.';
      case 'failed':
        return login.detail || 'Sign-in failed.';
      default:
        return login.detail;
    }
  };

  const cancelButton = (
    <button
      onClick={cancel}
      style={{
        background: 'none',
        border: isPhone ? '1px solid var(--danger)' : 'none',
        borderRadius: isPhone ? 4 : undefined,
        color: 'var(--danger)',
        cursor: 'pointer',
        fontSize: isPhone ? 14 : 13,
        width: isPhone ? '100%' : 'auto',
        // 44 px for a finger, and never under the desktop click floor.
        minHeight: isPhone ? 44 : 26,
        padding: isPhone ? undefined : '4px 10px',
        marginTop: isPhone ? 8 : undefined,
      }}
    >
      Cancel
    </button>
  );

  const kind = pasteKind(login);
  // Controls sit in a full-width column on a phone (the .action-sheet
  // pattern) and keep their inline row on a pointer-sized screen.
  const actionClass = isPhone ? 'action-sheet' : '';
  const rowStyle: React.CSSProperties = isPhone
    ? { display: 'flex', flexDirection: 'column', gap: 8, marginTop: 8 }
    : { display: 'flex', gap: 8, marginTop: 8, alignItems: 'center' };

  return (
    <div
      style={{
        marginTop: 10,
        padding: '10px 12px',
        background: 'var(--surface-alt)',
        border: '1px solid var(--neutral-soft)',
        borderRadius: 6,
      }}
    >
      <div
        style={
          isPhone
            ? { display: 'flex', flexDirection: 'column', alignItems: 'stretch', gap: 8 }
            : { display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }
        }
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap', minWidth: 0 }}>
          <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text)' }}>{title}</span>
          {loggedIn && !active && (
            <span style={{ fontSize: 12, color: 'var(--success)', fontWeight: 600 }}>● connected</span>
          )}
        </div>
        {!isPhone && <div style={{ flex: 1 }} />}
        {!active ? (
          <button
            className="button"
            style={isPhone ? { minHeight: 44, width: '100%', fontSize: 14 } : { padding: '6px 14px', fontSize: 13 }}
            onClick={start}
          >
            {loggedIn ? 'Re-connect' : 'Connect'}
          </button>
        ) : (
          // On a phone Cancel goes last, under the steps it abandons; beside
          // the title it would outrank the sign-in link below it.
          !isPhone && cancelButton
        )}
      </div>

      {(login || error) && (
        <div style={{ marginTop: 8 }}>
          {error && <div style={{ color: 'var(--danger)', fontSize: 13 }}>{error}</div>}
          {login && (
            <div
              // 13 px and free to wrap: the worker's instructions are a
              // paragraph, and they are the whole screen on a phone.
              style={{
                fontSize: 13,
                lineHeight: 1.5,
                overflowWrap: 'anywhere',
                color:
                  login.status === 'failed'
                    ? 'var(--danger)'
                    : login.status === 'completed'
                    ? 'var(--success)'
                    : 'var(--text-muted)',
              }}
            >
              {statusLine()}
            </div>
          )}
          {login && active && login.auth_url && (
            // A real link, so a phone hands it to its own browser (and a
            // long press offers copy / open in a new tab).
            <a
              href={login.auth_url}
              target="_blank"
              rel="noopener noreferrer"
              className="button"
              style={
                isPhone
                  ? {
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      marginTop: 8,
                      minHeight: 44,
                      width: '100%',
                      fontSize: 14,
                      textDecoration: 'none',
                    }
                  : { display: 'inline-block', marginTop: 8, padding: '6px 14px', fontSize: 13, textDecoration: 'none' }
              }
            >
              Open sign-in page ↗
            </a>
          )}
          {login && login.status === 'awaiting_code' && (
            <div style={rowStyle}>
              <input
                value={code}
                onChange={(e) => setCode(e.target.value)}
                onKeyDown={onFieldKeyDown}
                aria-label={kind === 'url' ? 'Redirected address' : 'Authorization code'}
                placeholder={
                  kind === 'url' ? 'Paste the redirected address here' : 'Paste the authorization code here'
                }
                // Phone input hygiene: the right keyboard, no autocapitalise
                // or autocorrect mangling a code, and the send key submits.
                inputMode={kind === 'url' ? 'url' : 'text'}
                type="text"
                autoComplete="off"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                enterKeyHint="send"
                style={
                  isPhone
                    ? { width: '100%', minHeight: 44, fontSize: 16, padding: '8px 10px' }
                    : { flex: 1, padding: '6px 8px', fontSize: 13 }
                }
                disabled={codeSent}
              />
              <div className={actionClass} style={isPhone ? undefined : { display: 'flex', gap: 8 }}>
                {clipboardReadable && (
                  <button
                    className="button-secondary button"
                    style={isPhone ? undefined : { padding: '6px 14px', fontSize: 13, width: 'auto' }}
                    onClick={pasteFromClipboard}
                    disabled={codeSent}
                  >
                    Paste
                  </button>
                )}
                <button
                  className="button"
                  style={isPhone ? undefined : { padding: '6px 14px', fontSize: 13, width: 'auto' }}
                  onClick={submitCode}
                  disabled={codeSent || !code.trim()}
                >
                  {codeSent ? 'Code sent…' : kind === 'url' ? 'Submit address' : 'Submit code'}
                </button>
              </div>
            </div>
          )}
          {isPhone && active && cancelButton}
          {login && login.status === 'failed' && (
            <button
              className="button-secondary button"
              style={
                isPhone
                  ? { marginTop: 8, minHeight: 44, width: '100%', fontSize: 14 }
                  : { marginTop: 8, padding: '6px 14px', fontSize: 13, width: 'auto' }
              }
              onClick={start}
            >
              Retry
            </button>
          )}
        </div>
      )}
      {!login && !loggedIn && (
        <div style={{ fontSize: 12, color: 'var(--text-muted)', marginTop: 6 }}>
          {target === 'user'
            ? 'Signs into the vendor CLI on your own machine, using your own subscription. Your personal runner (Agent Connector) must be running.'
            : 'Signs into the vendor CLI on the machine running agentd, using your own subscription. The worker must be running.'}
        </div>
      )}
    </div>
  );
};
