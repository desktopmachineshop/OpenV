// Where a sign-in goes back to. A page that needs a session sends someone
// to /login?next=<its own address> (today the consent page an AI app opens,
// REQ-44); a password sign-in goes straight back there, and a single
// sign-on, which leaves the app for the provider and comes back to "/", finds
// it here in sessionStorage. Only a path inside the app is ever honoured, so
// a crafted link cannot send a fresh session to another site.

const RETURN_TO_KEY = 'openv_return_to';

/** path when it is an address inside the app, else "". */
export const safeReturnPath = (path: string | null | undefined): string => {
  if (!path || !path.startsWith('/') || path.startsWith('//') || path.startsWith('/\\')) return '';
  return path;
};

/** Keep path for after a single sign-on round trip. */
export const rememberReturnTo = (path: string): void => {
  const safe = safeReturnPath(path);
  if (!safe) return;
  try {
    sessionStorage.setItem(RETURN_TO_KEY, safe);
  } catch {
    // Storage unavailable: the member lands on the projects list instead.
  }
};

/** The path a single sign-on should go back to, or "". */
export const pendingReturnTo = (): string => {
  try {
    return safeReturnPath(sessionStorage.getItem(RETURN_TO_KEY));
  } catch {
    return '';
  }
};

/** Forget it, once the page it named is reached. */
export const forgetReturnTo = (): void => {
  try {
    sessionStorage.removeItem(RETURN_TO_KEY);
  } catch {
    // Nothing kept.
  }
};
