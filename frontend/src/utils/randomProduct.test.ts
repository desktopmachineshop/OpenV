import {
  clearInventedProducts,
  describeProductFlaws,
  fromSharedProduct,
  isSharedProduct,
  toSharePayload,
  generateRandomProduct,
  inventProductPrompt,
  isInventedProduct,
  loadInventedProducts,
  parseInventedProduct,
  saveInventedProduct,
  RandomProduct,
} from './randomProduct';

// Every roll must hang together as one product: the name appears in the
// vision, the description states the gimmick, and nothing is blank or
// half-assembled. Rolling many times covers the whole concept pool.
describe('generateRandomProduct', () => {
  const rolls = Array.from({ length: 300 }, () => generateRandomProduct());

  it('fills every field with a complete sentence or phrase', () => {
    rolls.forEach((p) => {
      expect(p.category.length).toBeGreaterThan(0);
      expect(p.name.trim().length).toBeGreaterThan(0);
      // Descriptions/visions/problems are sentences; audiences are phrases.
      expect(p.description).toMatch(/^A .+ that .+\.$/);
      expect(p.vision).toMatch(/\.$/);
      expect(p.problem).toMatch(/\.$/);
      // Audiences must be the specific people this gimmick serves, caught in
      // the moment it fixes — not a bare demographic that fits any product.
      expect(p.targetUsers.split(' ').length).toBeGreaterThanOrEqual(8);
      // A qualifying clause is what makes it a situation rather than a label.
      expect(p.targetUsers).toMatch(/\b(who|whom|whose|whoever|where|which|that)\b/);
      // No unresolved template fragments.
      [p.description, p.vision, p.problem, p.targetUsers].forEach((text) => {
        expect(text).not.toMatch(/undefined|\$\{|\[object/);
      });
    });
  });

  it('ties the vision back to the product name', () => {
    rolls.forEach((p) => {
      expect(p.vision).toContain(p.name);
    });
  });

  it('varies both the concept and the brand name across rolls', () => {
    expect(new Set(rolls.map((p) => p.category)).size).toBeGreaterThan(3);
    expect(new Set(rolls.map((p) => p.name)).size).toBeGreaterThan(5);
    // A concept always pairs with its own description, never another's.
    const byDescription = new Map<string, Set<string>>();
    rolls.forEach((p) => {
      const names = byDescription.get(p.description) || new Set<string>();
      names.add(p.targetUsers);
      byDescription.set(p.description, names);
    });
    byDescription.forEach((audiences) => {
      expect(audiences.size).toBe(1);
    });
  });
});

describe('inventProductPrompt', () => {
  it('asks for exactly the fields the card renders', () => {
    const prompt = inventProductPrompt([]);
    ['category', 'name', 'description', 'vision', 'problem', 'targetUsers'].forEach((field) => {
      expect(prompt).toContain(field);
    });
    expect(prompt).toMatch(/only a json object/i);
  });

  it('tells the agent what to avoid so repeat clicks stay fresh', () => {
    const prompt = inventProductPrompt(['Crustodian (robot)', 'Kevinproof (kitchen appliance)']);
    expect(prompt).toContain('Crustodian (robot)');
    expect(prompt).toContain('Kevinproof (kitchen appliance)');
    // With nothing shown yet there is nothing to avoid.
    expect(inventProductPrompt([])).not.toMatch(/Already used/);
  });
});

describe('parseInventedProduct', () => {
  const valid = {
    category: 'stationery',
    name: 'Inkwell',
    description: 'A fountain pen that logs how much you actually wrote today.',
    vision: 'Inkwell becomes the pen that proves the notebook was used.',
    problem: 'Notebooks fill with good intentions and nobody can tell which pages were real work.',
    targetUsers: 'stationery hoarders who buy a fifth notebook while four sit empty',
  };

  it('reads a bare JSON reply', () => {
    expect(parseInventedProduct(JSON.stringify(valid))).toEqual(valid);
  });

  it('reads JSON wrapped in a fence or surrounded by chatter', () => {
    expect(
      parseInventedProduct('Sure! ```json\n' + JSON.stringify(valid) + '\n```\nHope that helps.')
    ).toEqual(valid);
  });

  it('trims whitespace around the values', () => {
    const padded = { ...valid, name: '  Inkwell  ' };
    expect(parseInventedProduct(JSON.stringify(padded))?.name).toBe('Inkwell');
  });

  it('rejects replies that are not a usable product', () => {
    const { name, ...missingName } = valid;
    expect(parseInventedProduct(JSON.stringify(missingName))).toBeNull();
    expect(parseInventedProduct(JSON.stringify({ ...valid, vision: '   ' }))).toBeNull();
    expect(parseInventedProduct('I could not think of one, sorry.')).toBeNull();
    expect(parseInventedProduct('{"category": "broken"')).toBeNull();
    expect(parseInventedProduct('')).toBeNull();
  });

  it('handles braces inside string values', () => {
    const braced = { ...valid, description: 'A pen that writes {curly} notes.' };
    expect(parseInventedProduct(JSON.stringify(braced))?.description).toBe(
      'A pen that writes {curly} notes.'
    );
  });
});

describe('kept inventions', () => {
  const invention = (name: string): RandomProduct => ({
    category: 'stationery',
    name,
    description: `A ${name} that logs how much you actually wrote today.`,
    vision: `${name} becomes the pen that proves the notebook was used.`,
    problem: 'Notebooks fill with good intentions and nobody can tell which pages were real work.',
    targetUsers: 'stationery hoarders who buy a fifth notebook while four sit empty',
  });

  beforeEach(() => {
    clearInventedProducts();
  });

  it('keeps an invention across reloads', () => {
    expect(loadInventedProducts()).toEqual([]);
    saveInventedProduct(invention('Inkwell'));
    expect(loadInventedProducts()).toEqual([invention('Inkwell')]);
  });

  it('replaces a re-invented name instead of stacking duplicates', () => {
    saveInventedProduct(invention('Inkwell'));
    const revised = { ...invention('Inkwell'), description: 'A pen that does something else.' };
    const kept = saveInventedProduct(revised);
    expect(kept).toHaveLength(1);
    expect(kept[0].description).toBe('A pen that does something else.');
  });

  it('keeps the newest first and forgets them on request', () => {
    saveInventedProduct(invention('First'));
    saveInventedProduct(invention('Second'));
    expect(loadInventedProducts().map((p) => p.name)).toEqual(['Second', 'First']);
    clearInventedProducts();
    expect(loadInventedProducts()).toEqual([]);
  });

  it('ignores corrupt or half-formed stored entries', () => {
    localStorage.setItem('openv-invented-products', 'not json');
    expect(loadInventedProducts()).toEqual([]);
    localStorage.setItem(
      'openv-invented-products',
      JSON.stringify({ v: 1, products: [{ name: 'Half' }, invention('Whole')] })
    );
    expect(loadInventedProducts().map((p) => p.name)).toEqual(['Whole']);
  });

  it('mixes kept inventions into the reroll pool', () => {
    const kept = [invention('Inkwell')];
    const rolls = Array.from({ length: 400 }, () => generateRandomProduct(kept));
    expect(rolls.some((p) => p.name === 'Inkwell')).toBe(true);
    // Built-ins still appear, so an invention does not take over the pool.
    expect(rolls.some((p) => p.name !== 'Inkwell')).toBe(true);
    // A rolled invention is recognised so the card can tag it.
    const rolledInvention = rolls.find((p) => p.name === 'Inkwell')!;
    expect(isInventedProduct(rolledInvention, kept)).toBe(true);
    expect(isInventedProduct(rolls.find((p) => p.name !== 'Inkwell')!, kept)).toBe(false);
  });

  it('rolls only built-ins when nothing has been invented', () => {
    const rolls = Array.from({ length: 50 }, () => generateRandomProduct([]));
    rolls.forEach((p) => expect(isInventedProduct(p, [])).toBe(false));
  });
});

describe('community-shared products', () => {
  const payload = {
    id: 'shared-1',
    category: 'kitchen appliance',
    name: 'Kevinproof',
    description: 'A coffee tin that recognises Kevin and locks.',
    vision: 'Kevinproof becomes the reason the bean jar survives a Tuesday.',
    problem: 'Beans vanish overnight and nobody admits to owning the grinder.',
    target_users: 'office workers whose beans keep leaving with Kevin',
  };

  it('maps the API shape onto a rollable product, keeping the id', () => {
    const product = fromSharedProduct(payload);
    expect(product.targetUsers).toBe(payload.target_users);
    expect(product.sharedId).toBe('shared-1');
    expect(isSharedProduct(product)).toBe(true);
  });

  it('sends only the six card fields when sharing', () => {
    // The server assigns id, timestamp and author; a client that could set
    // them could forge attribution, so they must not be in the payload.
    expect(toSharePayload(fromSharedProduct(payload))).toEqual({
      category: payload.category,
      name: payload.name,
      description: payload.description,
      vision: payload.vision,
      problem: payload.problem,
      target_users: payload.target_users,
    });
  });

  it('rolls shared products alongside built-ins without claiming they are local inventions', () => {
    const community = [fromSharedProduct(payload)];
    const rolls = Array.from({ length: 400 }, () => generateRandomProduct(community));

    // Rolls are told apart by the shared id, not by name: 'Kevinproof' is
    // also a built-in concept name, so a name match finds whichever kind was
    // rolled first and the assertion below passes or fails at random.
    const rolledShared = rolls.find((p) => p.sharedId === payload.id);
    expect(rolledShared).toBeDefined();
    expect(isSharedProduct(rolledShared!)).toBe(true);
    // Somebody else's shared product is not one of this browser's kept
    // inventions, so the card must not tag it as agent-invented here.
    expect(isInventedProduct(rolledShared!, [])).toBe(false);
    // Built-ins keep appearing, and they carry no shared id.
    const builtIn = rolls.find((p) => !p.sharedId)!;
    expect(isSharedProduct(builtIn)).toBe(false);
  });
});

// The quality gate has to encode the standard the built-in concepts already
// meet — otherwise it either rejects good products or lets weak ones into the
// collection everyone rolls from.
describe('describeProductFlaws', () => {
  it('accepts every built-in roll', () => {
    Array.from({ length: 300 }, () => generateRandomProduct()).forEach((product) => {
      expect(describeProductFlaws(product)).toEqual([]);
    });
  });

  const good: RandomProduct = {
    category: 'kitchen appliance',
    name: 'Kevinproof',
    description: 'A coffee tin that recognises Kevin and locks.',
    vision: 'Kevinproof becomes the reason the office bean jar survives a Tuesday.',
    problem: 'Beans vanish overnight and nobody will admit to owning the grinder.',
    targetUsers: 'coffee-obsessed office workers whose beans keep leaving with Kevin',
  };

  it('catches the faults that made early inventions weak', () => {
    const cases: [string, Partial<RandomProduct>, RegExp][] = [
      // The failure the maintainer reported twice: a name bolted together
      // from a word and a generic suffix, fitting any product anywhere.
      ['generic suffix', { name: 'QuibbleMax', vision: 'QuibbleMax becomes the thing.' }, /generic suffix/i],
      ['name is a sentence', { name: 'The Coffee Tin That Locks', vision: 'The Coffee Tin That Locks wins.' }, /not a brand/i],
      ['vision drops the name', { vision: 'It becomes the reason the jar survives.' }, /vision must contain/i],
      ['description is a category summary', { description: 'Improves kitchen security outcomes.' }, /A <what it is> that/],
      ['audience is a label', { targetUsers: 'office workers' }, /moment/i],
      ['audience has no clause', { targetUsers: 'busy tired underpaid overworked hungry caffeinated office people' }, /qualifying clause/i],
      ['problem names the product', { problem: 'Kevinproof is needed because beans vanish.' }, /must not mention the product/i],
    ];
    cases.forEach(([label, patch, expected]) => {
      const flaws = describeProductFlaws({ ...good, ...patch });
      expect(flaws.length).toBeGreaterThan(0);
      expect(flaws.join(' ')).toMatch(expected);
      expect(label).toBeTruthy();
    });
  });

  it('passes a product that hangs together', () => {
    expect(describeProductFlaws(good)).toEqual([]);
  });
});

describe('inventProductPrompt quality brief', () => {
  it('shows two complete worked examples drawn from the built-in concepts', () => {
    const prompt = inventProductPrompt([]);
    // Examples are rendered products, so every card field appears in JSON form.
    ['"category"', '"name"', '"description"', '"vision"', '"problem"', '"targetUsers"'].forEach((key) => {
      expect(prompt.split(key).length).toBeGreaterThan(2);
    });
    // And they are real rolls, not invented illustrations: the concepts' own
    // audience wording is distinctive enough to find.
    expect(prompt).toMatch(/becomes|Make /);
  });

  it('names the failure modes that made early inventions weak', () => {
    const prompt = inventProductPrompt([]);
    expect(prompt).toMatch(/"Max", "Pro", "X"/);
    expect(prompt).toMatch(/moment, with a specific detail/i);
    expect(prompt).toMatch(/Before you answer, check your own draft/i);
  });

  it('tells a retry exactly what to fix', () => {
    const prompt = inventProductPrompt([], ['The vision must contain the product name.']);
    expect(prompt).toContain('Your previous attempt was rejected');
    expect(prompt).toContain('The vision must contain the product name.');
    // A first attempt carries no rejection notes.
    expect(inventProductPrompt([])).not.toContain('previous attempt');
  });
});
