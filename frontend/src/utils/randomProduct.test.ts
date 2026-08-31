import { generateRandomProduct } from './randomProduct';

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
