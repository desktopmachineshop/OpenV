import { useEffect, useState } from 'react';
import { IdentityLinkPreview, authAPI } from '../api/client';

// useIdentityLink resolves the link request a Google or OIDC sign-on left
// this browser holding (/login?link=<provider>), so the sign-in page can name
// the password account the link confirms. onFound runs once with the preview;
// error is set when the request has gone (expired, spent or never made).
export function useIdentityLink(
  provider: string,
  onFound: (preview: IdentityLinkPreview) => void
): { preview: IdentityLinkPreview | null; error: string } {
  const [preview, setPreview] = useState<IdentityLinkPreview | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!provider) return;
    let cancelled = false;
    authAPI
      .identityLink()
      .then((res) => {
        if (cancelled) return;
        setPreview(res.data);
        onFound(res.data);
      })
      .catch(() => {
        if (!cancelled)
          setError('That sign-in has expired. Sign in again, with your password or with the button below.');
      });
    return () => {
      cancelled = true;
    };
    // onFound is the caller's setter wrapper; the lookup runs once per provider.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [provider]);
  return { preview, error };
}
