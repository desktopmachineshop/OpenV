import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { ProviderAPIKeyField, providerKeyEnv } from './ProviderAPIKeyField';
import { providerSettingsAPI } from '../../api/client';

vi.mock('../../api/client', async (orig) => mockApi(await orig()));

const providers = vi.mocked(providerSettingsAPI);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  act(() => {
    root = createRoot(container);
  });
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
});

const typeInto = (input: HTMLInputElement, value: string) => {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
};

describe('ProviderAPIKeyField', () => {
  it('names the variable each provider reads, unless the setting overrides it', () => {
    expect(providerKeyEnv('claude-code')).toBe('ANTHROPIC_API_KEY');
    expect(providerKeyEnv('codex-cli')).toBe('OPENAI_API_KEY');
    expect(providerKeyEnv('antigravity-cli')).toBe('GEMINI_API_KEY');
    expect(providerKeyEnv('gemini-cli', 'GOOGLE_API_KEY')).toBe('GOOGLE_API_KEY');
  });

  it('saves a pasted key and clears the field, showing only the last four characters', async () => {
    const stored = { key_env: 'ANTHROPIC_API_KEY', last4: 'WXYZ', updated_by: 'u', updated_at: '' };
    providers.setKey.mockResolvedValue({ data: stored } as any);
    const onSaved = vi.fn();
    act(() => {
      root.render(
        <ProviderAPIKeyField keyEnv="ANTHROPIC_API_KEY" enabled onSaved={onSaved} onRemoved={vi.fn()} />,
      );
    });
    const input = container.querySelector('input') as HTMLInputElement;
    expect(input.type).toBe('password');
    act(() => typeInto(input, '  fake-test-key-WXYZ '));
    await act(async () => {
      (container.querySelector('button[type="submit"]') as HTMLButtonElement).click();
    });
    expect(providers.setKey).toHaveBeenCalledWith('ANTHROPIC_API_KEY', 'fake-test-key-WXYZ');
    expect(onSaved).toHaveBeenCalledWith(stored);
    expect(input.value).toBe('');

    act(() => {
      root.render(
        <ProviderAPIKeyField keyEnv="ANTHROPIC_API_KEY" stored={stored} enabled onSaved={onSaved} onRemoved={vi.fn()} />,
      );
    });
    expect(container.textContent).toContain('ending WXYZ');
    expect(container.textContent).not.toContain('fake-test-key');
  });

  it('says the server cannot store keys when it has no secrets key', () => {
    act(() => {
      root.render(
        <ProviderAPIKeyField keyEnv="OPENAI_API_KEY" enabled={false} showKeyEnv onSaved={vi.fn()} onRemoved={vi.fn()} />,
      );
    });
    expect(container.querySelector('input')).toBeNull();
    expect(container.textContent).toContain('OPENV_SECRETS_KEY');
  });

  it('names the key variable on a self-hosted deployment only', () => {
    act(() => {
      root.render(<ProviderAPIKeyField keyEnv="OPENAI_API_KEY" enabled onSaved={vi.fn()} onRemoved={vi.fn()} />);
    });
    expect(container.querySelector('label')?.textContent).toBe('API key');
    expect(container.textContent).not.toContain('OPENAI_API_KEY');
    act(() => {
      root.render(
        <ProviderAPIKeyField keyEnv="OPENAI_API_KEY" enabled={false} onSaved={vi.fn()} onRemoved={vi.fn()} />,
      );
    });
    expect(container.textContent).not.toContain('OPENAI_API_KEY');
    expect(container.textContent).not.toContain('OPENV_SECRETS_KEY');
    act(() => {
      root.render(
        <ProviderAPIKeyField keyEnv="OPENAI_API_KEY" enabled showKeyEnv onSaved={vi.fn()} onRemoved={vi.fn()} />,
      );
    });
    expect(container.querySelector('label')?.textContent).toBe('API key (OPENAI_API_KEY)');
  });
});
