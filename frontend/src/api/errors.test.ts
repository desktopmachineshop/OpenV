import { apiErrorMessage } from './errors';

describe('apiErrorMessage', () => {
  it('prefers the backend { error } body', () => {
    const err = { response: { data: { error: 'name already taken' } }, message: 'Request failed with status code 409' };
    expect(apiErrorMessage(err)).toBe('name already taken');
  });

  it('never renders "[object Object]" for object bodies without error/message', () => {
    const err = { response: { data: { code: 500 } }, message: 'Request failed with status code 500' };
    expect(apiErrorMessage(err)).toBe('Request failed with status code 500');
  });

  it('uses a plain string body as-is', () => {
    const err = { response: { data: 'forbidden' }, message: 'Request failed with status code 403' };
    expect(apiErrorMessage(err)).toBe('forbidden');
  });

  it('uses a { message } body when there is no error field', () => {
    const err = { response: { data: { message: 'invalid token' } }, message: 'boom' };
    expect(apiErrorMessage(err)).toBe('invalid token');
  });

  it('falls back to err.message for network errors', () => {
    expect(apiErrorMessage(new Error('Network Error'))).toBe('Network Error');
  });

  it('falls back to the provided default otherwise', () => {
    expect(apiErrorMessage({}, 'Sign-in failed')).toBe('Sign-in failed');
    expect(apiErrorMessage(null)).toBe('Request failed');
  });
});
