// Random product generator for the new-project wizard's testing mode: mints
// an amusing-but-workable product concept to seed the Guided Wizard, where
// the connected copilot agent takes it from framing to requirements.
//
// Every roll comes from ONE concept, so the parts hang together: the name
// riffs on the gimmick, the description states it, the vision is that
// product's ambition, the problem is why today's alternatives fail the
// audience, and the audience is the one the gimmick serves. Randomness picks
// the concept and its brand name, never the pieces of a sentence.

export interface RandomProduct {
  category: string;
  name: string;
  description: string;
  vision: string;
  problem: string;
  targetUsers: string;
}

interface Concept {
  category: string;
  /** Brandable names that all riff on this concept's gimmick. */
  names: string[];
  /** What the product is — a noun phrase that follows "A ". */
  what: string;
  /** What it does — a verb phrase that follows "that ". */
  does: string;
  /**
   * Who it is for — a noun phrase that follows "for ". Write the people this
   * exact gimmick serves, caught in the specific moment it fixes ("engineers
   * who reconstruct yesterday from browser history thirty seconds before
   * stand-up"), never a generic demographic that would fit any product.
   */
  audience: string;
  /** Why today's alternatives fail that audience. A complete sentence. */
  gap: string;
  /** The ambition — a noun phrase that follows "becomes ". */
  ambition: string;
}

