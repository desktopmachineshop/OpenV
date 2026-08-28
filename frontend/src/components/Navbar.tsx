import React from 'react';
import { OrgSwitcher } from './OrgSwitcher';
import { UserMenu } from './UserMenu';

interface NavbarProps {
  title?: React.ReactNode;
  onSwitchProject?: () => void;
  showSwitchButton?: boolean;
  /** Show the workspace switcher (next to the logo) and the user menu (far right). */
  showWorkspaceControls?: boolean;
  baselineOptions?: { id: string; name: string }[];
  selectedBaselineId?: string;
  onBaselineChange?: (id: string) => void;
  onCaptureBaseline?: () => void;
  onDeleteBaseline?: (id: string) => void;
  onGenerateReport?: () => void;
  baselineDisabled?: boolean;
}

export const Navbar: React.FC<NavbarProps> = ({
  title,
  onSwitchProject,
  showSwitchButton = false,
  showWorkspaceControls = false,
  baselineOptions = [],
  selectedBaselineId = 'live',
  onBaselineChange,
  onCaptureBaseline,
  onDeleteBaseline,
  onGenerateReport,
  baselineDisabled = false,
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
        {showWorkspaceControls && <OrgSwitcher variant="light" />}
      </div>

      <div style={{ flex: 1, textAlign: 'center' }}>
        {title && (
          <div style={{ color: '#2c3e50' }}>
            {title}
          </div>
        )}
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
        {onBaselineChange && (
          <select
            value={selectedBaselineId}
            onChange={(e) => onBaselineChange(e.target.value)}
            disabled={baselineDisabled}
            style={{
              height: '36px',
              padding: '0 10px',
              borderRadius: '4px',
              border: '1px solid #bdc3c7',
              fontSize: '12px',
              backgroundColor: baselineDisabled ? '#f5f5f5' : 'white',
              cursor: baselineDisabled ? 'not-allowed' : 'pointer',
              minWidth: '180px',
            }}
          >
            <option value="live">Live Project</option>
            {baselineOptions.map((baseline) => (
              <option key={baseline.id} value={baseline.id}>
                {baseline.name}
              </option>
            ))}
          </select>
        )}
        {onDeleteBaseline && (
          <button
            onClick={() => onDeleteBaseline(selectedBaselineId)}
            disabled={baselineDisabled || selectedBaselineId === 'live'}
            style={{
              height: '36px',
              padding: '0 10px',
              backgroundColor: (baselineDisabled || selectedBaselineId === 'live') ? '#bdc3c7' : '#e74c3c',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: (baselineDisabled || selectedBaselineId === 'live') ? 'not-allowed' : 'pointer',
              fontSize: '12px',
            }}
            title="Delete selected baseline"
          >
            🗑
          </button>
        )}
        {onCaptureBaseline && (
          <button
            onClick={onCaptureBaseline}
            disabled={baselineDisabled}
            style={{
              height: '36px',
              padding: '0 12px',
              backgroundColor: baselineDisabled ? '#bdc3c7' : '#2ecc71',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: baselineDisabled ? 'not-allowed' : 'pointer',
              fontSize: '12px',
            }}
          >
            Capture Baseline
          </button>
        )}
        {onGenerateReport && (
          <button
            onClick={onGenerateReport}
            disabled={baselineDisabled}
            style={{
              height: '36px',
              padding: '0 12px',
              backgroundColor: baselineDisabled ? '#bdc3c7' : '#3b82f6',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: baselineDisabled ? 'not-allowed' : 'pointer',
              fontSize: '12px',
            }}
          >
            Generate Report
          </button>
        )}
        {showSwitchButton && onSwitchProject && (
          <button
            onClick={onSwitchProject}
            style={{
              height: '36px',
              padding: '0 16px',
              minWidth: '120px',
              backgroundColor: '#95a5a6',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              fontSize: '12px',
            }}
          >
            ← Switch Project
          </button>
        )}
        {showWorkspaceControls && <UserMenu variant="light" />}
      </div>
    </div>
  );
};
