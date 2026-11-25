import type { FeatureKey } from '../../generated/contract';

/**
 * The google-api provider is offered as Google AI Studio (REQ-137 gate):
 * agents on it run on the workspace's AI Studio key in every project.
 */
export const GOOGLE_AI_STUDIO_FEATURE: FeatureKey = 'google-ai-studio';

/** Where an admin makes a Google AI Studio key. */
export const AI_STUDIO_KEY_URL = 'https://aistudio.google.com/apikey';

// The names the UI shows where a provider id is not self-explanatory. The
// id itself stays the value saved in an agent definition.
const PROVIDER_LABELS: Record<string, string> = {
  'google-api': 'Google AI Studio',
};

/** The provider's display name: its label, or the id itself. */
export const providerLabel = (provider: string): string => PROVIDER_LABELS[provider] || provider;

/** Providers that run only on a workspace API key and have no sign-in. */
export const alwaysAPIKey = (provider: string): boolean => provider === 'google-api';
