import { useAppStore } from './state/store';

// What a deployment offers, as GET /api/v1/auth/config names it (ext's
// capabilities): the hosted service's screens (plans, billing, the public
// site, release channels, visitor analytics) show only where it offers
// them, so a self-hosted OpenV shows none of them.
export const CAPABILITY_HOSTED = 'hosted';
export const CAPABILITY_BILLING = 'billing';

/**
 * Whether this deployment offers `name`. Until the server has said, or if
 * it could not be asked, the answer is yes: the screens stay as they always
 * were rather than flicker away on a slow or failed read.
 */
export function hasCapability(capabilities: string[] | null, name: string): boolean {
  return capabilities === null || capabilities.includes(name);
}

export function useCapability(name: string): boolean {
  const capabilities = useAppStore((s) => s.capabilities);
  return hasCapability(capabilities, name);
}
