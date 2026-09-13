import { Artifact, artifactAPI } from '../../api/client';
import { applySuggestionsToProject } from './applySuggestion';

// The apply path against a mocked API. What matters is which calls it makes
// and with what: an edit must not blank the fields it does not name, a move
// must renumber only what changed, a placed artifact must land where the
// card said, and a batch must see its own earlier writes.

jest.mock('../../api/client', () => ({
  artifactAPI: { create: jest.fn(), update: jest.fn() },
  productProfileAPI: { get: jest.fn(), update: jest.fn() },
}));

const api = artifactAPI as jest.Mocked<typeof artifactAPI>;

const art = (id: string, parent: string | null, order: number, extra: Partial<Artifact> = {}): Artifact =>
  ({
    id,
    ref: id.toUpperCase(),
    project_id: 'p',
    parent_id: parent,
    type: id.startsWith('hdg') ? 'heading' : 'requirement',
    title: `Title ${id}`,
    body: `Body ${id}`,
    sort_order: order,
    attributes: { verification_method: 'test' },
    version: 1,
    valid_from: '',
    valid_to: null,
    created_at: '',
    updated_at: '',
    ...extra,
  }) as Artifact;

const project = () => [
  art('hdg-1', null, 1, { type: 'heading', attributes: {} }),
  art('req-1', 'hdg-1', 1),
  art('req-2', 'hdg-1', 2),
  art('hdg-2', null, 2, { type: 'heading', attributes: {} }),
];

beforeEach(() => {
  jest.clearAllMocks();
  // The server echoes what it was sent, with the id kept.
  // The mocks answer with the data the code reads; the rest of an axios
  // response is not consulted.
  api.update.mockImplementation((async (id: string, payload: Partial<Artifact>) => ({
    data: { id, ...payload },
  })) as any);
  api.create.mockImplementation((async (payload: Partial<Artifact>) => ({
    data: { id: 'new-1', ref: 'REQ-9', ...payload },
  })) as any);
});

describe('editing an artifact from a card', () => {
  it('sends only the fields the card named, merging attributes', async () => {
    const results = await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [{ key: 'k', suggestion: { kind: 'edit', ref: 'req-1', body: 'New body', attributes: { priority: 'must' } } }]
    );
    expect(results).toEqual([null]);
    expect(api.update).toHaveBeenCalledTimes(1);
    const [id, payload] = api.update.mock.calls[0];
    expect(id).toBe('req-1');
    // No title: the API leaves it alone. Attributes: the old ones survive.
    expect(payload).toEqual({ body: 'New body', attributes: { verification_method: 'test', priority: 'must' } });
  });

  // A locked wizard entry knows its artifact by id, not by reference, and
  // the assistant is told it may name either.
  it('accepts an artifact id where a reference is expected', async () => {
    const results = await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [{ key: 'k', suggestion: { kind: 'edit', ref: 'req-2', title: 'Renamed' } }]
    );
    expect(results).toEqual([null]);
    expect(api.update.mock.calls[0][0]).toBe('req-2');
  });

  it('says so when the reference names nothing', async () => {
    const results = await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [{ key: 'k', suggestion: { kind: 'edit', ref: 'REQ-77', title: 'x' } }]
    );
    expect(results[0]).toContain('REQ-77');
    expect(api.update).not.toHaveBeenCalled();
  });
});

describe('moving an artifact from a card', () => {
  it('reparents the artifact and renumbers only what changed', async () => {
    const results = await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [{ key: 'k', suggestion: { kind: 'move', ref: 'REQ-1', parent: 'HDG-2' } }]
    );
    expect(results).toEqual([null]);
    // req-1 becomes hdg-2's only child at 1; nothing else under hdg-2 to
    // renumber, and its old siblings are not touched by this plan.
    expect(api.update).toHaveBeenCalledTimes(1);
    const [id, payload] = api.update.mock.calls[0];
    expect(id).toBe('req-1');
    expect(payload).toMatchObject({ parent_id: 'hdg-2', sort_order: 1, title: 'Title req-1', body: 'Body req-1' });
  });

  it('reorders among siblings by anchor', async () => {
    await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [{ key: 'k', suggestion: { kind: 'move', ref: 'REQ-2', before: 'REQ-1' } }]
    );
    const written = api.update.mock.calls.map(([id, p]) => [id, p.sort_order]);
    expect(written).toEqual([
      ['req-2', 1],
      ['req-1', 2],
    ]);
  });

  it('refuses a move under itself, and explains an anchor elsewhere', async () => {
    const results = await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [
        { key: 'a', suggestion: { kind: 'move', ref: 'HDG-1', parent: 'HDG-1' } },
        { key: 'b', suggestion: { kind: 'move', ref: 'REQ-1', parent: 'HDG-2', after: 'REQ-2' } },
      ]
    );
    expect(results[0]).toContain('under itself');
    expect(results[1]).toContain('REQ-2');
    expect(api.update).not.toHaveBeenCalled();
  });
});

describe('adding an artifact by place', () => {
  it('creates it as a draft under the named heading, last', async () => {
    const results = await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [
        {
          key: 'k',
          suggestion: {
            kind: 'artifact',
            type: 'test-case',
            title: 'Estop test',
            body: 'Press it.',
            attributes: { execution_method: 'manual' },
            parent: 'hdg-1',
          },
        },
      ]
    );
    expect(results).toEqual([null]);
    expect(api.create).toHaveBeenCalledTimes(1);
    expect(api.create.mock.calls[0][0]).toMatchObject({
      project_id: 'p',
      type: 'test-case',
      parent_id: 'hdg-1',
      sort_order: 3,
      attributes: { execution_method: 'manual', status: 'draft' },
    });
    // No anchor, so nothing to renumber.
    expect(api.update).not.toHaveBeenCalled();
  });

  it('places it after the named sibling', async () => {
    await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [{ key: 'k', suggestion: { kind: 'artifact', type: 'requirement', title: 'Between', parent: 'HDG-1', after: 'REQ-1' } }]
    );
    const written = api.update.mock.calls.map(([id, p]) => [id, p.sort_order]);
    // Created at 3, then moved to 2, pushing req-2 to 3.
    expect(written).toEqual([
      ['new-1', 2],
      ['req-2', 3],
    ]);
  });

  // A later card in the same batch must see what an earlier one did, or
  // "add A, then add B after A" would fail on B.
  it('lets a later card in the batch refer to an earlier one', async () => {
    let n = 0;
    api.create.mockImplementation((async (payload: Partial<Artifact>) => {
      n += 1;
      return { data: { id: `new-${n}`, ref: `REQ-${8 + n}`, ...payload } };
    }) as any);
    const results = await applySuggestionsToProject(
      { projectId: 'p', artifacts: project() },
      [
        { key: 'a', suggestion: { kind: 'artifact', type: 'requirement', title: 'A', parent: 'HDG-2' } },
        { key: 'b', suggestion: { kind: 'move', ref: 'REQ-9', parent: 'HDG-1', position: 'first' } },
      ]
    );
    expect(results).toEqual([null, null]);
    expect(api.update.mock.calls[0][0]).toBe('new-1');
    expect(api.update.mock.calls[0][1]).toMatchObject({ parent_id: 'hdg-1', sort_order: 1 });
  });
});
