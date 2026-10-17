import { lazy } from 'react';
import { CAPABILITY_BILLING, CAPABILITY_HOSTED } from '../capabilities';
import { registerAdminSection, registerOrgSettingsTab, registerStartHook } from './registry';
import { sendVisitBeacon } from '../site/visitBeacon';

// The modules this build links in (ext/registry.ts). A build that links
// other modules replaces this file. The hosted service's screens register
// here until they move to its own repository
// (docs/plans/hosted-edition-separation.md, Phase 4), when this file is left
// registering nothing.

registerOrgSettingsTab({
  key: 'billing',
  label: 'Billing',
  after: 'limits',
  capabilities: [CAPABILITY_HOSTED, CAPABILITY_BILLING],
  Component: lazy(() => import('../components/org/OrgBillingTab').then((m) => ({ default: m.OrgBillingTab }))),
});

registerAdminSection({
  key: 'visitors',
  capabilities: [CAPABILITY_HOSTED],
  Component: lazy(() =>
    import('../components/admin/VisitorDashboard').then((m) => ({ default: m.VisitorDashboard }))
  ),
});

registerStartHook({ key: 'visit-beacon', capabilities: [CAPABILITY_HOSTED], run: sendVisitBeacon });
