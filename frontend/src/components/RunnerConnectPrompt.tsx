import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ConnectorPairing, connectorAPI, myRunnerKeyAPI } from '../api/client';

interface RunnerConnectPromptProps {
  orgId: string;
  onClose: () => void;
  // What triggered the prompt, e.g. "Your agent run is queued".
  reason?: string;
}

// Shown when an agent run can't find an active runner. Tries to open the
// locally installed OpenV Agent Connector via its openv-connector:// link;
// if nothing answers, offers pairing (one-time code) and download. Polls the
// member's runner status until it comes online.
export const RunnerConnectPrompt: React.FC<RunnerConnectPromptProps> = ({
  orgId,
  onClose,
  reason,
}) => {
  const [phase, setPhase] = useState<'opening' | 'waiting' | 'connected'>('opening');
  const [pairing, setPairing] = useState<ConnectorPairing | null>(null);
  const [error, setError] = useState('');
  const frameRef = useRef<HTMLIFrameElement | null>(null);

  // Fire the protocol link through a hidden iframe: no navigation, and
  // browsers without a handler fail silently instead of showing an error page.
  const openConnector = useCallback((link: string) => {
    if (frameRef.current) {
      frameRef.current.src = link;
    } else {
      window.location.href = link;
    }
  }, []);

  useEffect(() => {
    const t = window.setTimeout(() => openConnector(connectorAPI.startLink), 300);
    const fallback = window.setTimeout(
      () => setPhase((p) => (p === 'opening' ? 'waiting' : p)),
      3000
    );
    return () => {
      window.clearTimeout(t);
      window.clearTimeout(fallback);
    };
  }, [openConnector]);

  // Poll until this member's personal runner shows up online.
  useEffect(() => {
    if (phase === 'connected') return;
    const poll = window.setInterval(async () => {
      try {
        const res = await myRunnerKeyAPI.get(orgId);
        if (res.data.online) setPhase('connected');
      } catch {
        // Transient — keep polling.
      }
    }, 3000);
    return () => window.clearInterval(poll);
  }, [orgId, phase]);

  const createPairing = async () => {
    setError('');
    try {
      const res = await connectorAPI.createPairing(orgId);
      setPairing(res.data);
      openConnector(res.data.deep_link);
    } catch (err: any) {
      setError(`Failed to create a pairing code: ${err.response?.data || err.message}`);
    }
  };

  return (
    <div
      onClick={onClose}
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(44, 62, 80, 0.55)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 2000,
      }}
    >
      <iframe ref={frameRef} title="openv-connector" style={{ display: 'none' }} />
      <div
        className="card"
        onClick={(e) => e.stopPropagation()}
        style={{ width: 620, maxWidth: '92vw', background: '#fff', borderRadius: 8, padding: 24, margin: 0 }}
      >
        <h3 style={{ marginTop: 0, color: '#2c3e50' }}>
          {phase === 'connected' ? 'Runner connected' : 'No runner is online'}
        </h3>

        {error && (
          <div
            style={{
              background: '#fdecea',
              border: '1px solid #e74c3c',
              color: '#c0392b',
              padding: '10px 14px',
              borderRadius: 4,
              marginBottom: 12,
              fontSize: 13,
            }}
          >
            {error}
          </div>
        )}

        {phase === 'connected' ? (
          <>
            <p style={{ fontSize: 13, color: '#27ae60', fontWeight: 600 }}>
              ✓ Your personal runner is online — queued runs will start now.
            </p>
            <div style={{ textAlign: 'right' }}>
              <button className="button" style={{ width: 'auto' }} onClick={onClose}>
                Done
              </button>
            </div>
          </>
        ) : (
          <>
            <p style={{ fontSize: 13, color: '#7f8c8d', marginBottom: 14 }}>
              {reason || 'This run needs a runner.'} Agents execute on your own machine with your
              own AI subscription via the OpenV Agent Connector.
            </p>

            <div
              style={{
                background: '#f8f9fa',
                border: '1px solid #eee',
                borderRadius: 4,
                padding: '10px 14px',
                fontSize: 13,
                color: '#2c3e50',
                marginBottom: 14,
              }}
            >
              {phase === 'opening' ? (
                <>Opening your OpenV Agent Connector… allow the browser prompt if one appears.</>
              ) : (
                <>
                  Waiting for a runner to come online. If the connector didn&apos;t open, it may not
                  be installed or paired yet — use the options below.
                </>
              )}
            </div>

            <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', marginBottom: 16 }}>
              <button
                className="button"
                style={{ width: 'auto' }}
                onClick={() => openConnector(connectorAPI.startLink)}
              >
                Open connector
              </button>
              <button className="button-secondary button" style={{ width: 'auto' }} onClick={createPairing}>
                Pair connector
              </button>
              <a
                className="button-secondary button"
                style={{ width: 'auto', textDecoration: 'none', display: 'inline-block' }}
                href={connectorAPI.downloadURL('windows')}
              >
                Download for Windows
              </a>
            </div>

            {pairing && (
              <div
                style={{
                  background: '#eaf4fd',
                  border: '1px solid #3498db',
                  borderRadius: 4,
                  padding: '12px 14px',
                  fontSize: 13,
                  color: '#2c3e50',
                  marginBottom: 16,
                }}
              >
                <div style={{ fontWeight: 600, marginBottom: 6 }}>Pairing started</div>
                <div style={{ marginBottom: 8 }}>
                  Your connector should open and pair automatically. If not, run it manually with
                  this one-time code (valid until{' '}
                  {new Date(pairing.expires_at).toLocaleTimeString()}):
                </div>
                <pre
                  style={{
                    background: '#2c3e50',
                    color: '#ecf0f1',
                    borderRadius: 4,
                    padding: '8px 12px',
                    fontSize: 12,
                    overflowX: 'auto',
                    margin: 0,
                  }}
                >
                  {`openv-connector.exe "${pairing.deep_link}"`}
                </pre>
              </div>
            )}

            <div style={{ fontSize: 12, color: '#7f8c8d', marginBottom: 16 }}>
              First time? Download the connector, unzip it somewhere permanent, double-click{' '}
              <code>openv-connector.exe</code> once to register it, then click “Pair connector”
              here. Your CLI sign-ins never leave your machine.
            </div>

            <div style={{ textAlign: 'right' }}>
              <button className="button-secondary button" style={{ width: 'auto' }} onClick={onClose}>
                Close
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
};