const CONCEPTS: Concept[] = [
  {
    category: 'software',
    names: ['Yesterdaily', 'Standupp', 'Recapper'],
    what: 'stand-up meeting assistant',
    does: 'writes your daily update from your actual commit history and gently flags the days you did nothing',
    audience: 'engineers who reconstruct yesterday from browser history thirty seconds before stand-up',
    gap: 'Stand-up updates are reconstructed from memory at 9:01 a.m., which is why they are equal parts fiction and apology.',
    ambition: 'the two-minute ritual that makes stand-up honest without making it longer',
  },
  {
    category: 'software',
    names: ['Bailout', 'Exit Strategy', 'Phoney'],
    what: 'calendar assistant',
    does: 'invents a plausible, escalating excuse and calls you out of any meeting that runs twenty minutes over',
    audience: 'people whose 3pm quick sync has run twenty minutes over every week since March',
    gap: 'Leaving a meeting early takes either courage or a believable emergency, and almost nobody has one on hand at 4pm.',
    ambition: 'the escape hatch every over-booked calendar quietly needs',
  },
  {
    category: 'hardware',
    names: ['Fogoff', 'DeskCannon', 'Smokescreen'],
    what: 'desk-mounted fog machine and status lamp',
    does: 'releases a small dramatic fog cloud when your focus timer starts and glows green when you are interruptible',
    audience: 'open-plan developers whose deep work ends with a fourth cheerful "got a sec?" before lunch',
    gap: 'Headphones are ambiguous, do-not-disturb statuses are invisible from three desks away, and saying "I am busy" out loud all day is exhausting.',
    ambition: 'the availability signal readable from across the room, with theatre',
  },
  {
    category: 'hardware',
    names: ['Truce', 'Degree Zero', 'ThermoPeace'],
    what: 'thermostat with a voting panel',
    does: 'requires a quorum before anyone can change the temperature and keeps a log of who keeps trying',
    audience: 'households where one person wears a fleece indoors and another opens windows in February',
    gap: 'A thermostat has one setting and many stakeholders, so the coldest-blooded person wins by stealth every single afternoon.',
    ambition: 'the end of the thermostat cold war, complete with an audit trail',
  },
  {
    category: 'video game',
    names: ['Krakenpark', 'Leviathan Valet', 'Deep Reverse'],
    what: 'physics puzzle game',
    does: 'has you parallel-park increasingly enormous sea creatures into increasingly small harbours',
    audience: 'players who bounced off world-saving epics and want to be quietly excellent at reversing a blue whale into a fishing berth',
    gap: 'The store is full of games about saving the world and nearly empty of games about doing a small, strange task perfectly.',
    ambition: 'the game people sink sixty hours into and describe to friends as "you park a whale"',
  },
  {
    category: 'video game',
    names: ['Afterclerk', 'Spectral Services', 'HauntDesk'],
    what: 'cozy management game',
    does: 'puts you behind the counter of a permit office for ghosts who are extremely particular about paperwork',
    audience: 'people who unwind by sorting other people’s paperwork and would prefer the customers to be haunted',
    gap: 'Management games simulate empires; nobody serves the player who just wants a tidy desk and a satisfied customer.',
    ambition: 'the comfort game that makes admin work feel like a warm bath',
  },
  {
    category: 'toy',
    names: ['Mimicorn', 'EchoPal', 'Parrotplush'],
    what: 'plush axolotl with a hidden microphone',
    does: 'repeats the last thing it heard in a tiny, slightly wrong voice',
    audience: 'parents of six-year-olds who have just discovered that repeating everything back is comedy',
    gap: 'Talking toys cycle through the same forty phrases until the batteries die, and children find the pattern in an afternoon.',
    ambition: 'the toy that gets confiscated at bedtime and smuggled back by breakfast',
  },
  {
    category: 'toy',
    names: ['Sulk', 'Broodcloud', 'Moodlump'],
    what: 'plush storm cloud',
    does: 'glows brighter and rumbles louder the longer it is left abandoned on the floor',
    audience: 'families who step over the same three abandoned toys nightly on the way to the tidy-up argument',
    gap: 'Tidying up is a nightly argument because toys have no opinion about being left underfoot.',
    ambition: 'the toy that does the nagging so nobody in the house has to',
  },
  {
    category: 'kitchen appliance',
    names: ['Toastmood', 'Browning Point', 'Charmometer'],
    what: 'countertop toaster',
    does: 'asks how your morning is going and browns the bread to match, from "pale and hopeful" to "carbonised"',
    audience: 'people who set the dial to 4, get charcoal, and start every day negotiating with an appliance',
    gap: 'Toasters offer a numbered dial that means nothing, so every slice is a gamble placed before anyone is awake enough to gamble.',
    ambition: 'the appliance that finally admits toast is an emotional decision',
  },
  {
    category: 'kitchen appliance',
    names: ['Kevinproof', 'Beanwatch', 'Brewdential'],
    what: 'office coffee grinder with a fingerprint lock and a bean ledger',
    does: 'grinds only your beans, weighs every gram taken, and posts a weekly leaderboard of who took whose',
    audience: 'coffee-obsessed office workers locked in a passive-aggressive hunt for the perfect brew and for whoever keeps taking their beans',
    gap: 'The office kitchen runs on an honour system that collapses the moment someone brings in a good single-origin bag, and there is never any evidence — only suspicion and an empty jar.',
    ambition: 'the appliance that settles the office bean wars with data instead of passive-aggressive sticky notes',
  },
  {
    category: 'wearable',
    names: ['Sighence', 'Huffwatch', 'Exhale'],
    what: 'lapel pin',
    does: 'counts your sighs per hour and charts them against your calendar',
    audience: 'knowledge workers who suspect the recurring Thursday 2pm is why they feel like this, and want receipts',
    gap: 'Wearables obsess over steps and sleep while ignoring the vital signs of office survival.',
    ambition: 'the wearable that turns a vaguely terrible week into a chart you can show someone',
  },
  {
    category: 'pet tech',
    names: ['Petition', 'Meowdio', 'Grievance Bell'],
    what: 'paw-operated intercom',
    does: 'lets pets file formal complaints, which arrive as time-stamped voice notes on your phone',
    audience: 'cat owners who already answer out loud when the cat stares at a full bowl and screams',
    gap: 'Pets currently communicate by knocking things off tables — an unstructured feedback channel with no audit trail.',
    ambition: 'the official grievance procedure every household pet has been demanding for centuries',
  },
  {
    category: 'board game',
    names: ['Broth Runners', 'Souperior', 'Ladle & Order'],
    what: 'social deduction board game',
    does: 'has players smuggling soup across a fantasy border while one of them is secretly the customs inspector',
    audience: 'game groups who can recite each other’s tells after six years of the same three deduction games',
    gap: 'Every group has played the same three deduction games to death and now knows exactly how each other lies.',
    ambition: 'the game night that ends in laughter, betrayal, and one strongly worded house rule',
  },
  {
    category: 'garden tech',
    names: ['Gnomecast', 'Squirrelspotter', 'Lawn Commentary'],
    what: 'solar-powered garden gnome',
    does: 'live-narrates squirrel activity in the voice of an increasingly invested sports commentator',
    audience: 'gardeners who have named the squirrel that keeps defeating their bird-feeder engineering',
    gap: 'Garden pests get treated as a problem to be solved when they are, in fact, the best entertainment on the street.',
    ambition: 'the garden ornament people leave the kitchen window open for',
  },
  {
    category: 'robot',
    names: ['Crustodian', 'Slicekeeper', 'Leftover Watch'],
    what: 'palm-sized fridge robot',
    does: 'guards the last slice of pizza and reports, with photographic evidence, exactly who took it',
    audience: 'flatshares where the container says DO NOT EAT, in marker, and it gets eaten anyway',
    gap: 'A name written on a container is only a suggestion: the shared fridge is a lawless place with no witnesses.',
    ambition: 'the tiny robot that finally brings law to the shared fridge',
  },
];

