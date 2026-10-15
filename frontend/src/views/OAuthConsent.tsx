import React, { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Org, OAuthConsentRequest, connectedAppsAPI, orgsAPI } from '../api/client';
import { apiErrorMessage } from '../api/errors';
import { forgetReturnTo } from '../utils/returnTo';

// The consent an AI app asks for when a member connects it to OpenV over
// the remote MCP endpoint (REQ-44). The app sent the browser to the API's
// /oauth/authorize, which checked the app and sent it on here with the
// request. The member, signed in, picks the workspace the app may act in
// and whether it may change anything there, then approves or declines; the
// API answers where to send the browser, back to the app with a code or
// with access_denied.

const READ = 'openv:read';
const READ_WRITE = 'openv:read openv:write';

const shell: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  background: 'var(--sidebar-bg)',
  padding: 16,
  boxSizing: 'border-box',
  overflowY: 'auto',
};

const card: React.CSSProperties = {
  width: '100%',
  maxWidth: 440,
  padding: 32,
  background: 'var(--surface)',
  borderRadius: 8,
  boxSizing: 'border-box',
  margin: 0,
};

const para: React.CSSProperties = { color: 'var(--text-body)', fontSize: 14, lineHeight: 1.5 };

export const OAuthConsent: React.FC = () => {
  const [params] = useSearchParams();
  const request = useMemo(
    () => ({
      clientId: params.get('client_id') || '',
      redirectUri: params.get('redirect_uri') || '',
      codeChallenge: params.get('code_challenge') || '',
      codeChallengeMethod: params.get('code_challenge_method') || '',
      state: params.get('state') || '',
      // The app may ask for read only; it is never given more than it asked.
      readOnlyAsked: (params.get('scope') || '').trim() === READ,
    }),
    [params]
  );
  const [app, setApp] = useState<OAuthConsentRequest | null>(null);
  const [orgs, setOrgs] = useState<Org[]>([]);
  const [orgId, setOrgId] = useState('');
  const [scope, setScope] = useState(request.readOnlyAsked ? READ : READ_WRITE);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const incomplete = !request.clientId || !request.redirectUri || !request.codeChallenge;

  useEffect(() => {
    // The sign-in that brought the member here is done with.
    forgetReturnTo();
    if (incomplete) return;
    connectedAppsAPI
      .consent(request.clientId, request.redirectUri)
      .then((res) => setApp(res.data))
      .catch((err) => setError(apiErrorMessage(err)));
    orgsAPI
      .list()
      .then((res) => {
        setOrgs(res.data.orgs);
        setOrgId(res.data.active_org || res.data.orgs[0]?.id || '');
      })
      .catch((err) => setError(apiErrorMessage(err)));
  }, [incomplete, request.clientId, request.redirectUri]);

  const decide = async (approve: boolean) => {
    setBusy(true);
    setError('');
    try {
      const res = await connectedAppsAPI.decide({
        client_id: request.clientId,
        redirect_uri: request.redirectUri,
        code_challenge: request.codeChallenge,
        code_challenge_method: request.codeChallengeMethod,
        state: request.state,
        scope,
        org_id: orgId,
        approve,
      });
      window.location.assign(res.data.redirect_to);
    } catch (err) {
      setError(apiErrorMessage(err));
      setBusy(false);
    }
  };

  return (
    <div className="app-shell" style={shell}>
      <div className="card" style={card}>
        {incomplete ? (
          <>
            <h1 style={{ margin: '0 0 8px', fontSize: 22, color: 'var(--text)' }}>This link is incomplete</h1>
            <p style={para}>Start connecting again from the AI app: it opens this page with what it needs.</p>
          </>
        ) : (
          <>
            <h1 style={{ margin: '0 0 8px', fontSize: 22, color: 'var(--text)' }}>
              Connect {app ? app.client_name : 'an app'} to OpenV
            </h1>
            <p style={para}>
              <b>{app ? app.client_name : 'The app'}</b> will act as you in the workspace you choose, through the same
              tools an OpenV agent uses. Everything it does is recorded as yours, and you can disconnect it at any time
              from your settings, under Connected apps.
            </p>
            {app && (
              <p style={{ ...para, fontSize: 13, color: 'var(--text-muted)' }}>
                After you answer you go back to <b>{app.redirect_host}</b>.
              </p>
            )}
            <div className="form-group" style={{ marginBottom: 12 }}>
              <label htmlFor="consent-workspace">Workspace</label>
              <select id="consent-workspace" value={orgId} onChange={(e) => setOrgId(e.target.value)} disabled={busy}>
                {orgs.map((o) => (
                  <option key={o.id} value={o.id}>
                    {o.name}
                  </option>
                ))}
              </select>
            </div>
            <fieldset style={{ border: 'none', padding: 0, margin: '0 0 16px' }}>
              <legend style={{ fontSize: 13, fontWeight: 600, marginBottom: 6 }}>What it may do</legend>
              {!request.readOnlyAsked && (
                <label style={{ display: 'block', fontSize: 14, marginBottom: 4 }}>
                  <input
                    type="radio"
                    name="consent-scope"
                    checked={scope === READ_WRITE}
                    onChange={() => setScope(READ_WRITE)}
                    disabled={busy}
                    style={{ width: 'auto', marginRight: 6 }}
                  />
                  Read and change: add and edit artifacts and links, record test results, comment
                </label>
              )}
              <label style={{ display: 'block', fontSize: 14 }}>
                <input
                  type="radio"
                  name="consent-scope"
                  checked={scope === READ}
                  onChange={() => setScope(READ)}
                  disabled={busy}
                  style={{ width: 'auto', marginRight: 6 }}
                />
                Read only
              </label>
            </fieldset>
            {error && (
              <div role="alert" style={{ color: 'var(--danger)', fontSize: 13, marginBottom: 12 }}>
                {error}
              </div>
            )}
            <div style={{ display: 'flex', gap: 8 }}>
              <button
                type="button"
                className="button"
                onClick={() => decide(true)}
                disabled={busy || !app || !orgId}
                style={{ flex: 1 }}
              >
                Connect
              </button>
              <button
                type="button"
                className="button-secondary"
                onClick={() => decide(false)}
                disabled={busy || !app}
                style={{ flex: 1 }}
              >
                Cancel
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
};
