import { describe, it, expect, vi, beforeEach } from 'vitest';

/**
 * Import Project reads an OpenV JSON export or a ReqIF file. A .reqif or
 * .xml file is sent as ReqIF (format=reqif), so one that is not valid ReqIF
 * is reported as a ReqIF error rather than as malformed JSON; anything else
 * goes as JSON, as it always has.
 */

type PostConfig = { headers?: Record<string, string>; params?: Record<string, string> };

const post = vi.fn((_url: string, _body?: unknown, _config?: PostConfig) =>
  Promise.resolve({ data: { status: 'success', message: '', project_id: 'p1' } })
);

vi.mock('axios', () => {
  const instance = {
    post,
    get: vi.fn(() => Promise.resolve({ data: {} })),
    put: vi.fn(() => Promise.resolve({ data: {} })),
    patch: vi.fn(() => Promise.resolve({ data: {} })),
    delete: vi.fn(() => Promise.resolve({ data: {} })),
    interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } },
  };
  return { default: { create: () => instance } };
});

const lastCall = () => post.mock.calls[post.mock.calls.length - 1];

describe('projectAPI.import', () => {
  beforeEach(() => post.mockClear());

  it('sends a JSON export as JSON, with no format hint', async () => {
    const { projectAPI } = await import('./client');
    await projectAPI.import(new File(['{"project":{}}'], 'export.json'));
    const [url, body, config] = lastCall();
    expect(url).toBe('/api/v1/projects/import');
    expect(body).toBe('{"project":{}}');
    expect(config?.headers?.['Content-Type']).toBe('application/json');
    expect(config?.params).toBeUndefined();
  });

  it.each(['spec.reqif', 'SPEC.REQIF', 'from-doors.xml'])('sends %s as ReqIF', async (name) => {
    const { projectAPI } = await import('./client');
    await projectAPI.import(new File(['<REQ-IF/>'], name));
    const [, body, config] = lastCall();
    expect(body).toBe('<REQ-IF/>');
    expect(config?.headers?.['Content-Type']).toBe('application/xml');
    expect(config?.params).toEqual({ format: 'reqif' });
  });
});
