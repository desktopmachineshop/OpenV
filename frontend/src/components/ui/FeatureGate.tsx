import React from 'react';
import type { FeatureKey } from '../../generated/contract';
import { useAppStore } from '../../state/store';

// A page whose route exists for every workspace but whose content waits for a
// feature gate. Nothing renders until the gates have loaded, so a workspace
// on the stable channel never sees the page flash on; once they have, a
// workspace without the feature reads why the page is empty, rather than a
// blank screen, when it follows an old link or types the address.

interface FeatureGateProps {
  feature: FeatureKey;
  /** The page's name, as the menu writes it. */
  name: string;
  children: React.ReactNode;
}

export const FeatureGate: React.FC<FeatureGateProps> = ({ feature, name, children }) => {
  const features = useAppStore((s) => s.features);
  if (!features) return null;
  if (features.features[feature]) return <>{children}</>;
  return (
    <div style={{ padding: 16 }}>
      <div
        role="status"
        style={{
          maxWidth: 560,
          padding: '12px 14px',
          border: '1px solid var(--border)',
          borderRadius: 6,
          background: 'var(--surface)',
          color: 'var(--text-secondary)',
          fontSize: 14,
        }}
      >
        The {name} page is not in your workspace's release yet. It arrives with a later release of OpenV.
      </div>
    </div>
  );
};
