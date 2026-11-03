import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { Chip, chipStyle, toneFor } from './Chip';
import { buttonStyle } from './buttonStyles';

// The shared chip: each status word reads in its one tone, unknown words are
// neutral, and the chip is a tint with a border and dark text, never white on
// a solid fill.

describe('toneFor', () => {
  it.each([
    ['pass', 'success'],
    ['Approved', 'success'],
    ['completed', 'success'],
    ['fail', 'danger'],
    ['aborted', 'danger'],
    ['blocked', 'warning'],
    ['suspect', 'warning'],
    ['in-review', 'info'],
    ['in review', 'info'],
    ['running', 'info'],
    ['in-progress', 'info'],
    ['proposal', 'agent'],
    ['draft', 'neutral'],
    ['not-run', 'neutral'],
    ['uncovered', 'neutral'],
    ['', 'neutral'],
  ])('%s is %s', (word, tone) => {
    expect(toneFor(word)).toBe(tone);
  });
});

describe('Chip', () => {
  it('takes its tone from its word, as a tint with a border', () => {
    const html = renderToStaticMarkup(<Chip>fail</Chip>);
    expect(html).toContain('background:var(--tint-red)');
    expect(html).toContain('border:1px solid var(--tint-red-border)');
    expect(html).toContain('color:var(--danger-strong)');
    expect(html).toContain('>fail<');
  });

  it('takes an explicit tone over its word', () => {
    expect(renderToStaticMarkup(<Chip tone="agent">fail</Chip>)).toContain('var(--tint-purple)');
  });

  it('never draws white text on a fill', () => {
    for (const tone of ['success', 'danger', 'warning', 'info', 'neutral', 'agent'] as const) {
      expect(chipStyle(tone).color).not.toBe('#fff');
    }
  });
});

describe('buttonStyle', () => {
  it('has one filled kind, and greys any kind out when disabled', () => {
    expect(buttonStyle('primary').background).toBe('var(--accent-strong)');
    expect(buttonStyle('secondary').background).toBe('var(--surface)');
    expect(buttonStyle('danger').background).toBe('var(--surface)');
    expect(buttonStyle('primary', { disabled: true }).cursor).toBe('not-allowed');
  });
});
