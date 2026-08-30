// Random product generator for the new-project wizard's testing mode: mints
// an amusing-but-workable product concept (software, hardware, games, toys,
// appliances, …) to seed the Guided Wizard, where the connected copilot
// agent takes it from framing to requirements. Pure client-side mad-libs —
// no AI call needed just to roll the dice.

export interface RandomProduct {
  category: string;
  name: string;
  description: string;
  vision: string;
  problem: string;
  targetUsers: string;
}

const pick = <T>(items: T[]): T => items[Math.floor(Math.random() * items.length)];

const BRAND_PREFIXES = [
  'Snoozle', 'Wobble', 'Grump', 'Quibble', 'Blorp', 'Fidget', 'Crumb',
  'Waffle', 'Doom', 'Pickle', 'Thunder', 'Meep', 'Gadget', 'Noodle', 'Zest',
];
const BRAND_SUFFIXES = [
  'matic 3000', 'Tron', 'ly', 'Hub', 'Force', 'Buddy', 'X', 'Deluxe',
  ' Prime', 'inator', 'Sync', 'Nest', 'Pod', 'Max', 'OS',
];

const AUDIENCES = [
  'over-caffeinated project managers',
  'competitive toddlers and their exhausted guardians',
  'cats with strong opinions and the humans who serve them',
  'retired pirates adjusting to suburban life',
  'introverts who agreed to one (1) social event this year',
  'houseplant owners with a guilty conscience',
  'weekend astronauts and other optimists',
  'people who own more board games than chairs',
  'garage inventors whose smoke alarm knows them personally',
  'office workers locked in passive-aggressive thermostat wars',
  'dog walkers seeking corporate sponsorship',
  'grandparents who discovered speedrunning',
];

interface CategoryTemplate {
  category: string;
  // (thing, audience) => parts
  build: (name: string, audience: string) => Omit<RandomProduct, 'category' | 'name'>;
}

