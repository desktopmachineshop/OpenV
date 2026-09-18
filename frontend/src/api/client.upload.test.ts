import { describe, it, expect, vi, beforeEach } from 'vitest';

/**
 * Uploads must not inherit the client's JSON timeout.
 *
 * The axios instance carries a 60 s timeout sized for JSON calls, and axios
 * counts that as wall-clock across the whole request — the body included. On
 * a multipart upload that stops being a server deadline and becomes a cap on
 * how long the member's connection has to push the file, so the real ceiling
 * became their upstream bandwidth rather than the size limit the workspace's
 * plan publishes. A file inside the plan's limit failed with
 * "timeout of 60000ms exceeded" and the API never saw the request at all.
 *
 * These tests pin the two halves of the fix: every upload sends timeout 0,
 * and every upload that takes a progress callback reports real numbers to it.
 */

type UploadConfig = {
  timeout?: number;
  onUploadProgress?: (event: { loaded: number; total?: number }) => void;
};

const post = vi.fn(
  (_url: string, _body?: unknown, _config?: UploadConfig) => Promise.resolve({ data: {} })
);

vi.mock('axios', () => {
  const instance = {
    post,
    get: vi.fn(() => Promise.resolve({ data: {} })),
    put: vi.fn(() => Promise.resolve({ data: {} })),
    patch: vi.fn(() => Promise.resolve({ data: {} })),
    delete: vi.fn(() => Promise.resolve({ data: {} })),
    interceptors: {
      request: { use: vi.fn() },
      response: { use: vi.fn() },
    },
  };
  return { default: { create: () => instance } };
});

/** The config object an upload handed to axios, for the last call made. */
const lastConfig = (): UploadConfig => {
  const call = post.mock.calls[post.mock.calls.length - 1];
  const config = call?.[2];
  if (!config) throw new Error('the upload passed axios no config at all');
  return config;
};

/** Feed the axios progress callback one event, as the browser would. */
const report = (event: { loaded: number; total?: number }) => {
  const onUploadProgress = lastConfig().onUploadProgress;
  if (!onUploadProgress) throw new Error('the upload registered no progress handler');
  onUploadProgress(event);
};

const file = () => new File(['x'], 'drawing.step', { type: 'application/octet-stream' });

describe('file uploads', () => {
  beforeEach(() => {
    post.mockClear();
  });

  it('gives an attachment upload no timeout, rather than the 60s JSON one', async () => {
    const { attachmentAPI } = await import('./client');
    await attachmentAPI.upload('artifact-1', file());

    expect(lastConfig().timeout).toBe(0);
  });

  it('gives a new figure version no timeout either', async () => {
    const { attachmentAPI } = await import('./client');
    await attachmentAPI.uploadVersion('attachment-1', file());

    expect(lastConfig().timeout).toBe(0);
  });

  it('gives an evidence file no timeout either', async () => {
    const { evidenceAPI } = await import('./client');
    await evidenceAPI.uploadFile('bundle-1', file());

    expect(lastConfig().timeout).toBe(0);
  });

  it('reports upload progress as a whole percentage', async () => {
    const { attachmentAPI } = await import('./client');
    const seen: (number | null)[] = [];
    await attachmentAPI.upload('artifact-1', file(), (percent) => seen.push(percent));

    report({ loaded: 0, total: 200 });
    report({ loaded: 50, total: 200 });
    report({ loaded: 200, total: 200 });

    expect(seen).toEqual([0, 25, 100]);
  });

  it('rounds a fractional percentage rather than reporting a long decimal', async () => {
    const { attachmentAPI } = await import('./client');
    const seen: (number | null)[] = [];
    await attachmentAPI.upload('artifact-1', file(), (percent) => seen.push(percent));

    report({ loaded: 1, total: 3 });

    expect(seen).toEqual([33]);
  });

  it('reports null while the body size is unknown, not a percentage of nothing', async () => {
    const { attachmentAPI } = await import('./client');
    const seen: (number | null)[] = [];
    await attachmentAPI.upload('artifact-1', file(), (percent) => seen.push(percent));

    report({ loaded: 4096, total: undefined });

    expect(seen).toEqual([null]);
  });

  it('never reports more than 100, even if the body outruns its own total', async () => {
    const { attachmentAPI } = await import('./client');
    const seen: (number | null)[] = [];
    await attachmentAPI.upload('artifact-1', file(), (percent) => seen.push(percent));

    report({ loaded: 300, total: 200 });

    expect(seen).toEqual([100]);
  });

  it('leaves onUploadProgress unset when no caller asked for progress', async () => {
    const { attachmentAPI } = await import('./client');
    await attachmentAPI.upload('artifact-1', file());

    expect(lastConfig().onUploadProgress).toBeUndefined();
  });
});
