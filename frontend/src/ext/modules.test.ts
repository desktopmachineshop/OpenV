import { describe, expect, it } from 'vitest';
import './modules';
import { orgSettingsTab, registeredAdminSections, registeredStartHooks, withOrgSettingsTabs } from './registry';

// What this build links in: the hosted service's screens, each shown only
// where the deployment offers them, so a self-hosted OpenV has none.
describe('the modules this build registers', () => {
  const core = [
    { key: 'members', label: 'Members' },
    { key: 'limits', label: 'Limits' },
    { key: 'teams', label: 'Teams' },
  ];

  it('puts the Billing tab after Limits on the hosted service or wherever billing is offered', () => {
    expect(withOrgSettingsTabs(core, ['hosted']).map((t) => t.key)).toEqual(['members', 'limits', 'billing', 'teams']);
    expect(withOrgSettingsTabs(core, ['billing']).map((t) => t.key)).toContain('billing');
    expect(orgSettingsTab('billing')?.label).toBe('Billing');
  });

  it('adds nothing on a self-hosted deployment', () => {
    expect(withOrgSettingsTabs(core, []).map((t) => t.key)).toEqual(['members', 'limits', 'teams']);
    expect(registeredAdminSections([])).toEqual([]);
    expect(registeredStartHooks([])).toEqual([]);
  });

  it('adds the Visitors dashboard and the visit beacon on the hosted service', () => {
    expect(registeredAdminSections(['hosted']).map((s) => s.key)).toEqual(['visitors']);
    expect(registeredStartHooks(['hosted']).map((h) => h.key)).toEqual(['visit-beacon']);
  });
});
