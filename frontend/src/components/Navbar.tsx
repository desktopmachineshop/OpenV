import React from 'react';

interface NavbarProps {
  title?: string;
  onSwitchProject?: () => void;
  showSwitchButton?: boolean;
}

export const Navbar: React.FC<NavbarProps> = ({
  title,
  onSwitchProject,
  showSwitchButton = false,
}) => {
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        padding: '16px 24px',
        backgroundColor: '#ffffff',
        borderBottom: '1px solid #ecf0f1',
        marginBottom: '24px',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
        <img
          src="/Images/logo.png"
          alt="OpenV Logo"
          style={{ height: '56px', width: 'auto' }}
        />
      </div>

      <div style={{ flex: 1, textAlign: 'center' }}>
        {title && (
          <h2 style={{ margin: 0, color: '#2c3e50', fontSize: '18px' }}>
            {title}
          </h2>
        )}
      </div>

      <div>
        {showSwitchButton && onSwitchProject && (
          <button
            onClick={onSwitchProject}
            style={{
              padding: '8px 16px',
              backgroundColor: '#95a5a6',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              fontSize: '14px',
            }}
          >
            ← Switch Project
          </button>
        )}
      </div>
    </div>
  );
};
