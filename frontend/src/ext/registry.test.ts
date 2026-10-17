import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  orgSettingsTab,
  registerAdminSection,
  registerOrgSettingsTab,
  registerStartHook,
  registeredAdminSections,
  registeredStartHooks,
  resetRegistry,
  withOrgSettingsTabs,
} from './registry';

const Nothing = () => null;
const CORE = [
  { key: 'general', label: 'General' },
  { key: 'limits', label: 'Limits' },
  { key: 'usage', label: 'Usage' },
];

describe('the frontend extension registry', () => {
  afterEach(() => resetRegistry());

  it('places a tab after the one it names, and at the end when that is not there', () => {
    registerOrgSettingsTab({ key: 'billing', label: 'Billing', after: 'limits', Component: Nothing });
    registerOrgSettingsTab({ key: 'extra', label: 'Extra', after: 'missing', Component: Nothing });
    expect(withOrgSettingsTabs(CORE, null).map((t) => t.key)).toEqual(['general', 'limits', 'billing', 'usage', 'extra']);
    expect(orgSettingsTab('billing')?.label).toBe('Billing');
    expect(orgSettingsTab('general')).toBeUndefined();
  });

  it('shows an entry only where the deployment offers one of its capabilities', () => {
    registerOrgSettingsTab({ key: 'billing', label: 'Billing', capabilities: ['hosted', 'billing'], Component: Nothing });
    registerAdminSection({ key: 'visitors', capabilities: ['hosted'], Component: Nothing });
    registerAdminSection({ key: 'always', Component: Nothing });
    expect(withOrgSettingsTabs(CORE, []).map((t) => t.key)).toEqual(['general', 'limits', 'usage']);
    expect(withOrgSettingsTabs(CORE, ['billing']).map((t) => t.key)).toContain('billing');
    expect(registeredAdminSections([]).map((s) => s.key)).toEqual(['always']);
    // Before the server has said, everything shows, as hasCapability answers.
    expect(registeredAdminSections(null).map((s) => s.key)).toEqual(['visitors', 'always']);
  });

  it('runs the start hooks the deployment offers', () => {
    const beacon = vi.fn();
    registerStartHook({ key: 'beacon', capabilities: ['hosted'], run: beacon });
    for (const h of registeredStartHooks(['hosted'])) h.run('u1');
    for (const h of registeredStartHooks([])) h.run('u2');
    expect(beacon).toHaveBeenCalledExactlyOnceWith('u1');
  });

  it('leaves out a registered tab whose key is a core tab', () => {
    registerOrgSettingsTab({ key: 'limits', label: 'Other limits', Component: Nothing });
    expect(withOrgSettingsTabs(CORE, null).map((t) => t.label)).toEqual(['General', 'Limits', 'Usage']);
    expect(orgSettingsTab('limits', CORE)).toBeUndefined();
  });

  it('refuses a key registered twice', () => {
    registerAdminSection({ key: 'visitors', Component: Nothing });
    expect(() => registerAdminSection({ key: 'visitors', Component: Nothing })).toThrow(/registered twice/);
  });
});
