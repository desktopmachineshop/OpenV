import { generateRandomProduct, inventProductPrompt, parseInventedProduct } from './randomProduct';

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
