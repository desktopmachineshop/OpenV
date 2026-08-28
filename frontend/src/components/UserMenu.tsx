import React, { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { authAPI } from '../api/client';
import { useAppStore } from '../state/store';
import { UserSettingsPanel } from './UserSettingsPanel';

interface UserMenuProps {
  /**
   * 'dark' fits the sidebar footer (#2c3e50): full-width row with the user's
   * name, menu opens upward. 'light' fits white page headers: compact avatar
   * trigger, menu opens downward and right-aligned.
   */
  variant?: 'dark' | 'light';
}

// User menu: avatar + identity trigger with a dropdown offering user settings
// and sign out. Shared between the project sidebar footer and the light
// top bars on org-level pages.
export const UserMenu: React.FC<UserMenuProps> = ({ variant = 'light' }) => {
  const navigate = useNavigate();
  const { currentUser, setCurrentUser } = useAppStore();
  const [menuOpen, setMenuOpen] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const dark = variant === 'dark';

  useEffect(() => {
    if (!menuOpen) return;
    const onClickOutside = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setMenuOpen(false);
      }
    };
    document.addEventListener('mousedown', onClickOutside);
    return () => document.removeEventListener('mousedown', onClickOutside);
  }, [menuOpen]);

  const handleLogout = async () => {
    try {
      await authAPI.logout();
    } finally {
      setCurrentUser(null);
      navigate('/login');
    }
  };

  const displayName = currentUser?.name || currentUser?.email || 'Not signed in';

  const avatar = currentUser?.avatar_url ? (
    <img
      src={currentUser.avatar_url}
      alt=""
      style={{ width: 28, height: 28, borderRadius: '50%' }}
    />
  ) : (
    <div
      style={{
        width: 28,
        height: 28,
        borderRadius: '50%',
        background: '#3498db',
        color: '#ecf0f1',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontSize: 13,
        fontWeight: 700,
        flexShrink: 0,
      }}
    >
      {(currentUser?.name || currentUser?.email || '?').charAt(0).toUpperCase()}
    </div>
  );

  const itemStyle: React.CSSProperties = {
    display: 'block',
    width: '100%',
    padding: '10px 14px',
    background: 'none',
    border: 'none',
    color: dark ? '#ecf0f1' : '#2c3e50',
    textAlign: 'left',
    cursor: 'pointer',
    fontSize: 13,
  };

  const dropdownStyle: React.CSSProperties = dark
    ? {
        position: 'absolute',
        bottom: 'calc(100% + 12px)',
        left: 0,
        right: 0,
        background: '#34495e',
        borderRadius: 6,
        overflow: 'hidden',
        boxShadow: '0 4px 12px rgba(0,0,0,0.3)',
      }
    : {
        position: 'absolute',
        top: 'calc(100% + 6px)',
        right: 0,
        minWidth: 200,
        background: '#fff',
        borderRadius: 6,
        border: '1px solid #e0e0e0',
        boxShadow: '0 6px 18px rgba(0,0,0,0.18)',
        zIndex: 1500,
        overflow: 'hidden',
      };

  return (
    <div ref={rootRef} style={{ position: 'relative' }}>
      <div
        style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}
        title={dark ? undefined : displayName}
        onClick={() => setMenuOpen(!menuOpen)}
      >
        {avatar}
        {dark ? (
          <div
            style={{
              fontSize: 13,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {displayName}
          </div>
        ) : (
          <span style={{ fontSize: 10, color: '#7f8c8d' }}>▼</span>
        )}
      </div>
      {menuOpen && (
        <div style={dropdownStyle}>
          {!dark && (
            <div
              style={{
                padding: '8px 14px',
                borderBottom: '1px solid #eee',
                fontSize: 12,
                color: '#7f8c8d',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                maxWidth: 260,
              }}
            >
              {displayName}
            </div>
          )}
          <button
            onClick={() => {
              setMenuOpen(false);
              setShowSettings(true);
            }}
            style={itemStyle}
          >
            Settings
          </button>
          <button onClick={handleLogout} style={itemStyle}>
            Sign out
          </button>
        </div>
      )}
      {showSettings && <UserSettingsPanel onClose={() => setShowSettings(false)} />}
    </div>
  );
};
