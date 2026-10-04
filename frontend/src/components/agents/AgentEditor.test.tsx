import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { mockApi } from '../../test/mockApi';
import { AgentEditor, ALLOWED_TOOLS_REQUIRED } from './AgentEditor';
import { agentsAPI, providerSettingsAPI, AgentDef } from '../../api/client';
import { useAppStore } from '../../state/store';

// The editor talks to the agents and provider-settings endpoints; every
// client method is stubbed, and the tests answer the ones the editor calls.
vi.mock('../../api/client', async (orig) => mockApi(await orig()));

const agents = vi.mocked(agentsAPI);
const providers = vi.mocked(providerSettingsAPI);

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  vi.clearAllMocks();
  providers.list.mockResolvedValue({ data: [] } as any);
  agents.create.mockResolvedValue({ data: {} } as any);
  agents.update.mockResolvedValue({ data: {} } as any);
  agents.raw.mockResolvedValue({ data: { content: '' } } as any);
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

const render = async (el: React.ReactElement) => {
  await act(async () => {
    root.render(el);
  });
};

const agentFixture = (allowedTools: string[]): AgentDef =>
  ({
    id: 'agent-1',
    slug: 'reviewer',
    name: 'Reviewer',
    description: '',
    provider: 'claude-code',
    model: '',
    effort: '',
    write_mode: 'proposal',
    repo_access: false,
    locked: false,
    max_turns: 30,
    timeout_seconds: 600,
    allowed_tools: allowedTools,
    system_prompt: 'Review things.',
  }) as AgentDef;

const saveButton = (): HTMLButtonElement =>
  Array.from(container.querySelectorAll('button')).find((b) =>
    /Save changes|Create agent/.test(b.textContent || '')
  ) as HTMLButtonElement;

const toolsInput = (): HTMLInputElement =>
  Array.from(container.querySelectorAll('input')).find((i) =>
    (i.getAttribute('placeholder') || '').includes('mcp__openv__')
  ) as HTMLInputElement;

const type = (input: HTMLInputElement, value: string) => {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
};

// REQ-91: an agent with no tool allowlist cannot be saved. An empty list is
// not "no tools" — it is every tool the vendor CLI has.
test('save is blocked while the allowed-tools field is empty', async () => {
  await render(<AgentEditor agent={agentFixture([])} onSaved={vi.fn()} onCancel={vi.fn()} />);

  expect(saveButton().disabled).toBe(true);
  expect(toolsInput().getAttribute('aria-invalid')).toBe('true');
  expect(container.textContent).toContain('Required: an empty list means');

  // Whitespace is not an allowlist either.
  type(toolsInput(), '  ,  ');
  expect(saveButton().disabled).toBe(true);

  type(toolsInput(), 'mcp__openv__*');
  expect(saveButton().disabled).toBe(false);
  expect(toolsInput().getAttribute('aria-invalid')).toBe('false');

  await act(async () => {
    saveButton().click();
  });
  expect(agents.update).toHaveBeenCalledTimes(1);
  expect(agents.update.mock.calls[0][1].allowed_tools).toEqual(['mcp__openv__*']);
});

// The 400 the API answers with is what the editor shows, so the reason a save
// was refused reads the same wherever it comes from.
test('shows the API message when the server refuses the definition', async () => {
  agents.update.mockRejectedValue({ response: { data: { error: ALLOWED_TOOLS_REQUIRED } } });
  await render(
    <AgentEditor agent={agentFixture(['mcp__openv__*'])} onSaved={vi.fn()} onCancel={vi.fn()} />
  );

  await act(async () => {
    saveButton().click();
  });
  expect(container.textContent).toContain('requires allowed_tools');
});

// antigravity-cli ships behind its release feature (REQ-137, #379 bug 82): a
// workspace whose channel has not received it is not offered it, whether the
// picker lists the server's providers or its own fallback, while an agent
// already on it keeps it.
describe('the antigravity-cli gate', () => {
  const initialStore = useAppStore.getState();
  const gates = (on: boolean) =>
    useAppStore.setState({
      features: { channel: 'stable', stable_release: '0.6.0', preview: false, features: { 'antigravity-cli': on } },
    });
  const providerOptions = () =>
    Array.from(container.querySelectorAll<HTMLSelectElement>('select'))
      .find((s) => Array.from(s.options).some((o) => o.value === 'claude-code'))!;
  const offered = () => Array.from(providerOptions().options).map((o) => o.value);
  const SERVER_LIST = ['claude-code', 'gemini-cli', 'antigravity-cli'].map((provider) => ({ provider, available_models: [] }));

  afterEach(() => {
    useAppStore.setState(initialStore, true);
  });

  test('a new agent is not offered it until the feature is on', async () => {
    gates(false);
    // The fallback list, before (or without) the server's.
    await render(<AgentEditor agent={null} onSaved={vi.fn()} onCancel={vi.fn()} />);
    expect(offered()).not.toContain('antigravity-cli');
    expect(offered()).toContain('gemini-cli');

    // The server's list.
    providers.list.mockResolvedValue({ data: SERVER_LIST } as any);
    act(() => root.unmount());
    act(() => {
      root = createRoot(container);
    });
    await render(<AgentEditor agent={null} onSaved={vi.fn()} onCancel={vi.fn()} />);
    expect(offered()).toEqual(['claude-code', 'gemini-cli']);

    await act(async () => {
      gates(true);
    });
    expect(offered()).toEqual(['claude-code', 'gemini-cli', 'antigravity-cli']);
  });

  test('an agent already on it keeps it with the feature off', async () => {
    gates(false);
    providers.list.mockResolvedValue({ data: SERVER_LIST } as any);
    const agent = { ...agentFixture(['mcp__openv__*']), provider: 'antigravity-cli' } as AgentDef;
    await render(<AgentEditor agent={agent} onSaved={vi.fn()} onCancel={vi.fn()} />);
    expect(offered()).toContain('antigravity-cli');
    expect(providerOptions().value).toBe('antigravity-cli');
  });
});
