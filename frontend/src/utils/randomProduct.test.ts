import {
  clearInventedProducts,
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

    const rolledShared = rolls.find((p) => p.name === 'Kevinproof');
    expect(rolledShared).toBeDefined();
    expect(isSharedProduct(rolledShared!)).toBe(true);
    // Somebody else's shared product is not one of this browser's kept
    // inventions, so the card must not tag it as agent-invented here.
    expect(isInventedProduct(rolledShared!, [])).toBe(false);
    // Built-ins keep appearing, and they carry no shared id.
    const builtIn = rolls.find((p) => p.name !== 'Kevinproof')!;
    expect(isSharedProduct(builtIn)).toBe(false);
  });
});
