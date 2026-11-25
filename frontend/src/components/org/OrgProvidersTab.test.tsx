import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { OrgProvidersTab } from './OrgProvidersTab';
import type { ProviderSetting } from '../../api/client';

const setting = (provider: string, auth_mode: ProviderSetting['auth_mode']): ProviderSetting =>
  ({
    id: provider,
    org_id: 'org-1',
    provider,
    auth_mode,
    api_key_env: '',
    default_model: '',
    enabled: true,
    last_detected: {},
    updated_at: '',
    available_models: [],
  }) as ProviderSetting;

vi.mock('../../api/client', async (orig) =>
  mockApi(await orig(), {
    providerSettingsAPI: {
      list: () => Promise.resolve({ data: [setting('gemini-cli', 'subscription-cli'), setting('google-api', 'api-key')] }),
      listKeys: () => Promise.resolve({ data: { enabled: true, self_hosted: false, keys: [] } }),
    },
  })
);
vi.mock('../../hooks/useFeature', () => ({ useFeature: () => true }));

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

// Google AI Studio has no sign-in: its card offers the key alone, under its
// own name, and says where to make one.
describe('the Google AI Studio provider card', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(async () => {
    container = document.createElement('div');
    document.body.appendChild(container);
    await act(async () => {
      root = createRoot(container);
      root.render(<OrgProvidersTab isAdmin />);
    });
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  const card = (title: string) =>
    Array.from(container.querySelectorAll('.card')).find((c) => c.querySelector('h3')?.textContent === title);

  it('is titled Google AI Studio, has no auth mode and links to AI Studio', () => {
    const aiStudio = card('Google AI Studio');
    expect(aiStudio).toBeTruthy();
    expect(aiStudio!.querySelector('select option[value="subscription-cli"]')).toBeNull();
    expect(aiStudio!.querySelector('[data-testid="ai-studio-note"] a')?.getAttribute('href')).toBe(
      'https://aistudio.google.com/apikey'
    );
  });

  it('leaves the Gemini CLI card its auth mode', () => {
    const gemini = card('gemini-cli');
    expect(gemini?.querySelector('select option[value="subscription-cli"]')).toBeTruthy();
    expect(gemini?.querySelector('[data-testid="ai-studio-note"]')).toBeNull();
  });
});