const CATEGORIES: CategoryTemplate[] = [
  {
    category: 'software',
    build: (name, audience) => ({
      description: `A ${pick(['judgmental', 'suspiciously cheerful', 'brutally honest', 'gently sarcastic'])} productivity app for ${audience}.`,
      vision: `${name} becomes the daily ritual that ${audience} pretend to complain about but secretly cannot live without.`,
      problem: `Existing tools assume users are rational, motivated, and awake. ${audience.charAt(0).toUpperCase() + audience.slice(1)} are at most one of those things at any given moment.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'hardware',
    build: (name, audience) => ({
      description: `A desk-mounted ${pick(['air-cannon', 'semaphore flag array', 'miniature fog machine', 'confetti dispenser'])} that signals availability status so nobody has to speak.`,
      vision: `${name} makes every workspace ${pick(['5% more theatrical', 'measurably quieter', 'dangerously efficient'])} while remaining technically allowed by HR.`,
      problem: `${audience.charAt(0).toUpperCase() + audience.slice(1)} lack a socially acceptable way to broadcast "do not perceive me" within a 4-meter radius.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'video game',
    build: (name, audience) => ({
      description: `A ${pick(['cozy', 'roguelike', 'turn-based', 'physics-driven'])} game about ${pick(['running a bureaucracy for ghosts', 'parallel parking increasingly large sea creatures', 'de-escalating arguments between sentient kitchen appliances', 'speed-folding infinite laundry'])}.`,
      vision: `${name} proves that ${audience} will 100% a game about literally anything if the sound design is satisfying enough.`,
      problem: `The market is saturated with games about saving the world; nobody is serving players who want to competently perform a small, weird job.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'toy',
    build: (name, audience) => ({
      description: `A plush ${pick(['axolotl', 'tax auditor', 'cumulonimbus cloud', 'cryptid of unspecified provenance'])} that ${pick(['repeats the last thing it heard in a tiny voice', 'vibrates ominously when homework is near', 'glows brighter the longer it is ignored'])}.`,
      vision: `${name} becomes the toy that ${audience} argue about at holiday gatherings, in a good way.`,
      problem: `Modern toys demand screens, accounts, and firmware updates. ${audience.charAt(0).toUpperCase() + audience.slice(1)} deserve chaos that works offline.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'kitchen appliance',
    build: (name, audience) => ({
      description: `A countertop appliance that ${pick(['toasts bread to a user-calibrated emotional state', 'brews tea while reading the weather in a disappointed tone', 'portions snacks according to how the day is actually going'])}.`,
      vision: `${name} restores the kitchen as a place of comfort, drama, and precisely one beep.`,
      problem: `Appliances either do too little or connect to the cloud to do it. ${audience.charAt(0).toUpperCase() + audience.slice(1)} want exactly one perfect function with zero firmware.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'wearable',
    build: (name, audience) => ({
      description: `A ${pick(['ring', 'sock', 'monocle', 'lapel pin'])} that tracks ${pick(['how many times you almost said something', 'sighs per hour with trend analysis', 'proximity to people who owe you money'])}.`,
      vision: `${name} gives ${audience} the metrics nobody asked for and everybody checks hourly.`,
      problem: `Wearables obsess over steps and sleep while ignoring the vital signs of daily social survival.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'pet tech',
    build: (name, audience) => ({
      description: `A ${pick(['treat-dispensing intercom', 'laser scheduling system', 'automated ball diplomacy platform'])} that lets pets ${pick(['file formal complaints', 'schedule zoomies within quiet hours', 'approve or veto houseguests'])}.`,
      vision: `${name} finally gives pets a seat at the table they were already sitting on.`,
      problem: `${audience.charAt(0).toUpperCase() + audience.slice(1)} currently interpret their animals through guesswork and vibes, with mixed diplomatic results.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'board game',
    build: (name, audience) => ({
      description: `A ${pick(['co-op', 'social deduction', 'legacy', 'dexterity'])} board game where players ${pick(['manage a haunted homeowners association', 'smuggle soup across a fantasy border', 'compete to give the worst constructive feedback'])}.`,
      vision: `${name} ends game nights in laughter, betrayal, and at least one strongly worded house rule.`,
      problem: `${audience.charAt(0).toUpperCase() + audience.slice(1)} have exhausted their shelves and their friendships need fresh, structured conflict.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'garden tech',
    build: (name, audience) => ({
      description: `A solar-powered ${pick(['scarecrow with customizable opinions', 'gnome that live-narrates squirrel activity', 'sprinkler that waters plants and eavesdroppers alike'])}.`,
      vision: `${name} turns every garden into a well-defended, mildly theatrical ecosystem.`,
      problem: `Gardens are under constant assault by pests, weather, and neighbors with unsolicited advice; ${audience} are outnumbered.`,
      targetUsers: audience,
    }),
  },
  {
    category: 'robot',
    build: (name, audience) => ({
      description: `A ${pick(['palm-sized', 'suspiciously polite', 'sock-drawer-dwelling'])} robot whose sole job is ${pick(['returning borrowed pens with a receipt', 'guarding the last slice of pizza', 'applauding minor achievements at maximum volume'])}.`,
      vision: `${name} does one small thing with the unwavering dedication ${audience} can no longer muster themselves.`,
      problem: `General-purpose robots overpromise. ${audience.charAt(0).toUpperCase() + audience.slice(1)} need one chore annihilated completely, not twelve chores attempted poorly.`,
      targetUsers: audience,
    }),
  },
];

export const generateRandomProduct = (): RandomProduct => {
  const template = pick(CATEGORIES);
  const name = `${pick(BRAND_PREFIXES)}${pick(BRAND_SUFFIXES)}`;
  const audience = pick(AUDIENCES);
  const parts = template.build(name, audience);
  return { category: template.category, name, ...parts };
};
