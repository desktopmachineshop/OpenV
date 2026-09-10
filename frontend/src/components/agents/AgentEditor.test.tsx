import React, { act } from 'react';
import { createRoot, Root } from 'react-dom/client';
import { AgentEditor, ALLOWED_TOOLS_REQUIRED } from './AgentEditor';
import { agentsAPI, providerSettingsAPI, AgentDef } from '../../api/client';

// The editor talks to the agents and provider-settings endpoints; the client
// module builds an axios instance at import time, so it is mocked wholesale.
jest.mock('../../api/client', () => ({
  agentsAPI: { create: jest.fn(), update: jest.fn(), raw: jest.fn() },
  providerSettingsAPI: { list: jest.fn() },
}));

const agents = agentsAPI as jest.Mocked<typeof agentsAPI>;
const providers = providerSettingsAPI as jest.Mocked<typeof providerSettingsAPI>;

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  jest.clearAllMocks();
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
  await render(<AgentEditor agent={agentFixture([])} onSaved={jest.fn()} onCancel={jest.fn()} />);

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
    <AgentEditor agent={agentFixture(['mcp__openv__*'])} onSaved={jest.fn()} onCancel={jest.fn()} />
  );

  await act(async () => {
    saveButton().click();
  });
  expect(container.textContent).toContain('requires allowed_tools');
});
