import {
  activeMentionQuery,
  applyMention,
  matchMentions,
  mentionCandidates,
  mentionHandle,
  mentionHandles,
  todoHandles,
  todoTargets,
} from './noteMentions';
import { ProjectMember } from '../api/client';

const member = (over: Partial<ProjectMember>): ProjectMember => ({
  project_id: 'p1',
  user_id: 'u1',
  role: 'editor',
  ...over,
});

describe('mentionHandles', () => {
  // These cases mirror mentions.Handles in internal/domain/mentions. The two
  // lists have to agree: a handle this menu writes that the server does not
  // recognise is a mention that names nobody.
  it('offers the name without spaces, the first name, then the email local part', () => {
    expect(mentionHandles('Dana Okoro', 'dana.okoro@example.com')).toEqual([
      'danaokoro',
      'dana',
      'dana.okoro',
    ]);
  });

  it('skips the name when there is none', () => {
    expect(mentionHandles('', 'jo@example.com')).toEqual(['jo']);
  });

  it('skips the email when it is not one', () => {
    expect(mentionHandles('Dana Okoro', 'not-an-email')).toEqual(['danaokoro', 'dana']);
  });
});

describe('mentionHandle', () => {
  it('writes the name handle when the server can capture it', () => {
    expect(mentionHandle('Dana Okoro', 'dana@example.com')).toBe('danaokoro');
  });

  it("falls back past a name the server's pattern would cut short", () => {
    // "ciarao'brien" stops at the apostrophe in @([\w.-]+), so writing it
    // would name nobody. The first name is the next handle the server also
    // answers to, and it is capturable, so that is what gets written.
    expect(mentionHandle("Ciara O'Brien", 'ciara.obrien@example.com')).toBe('ciara');
  });

  it('falls back to the email when no part of the name is capturable', () => {
    expect(mentionHandle("O'Brien", 'ciara.obrien@example.com')).toBe('ciara.obrien');
  });

  it('gives nothing when nothing about the person is capturable', () => {
    expect(mentionHandle("O'Brien", '')).toBe('');
  });
});

describe('mentionCandidates', () => {
  it('leaves out anyone who cannot be named', () => {
    const got = mentionCandidates([
      member({ user_id: 'u1', user_name: 'Dana Okoro', user_email: 'dana@example.com' }),
      member({ user_id: 'u2', user_name: "O'Brien" }),
    ]);
    expect(got.map((c) => c.userId)).toEqual(['u1']);
  });

  it('falls back to the email as a label when there is no name', () => {
    const got = mentionCandidates([member({ user_email: 'jo@example.com' })]);
    expect(got[0]).toMatchObject({ handle: 'jo', label: 'jo@example.com' });
  });
});

describe('activeMentionQuery', () => {
  it('opens on a single @ and reads it as a mention', () => {
    const text = 'ask @dan';
    expect(activeMentionQuery(text, text.length)).toEqual({
      start: 4,
      query: 'dan',
      scope: 'mention',
    });
  });

  it('reads a doubled @@ as a to-do', () => {
    const text = 'please @@dan';
    expect(activeMentionQuery(text, text.length)).toEqual({
      start: 7,
      query: 'dan',
      scope: 'todo',
    });
  });

  it('opens with nothing typed yet', () => {
    expect(activeMentionQuery('ask @', 5)).toMatchObject({ query: '', scope: 'mention' });
  });

  it('stays shut inside an email address', () => {
    const text = 'write to dana@example';
    expect(activeMentionQuery(text, text.length)).toBeNull();
  });

  it('stays shut once the writer has moved on', () => {
    const text = '@dana thanks';
    expect(activeMentionQuery(text, text.length)).toBeNull();
  });

  it('treats three or more @ as text, not a marker', () => {
    const text = 'wat @@@dan';
    expect(activeMentionQuery(text, text.length)).toBeNull();
  });
});

describe('matchMentions', () => {
  const candidates = mentionCandidates([
    member({ user_id: 'u1', user_name: 'Dana Okoro', user_email: 'dana@example.com' }),
    member({ user_id: 'u2', user_name: 'Sam Reeve', user_email: 'sam@example.com' }),
  ]);

  it('offers everyone before anything is typed', () => {
    expect(matchMentions(candidates, '')).toHaveLength(2);
  });

  it('matches on the handle', () => {
    expect(matchMentions(candidates, 'dana').map((c) => c.userId)).toEqual(['u1']);
  });

  it('matches on the displayed name', () => {
    expect(matchMentions(candidates, 'reeve').map((c) => c.userId)).toEqual(['u2']);
  });
});

describe('applyMention', () => {
  it('keeps the marker it was typed with', () => {
    const text = 'ask @dan';
    const query = activeMentionQuery(text, text.length)!;
    expect(applyMention(text, query, text.length, 'danaokoro')).toEqual({
      text: 'ask @danaokoro ',
      caret: 'ask @danaokoro '.length,
    });
  });

  it('keeps both markers for a to-do', () => {
    const text = 'please @@dan';
    const query = activeMentionQuery(text, text.length)!;
    expect(applyMention(text, query, text.length, 'danaokoro').text).toBe('please @@danaokoro ');
  });
});

describe('todoHandles', () => {
  it('finds nothing in a note with no doubled marker', () => {
    expect(todoHandles('thanks @danaokoro')).toEqual([]);
  });

  it('finds the doubled ones only', () => {
    expect(todoHandles('@danaokoro please see @@sam and @@jo')).toEqual(['sam', 'jo']);
  });

  it('names each person once', () => {
    expect(todoHandles('@@sam and again @@sam')).toEqual(['sam']);
  });

  it('ignores a doubled marker mid-word', () => {
    expect(todoHandles('a@@sam')).toEqual([]);
  });
});

describe('todoTargets', () => {
  const candidates = mentionCandidates([
    member({ user_id: 'u1', user_name: 'Dana Okoro', user_email: 'dana@example.com' }),
    member({ user_id: 'u2', user_name: 'Sam Reeve', user_email: 'sam.reeve@example.com' }),
  ]);

  it('resolves the people a note asks a to-do for', () => {
    expect(todoTargets('@@samreeve take this', candidates).map((c) => c.userId)).toEqual(['u2']);
  });

  it('resolves a handle typed by hand rather than picked from the menu', () => {
    // The menu would have written "samreeve"; "sam" is the first-name handle
    // the server also answers to, so a hand-typed note still works.
    expect(todoTargets('@@sam take this', candidates).map((c) => c.userId)).toEqual(['u2']);
  });

  it('ignores a name nobody answers to', () => {
    expect(todoTargets('@@nobody', candidates)).toEqual([]);
  });
});
