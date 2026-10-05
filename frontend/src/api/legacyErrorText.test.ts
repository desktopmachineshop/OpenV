import { apiErrorMessage, legacyErrorText } from './errors';

// legacyErrorText against the inline chain it replaces (quirk Q20), the one
// arch/errorChains.test.ts counts, written out the way 51 of its 53 call
// sites write it (RunDetailPanel.tsx:220, for one):
//
//   err.response?.data?.error || err.message || 'Failed to load run'
//
// Every row runs the chain itself beside the helper and expects both to give
// the same value, which the row also names. X15b-X15e move call sites onto
// the helper; their own tests are the guard there.

const inlineChain = (err: any, fallback: string) => err.response?.data?.error || err.message || fallback;

const FALLBACK = 'Failed to load run';
const objectError = { detail: 'not a string' };

const cases: [string, unknown, unknown][] = [
  ['the API error envelope', { response: { data: { error: 'run not found' } }, message: 'Request failed with status code 404' }, 'run not found'],
  ['an envelope with a code as well', { response: { data: { error: 'plan is read-only', code: 'plan_read_only' } }, message: 'm' }, 'plan is read-only'],
  ['an envelope whose error is empty', { response: { data: { error: '' } }, message: 'Request failed with status code 500' }, 'Request failed with status code 500'],
  ['a plain-text body, which the chain does not read', { response: { data: 'gateway timeout' }, message: 'Request failed with status code 504' }, 'Request failed with status code 504'],
  ['a { message } body, which the chain does not read', { response: { data: { message: 'invalid token' } }, message: 'Request failed with status code 401' }, 'Request failed with status code 401'],
  ['a body with neither', { response: { data: { code: 'x' } }, message: 'Request failed with status code 400' }, 'Request failed with status code 400'],
  ['a null body', { response: { data: null }, message: 'Request failed with status code 502' }, 'Request failed with status code 502'],
  ['an empty body', { response: { data: '' }, message: 'Request failed with status code 503' }, 'Request failed with status code 503'],
  ['no response (a network error)', { message: 'Network Error' }, 'Network Error'],
  ['a null response', { response: null, message: 'Network Error' }, 'Network Error'],
  ['an Error', new Error('boom'), 'boom'],
  ['an Error with an empty message', new Error(''), FALLBACK],
  ['an envelope and no message', { response: { data: { error: 'denied' } } }, 'denied'],
  ['nothing at all', {}, FALLBACK],
  ['an empty message', { message: '' }, FALLBACK],
  ['a thrown string', 'oops', FALLBACK],
  ['a thrown number', 42, FALLBACK],
  ['a thrown false', false, FALLBACK],
  ['a non-string error, passed on as it is', { response: { data: { error: objectError } }, message: 'm' }, objectError],
  ['a numeric error, passed on as it is', { response: { data: { error: 409 } }, message: 'm' }, 409],
  ['a zero error, which is falsy', { response: { data: { error: 0 } }, message: 'm' }, 'm'],
  ['a non-string message, passed on as it is', { message: 7 }, 7],
];

describe('legacyErrorText', () => {
  it.each(cases)('gives what the inline chain gives for %s', (_, err, want) => {
    expect(inlineChain(err, FALLBACK)).toBe(want);
    expect(legacyErrorText(err, FALLBACK)).toBe(want);
  });

  it('returns the fallback it is given, even an empty one', () => {
    expect(legacyErrorText({}, `Failed to ${'approve'} proposal`)).toBe('Failed to approve proposal');
    expect(inlineChain({}, '')).toBe('');
    expect(legacyErrorText({}, '')).toBe('');
  });

  it.each([
    ['null', null],
    ['undefined', undefined],
  ])('throws a TypeError for a %s error, as the chain does', (_, err) => {
    expect(() => inlineChain(err, FALLBACK)).toThrow(TypeError);
    expect(() => legacyErrorText(err, FALLBACK)).toThrow(TypeError);
  });

  it('reads response, data and error once each, then message only when it must, as the chain does', () => {
    const reads = (body: unknown) => {
      const log: string[] = [];
      const data = { get error() { log.push('error'); return body; } };
      const response = { get data() { log.push('data'); return data; } };
      const err = {
        get response() { log.push('response'); return response; },
        get message() { log.push('message'); return 'm'; },
      };
      return { err, log };
    };
    for (const [body, want] of [
      ['denied', ['response', 'data', 'error']],
      ['', ['response', 'data', 'error', 'message']],
    ] as const) {
      const chain = reads(body);
      const helper = reads(body);
      inlineChain(chain.err, FALLBACK);
      legacyErrorText(helper.err, FALLBACK);
      expect(chain.log).toEqual(want);
      expect(helper.log).toEqual(want);
    }
  });

  // Why the helper exists rather than apiErrorMessage at these sites: the
  // two differ on the bodies the chain skips, so moving a site to
  // apiErrorMessage would change what it shows.
  it.each([
    ['a plain-text body', { response: { data: 'gateway timeout' }, message: 'Request failed with status code 504' }],
    ['a { message } body', { response: { data: { message: 'invalid token' } }, message: 'Request failed with status code 401' }],
  ])('differs from apiErrorMessage on %s (Q20)', (_, err) => {
    expect(legacyErrorText(err, FALLBACK)).toBe(err.message);
    expect(apiErrorMessage(err, FALLBACK)).not.toBe(err.message);
  });
});
