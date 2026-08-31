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
  /** Who it is for — a noun phrase that follows "for ". */
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
    audience: 'engineers who can only remember yesterday under interrogation',
    gap: 'Stand-up updates are reconstructed from memory at 9:01 a.m., which is why they are equal parts fiction and apology.',
    ambition: 'the two-minute ritual that makes stand-up honest without making it longer',
  },
  {
    category: 'software',
    names: ['Bailout', 'Exit Strategy', 'Phoney'],
    what: 'calendar assistant',
    does: 'invents a plausible, escalating excuse and calls you out of any meeting that runs twenty minutes over',
    audience: 'people whose calendars are mostly meetings that could have been a shrug',
    gap: 'Leaving a meeting early takes either courage or a believable emergency, and almost nobody has one on hand at 4pm.',
    ambition: 'the escape hatch every over-booked calendar quietly needs',
  },
  {
    category: 'hardware',
    names: ['Fogoff', 'DeskCannon', 'Smokescreen'],
    what: 'desk-mounted fog machine and status lamp',
    does: 'releases a small dramatic fog cloud when your focus timer starts and glows green when you are interruptible',
    audience: 'open-plan office workers who get interrupted every eleven minutes',
    gap: 'Headphones are ambiguous, do-not-disturb statuses are invisible from three desks away, and saying "I am busy" out loud all day is exhausting.',
    ambition: 'the availability signal readable from across the room, with theatre',
  },
  {
    category: 'hardware',
    names: ['Truce', 'Degree Zero', 'ThermoPeace'],
    what: 'thermostat with a voting panel',
    does: 'requires a quorum before anyone can change the temperature and keeps a log of who keeps trying',
    audience: 'households and offices locked in permanent thermostat warfare',
    gap: 'A thermostat has one setting and many stakeholders, so the coldest-blooded person wins by stealth every single afternoon.',
    ambition: 'the end of the thermostat cold war, complete with an audit trail',
  },
  {
    category: 'video game',
    names: ['Krakenpark', 'Leviathan Valet', 'Deep Reverse'],
    what: 'physics puzzle game',
    does: 'has you parallel-park increasingly enormous sea creatures into increasingly small harbours',
    audience: 'players who want to do one absurd job extremely competently',
    gap: 'The store is full of games about saving the world and nearly empty of games about doing a small, strange task perfectly.',
    ambition: 'the game people sink sixty hours into and describe to friends as "you park a whale"',
  },
  {
    category: 'video game',
    names: ['Afterclerk', 'Spectral Services', 'HauntDesk'],
    what: 'cozy management game',
    does: 'puts you behind the counter of a permit office for ghosts who are extremely particular about paperwork',
    audience: 'people who find spreadsheets soothing and would like some ghosts in theirs',
    gap: 'Management games simulate empires; nobody serves the player who just wants a tidy desk and a satisfied customer.',
    ambition: 'the comfort game that makes admin work feel like a warm bath',
  },
  {
    category: 'toy',
    names: ['Mimicorn', 'EchoPal', 'Parrotplush'],
    what: 'plush axolotl with a hidden microphone',
    does: 'repeats the last thing it heard in a tiny, slightly wrong voice',
    audience: 'parents whose children have just discovered practical jokes',
    gap: 'Talking toys cycle through the same forty phrases until the batteries die, and children find the pattern in an afternoon.',
    ambition: 'the toy that gets confiscated at bedtime and smuggled back by breakfast',
  },
  {
    category: 'toy',
    names: ['Sulk', 'Broodcloud', 'Moodlump'],
    what: 'plush storm cloud',
    does: 'glows brighter and rumbles louder the longer it is left abandoned on the floor',
    audience: 'families renegotiating the tidying-up treaty every single evening',
    gap: 'Tidying up is a nightly argument because toys have no opinion about being left underfoot.',
    ambition: 'the toy that does the nagging so nobody in the house has to',
  },
  {
    category: 'kitchen appliance',
    names: ['Toastmood', 'Browning Point', 'Charmometer'],
    what: 'countertop toaster',
    does: 'asks how your morning is going and browns the bread to match, from "pale and hopeful" to "carbonised"',
    audience: 'people for whom breakfast is the only negotiable part of the morning',
    gap: 'Toasters offer a numbered dial that means nothing, so every slice is a gamble placed before anyone is awake enough to gamble.',
    ambition: 'the appliance that finally admits toast is an emotional decision',
  },
  {
    category: 'wearable',
    names: ['Sighence', 'Huffwatch', 'Exhale'],
    what: 'lapel pin',
    does: 'counts your sighs per hour and charts them against your calendar',
    audience: 'knowledge workers who suspect their job is the problem but lack the data',
    gap: 'Wearables obsess over steps and sleep while ignoring the vital signs of office survival.',
    ambition: 'the wearable that turns a vaguely terrible week into a chart you can show someone',
  },
  {
    category: 'pet tech',
    names: ['Petition', 'Meowdio', 'Grievance Bell'],
    what: 'paw-operated intercom',
    does: 'lets pets file formal complaints, which arrive as time-stamped voice notes on your phone',
    audience: 'cat owners who already narrate their pet’s inner monologue out loud',
    gap: 'Pets currently communicate by knocking things off tables — an unstructured feedback channel with no audit trail.',
    ambition: 'the official grievance procedure every household pet has been demanding for centuries',
  },
  {
    category: 'board game',
    names: ['Broth Runners', 'Souperior', 'Ladle & Order'],
    what: 'social deduction board game',
    does: 'has players smuggling soup across a fantasy border while one of them is secretly the customs inspector',
    audience: 'game groups whose shelves are full and whose friendships need fresh, structured conflict',
    gap: 'Every group has played the same three deduction games to death and now knows exactly how each other lies.',
    ambition: 'the game night that ends in laughter, betrayal, and one strongly worded house rule',
  },
  {
    category: 'garden tech',
    names: ['Gnomecast', 'Squirrelspotter', 'Lawn Commentary'],
    what: 'solar-powered garden gnome',
    does: 'live-narrates squirrel activity in the voice of an increasingly invested sports commentator',
    audience: 'gardeners at war with wildlife they secretly admire',
    gap: 'Garden pests get treated as a problem to be solved when they are, in fact, the best entertainment on the street.',
    ambition: 'the garden ornament people leave the kitchen window open for',
  },
  {
    category: 'robot',
    names: ['Crustodian', 'Slicekeeper', 'Leftover Watch'],
    what: 'palm-sized fridge robot',
    does: 'guards the last slice of pizza and reports, with photographic evidence, exactly who took it',
    audience: 'shared households where labelled food disappears anyway',
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
