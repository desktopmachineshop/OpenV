// The topic pages, in the order the footer lists them (types.ts says what a
// topic is). App.tsx routes each path by hand (K13); everything else that
// needs the list (the sitemap, llms.txt, pre-rendering, the footer) reads it
// from here.
import { BASELINES } from './baselines';
import { GLOSSARY } from './glossary';
import { REQIF } from './reqif';
import { SAFETY_CRITICAL } from './safetyCritical';
import { SELF_HOSTED } from './selfHosted';
import { TRACEABILITY } from './traceability';
import type { Topic } from './types';
import { VERIFICATION } from './verification';

export type { Topic, TopicSection } from './types';

export const TOPICS: readonly Topic[] = [
  TRACEABILITY,
  VERIFICATION,
  BASELINES,
  REQIF,
  SELF_HOSTED,
  SAFETY_CRITICAL,
  GLOSSARY,
];

export const getTopic = (path: string): Topic | undefined => TOPICS.find((t) => t.path === path);