// Vision patterns. Each reads correctly with an ambition phrased as a noun
// phrase, so the sentence stays coherent whichever concept is rolled.
const VISION_PATTERNS: ((name: string, ambition: string) => string)[] = [
  (name, ambition) => `${name} becomes ${ambition}.`,
  (name, ambition) => `Make ${name} ${ambition}.`,
];

const pick = <T>(items: T[]): T => items[Math.floor(Math.random() * items.length)];

export const generateRandomProduct = (): RandomProduct => {
  const concept = pick(CONCEPTS);
  const name = pick(concept.names);
  return {
    category: concept.category,
    name,
    description: `A ${concept.what} that ${concept.does}.`,
    vision: pick(VISION_PATTERNS)(name, concept.ambition),
    problem: concept.gap,
    targetUsers: concept.audience,
  };
};

// --------------------------------------------------------------------------
// Agent-invented products
// --------------------------------------------------------------------------
// The built-in concepts above are a fixed bucket, so they go stale as soon as
// you have seen them all. With a runner connected, the same brief that shaped
// those concepts is handed to the member's own agent, which invents something
// nobody has read before — including categories this file never imagined.

/**
 * The brief given to the agent. `avoid` carries concepts already shown in this
 * session so a fresh click keeps producing something new.
 */
export const inventProductPrompt = (avoid: string[]): string => {
  const avoidLine = avoid.length
    ? `\nAlready used in this session — invent something different from all of these: ${avoid.join('; ')}.\n`
    : '';
  return `Invent ONE fictional product concept to seed a requirements-tool demo. It must be funny in a dry, specific, observational way — the humour comes from a small true frustration taken seriously, never from randomness or wordplay salad — while still being a product an engineer could actually write requirements for.

Pick any category you like: software, hardware, a video game, a toy, a kitchen appliance, a wearable, pet tech, a board game, garden kit, a robot, sports equipment, transport, stationery, musical instruments — surprise me.
${avoidLine}
Every field must describe the SAME product, so the parts hang together:
- "category": a few words, lowercase (e.g. "kitchen appliance").
- "name": a brandable product name that riffs on this product's specific gimmick. Not a generic tech-sounding mash-up.
- "description": one sentence of the form "A <what it is> that <what it does>." — the concrete gimmick, not a category summary.
- "vision": one sentence starting with the product name, stating the ambition for THIS product (e.g. "<Name> becomes the toy that gets confiscated at bedtime and smuggled back by breakfast.").
- "problem": one or two sentences on why today's alternatives fail these particular people. No mention of the product itself.
- "targetUsers": the specific people this exact gimmick serves, caught in the moment it fixes — "engineers who reconstruct yesterday from browser history thirty seconds before stand-up", never a bare demographic like "engineers".

Reply with ONLY a JSON object, no commentary and no code fence:
{"category":"","name":"","description":"","vision":"","problem":"","targetUsers":""}`;
};

const REQUIRED_FIELDS: (keyof RandomProduct)[] = [
  'category',
  'name',
  'description',
  'vision',
  'problem',
  'targetUsers',
];

/**
 * Pull a product out of an agent's final text. Agents wrap JSON in fences or
 * add a sentence either side often enough that a strict JSON.parse of the
 * whole reply is not worth relying on, so the first balanced object wins.
 * Returns null when the reply is not a usable product.
 */
export const parseInventedProduct = (text: string): RandomProduct | null => {
  if (!text) return null;
  const start = text.indexOf('{');
  if (start < 0) return null;
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let i = start; i < text.length; i += 1) {
    const ch = text[i];
    if (escaped) {
      escaped = false;
    } else if (ch === '\\') {
      escaped = true;
    } else if (ch === '"') {
      inString = !inString;
    } else if (!inString && ch === '{') {
      depth += 1;
    } else if (!inString && ch === '}') {
      depth -= 1;
      if (depth === 0) {
        let parsed: any;
        try {
          parsed = JSON.parse(text.slice(start, i + 1));
        } catch {
          return null;
        }
        if (!parsed || typeof parsed !== 'object') return null;
        const product: Record<string, string> = {};
        for (const field of REQUIRED_FIELDS) {
          const value = parsed[field];
          if (typeof value !== 'string' || !value.trim()) return null;
          product[field] = value.trim();
        }
        return product as unknown as RandomProduct;
      }
    }
  }
  return null;
};
