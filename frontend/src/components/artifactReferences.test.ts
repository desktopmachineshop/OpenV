import { Artifact, Attachment, Link } from '../api/client';
import {
  REFERENCE_SCHEME,
  activeReferenceQuery,
  applyReference,
  linkifyReferences,
  matchReferences,
  referenceCandidates,
  referenceFromHref,
} from './artifactReferences';

const artifact = (over: Partial<Artifact> & { id: string }): Artifact => ({
  project_id: 'p1',
  type: 'requirement',
  title: '',
  body: '',
  attributes: {},
  version: 1,
  valid_from: '',
  valid_to: null,
  created_at: '',
  updated_at: '',
  ...over,
});

const link = (from_id: string, to_id: string, type = 'verifies'): Link => ({
  id: `${from_id}->${to_id}`,
  from_id,
  to_id,
  type,
  attributes: {},
  version: 1,
  created_at: '',
  updated_at: '',
});

const attachment = (over: Partial<Attachment> & { id: string; artifact_id: string }): Attachment => ({
  filename: 'f.png',
  original_filename: 'f.png',
  mime_type: 'image/png',
  file_path: '/tmp/f.png',
  file_size: 1,
  version: 1,
  created_at: '',
  ...over,
});

// The menu names only what the artifact is already connected to. Citing
// something it has no link to would be a claim the traceability matrix cannot
// see, so the menu never offers it.
describe('referenceCandidates', () => {
  const me = artifact({ id: 'me', ref: 'REQ-17' });
  const linkedTo = artifact({ id: 'tc', ref: 'TC-4', title: 'Key rotation suite' });
  const unlinked = artifact({ id: 'other', ref: 'REQ-99', title: 'Nothing to do with me' });

  it('offers the artifact its own figures first, then what it links to', () => {
    const got = referenceCandidates(
      me,
      [link('tc', 'me')],
      [me, linkedTo, unlinked],
      [attachment({ id: 'a1', artifact_id: 'me', figure_ref: 'REQ-17-FIG-1', original_filename: 'pump.png' })]
    );
    expect(got.map((c) => c.ref)).toEqual(['REQ-17-FIG-1', 'TC-4']);
    expect(got[0]).toMatchObject({ kind: 'figure', label: 'pump.png' });
    expect(got[1]).toMatchObject({ kind: 'artifact', label: 'Key rotation suite', relation: 'verifies' });
  });

  it('leaves out artifacts it is not linked to', () => {
    const got = referenceCandidates(me, [link('tc', 'me')], [me, linkedTo, unlinked], []);
    expect(got.map((c) => c.ref)).not.toContain('REQ-99');
  });

  it('follows a link in either direction, and names each neighbour once', () => {
    const got = referenceCandidates(
      me,
      [link('me', 'tc', 'derives-from'), link('tc', 'me', 'verifies')],
      [me, linkedTo],
      []
    );
    expect(got.map((c) => c.ref)).toEqual(['TC-4']);
  });

  it('skips a neighbour with no reference to cite, and figures of other artifacts', () => {
    const refless = artifact({ id: 'nope', title: 'No ref' });
    const got = referenceCandidates(
      me,
      [link('me', 'nope'), link('me', 'missing')],
      [me, refless],
      [attachment({ id: 'a2', artifact_id: 'someone-else', figure_ref: 'REQ-99-FIG-1' })]
    );
    expect(got).toEqual([]);
  });

  it('offers nothing while creating an artifact that does not exist yet', () => {
    expect(referenceCandidates(undefined, [link('a', 'b')], [linkedTo], [])).toEqual([]);
  });
});

describe('activeReferenceQuery', () => {
  it('opens on a # at the start of a word and tracks what follows', () => {
    expect(activeReferenceQuery('see #', 5)).toEqual({ start: 4, query: '' });
    expect(activeReferenceQuery('see #REQ', 8)).toEqual({ start: 4, query: 'REQ' });
    expect(activeReferenceQuery('#REQ', 4)).toEqual({ start: 0, query: 'REQ' });
    expect(activeReferenceQuery('line\n#TC', 8)).toEqual({ start: 5, query: 'TC' });
  });

  it('ignores a # inside a word, so URLs and "PR#12" are left alone', () => {
    expect(activeReferenceQuery('PR#12', 5)).toBeNull();
    expect(activeReferenceQuery('http://x/y#frag', 15)).toBeNull();
  });

  it('closes once the writer moves past the reference', () => {
    expect(activeReferenceQuery('see #REQ-17 and', 15)).toBeNull();
    expect(activeReferenceQuery('no reference here', 17)).toBeNull();
  });

  it('reads from the caret, not the end of the text', () => {
    // Caret sits just after "#RE" while more text follows.
    expect(activeReferenceQuery('see #REQ-17 and more', 7)).toEqual({ start: 4, query: 'RE' });
  });
});

describe('matchReferences', () => {
  const candidates = referenceCandidates(
    artifact({ id: 'me' }),
    [link('tc', 'me')],
    [artifact({ id: 'me' }), artifact({ id: 'tc', ref: 'TC-4', title: 'Key rotation suite' })],
    [attachment({ id: 'a1', artifact_id: 'me', figure_ref: 'REQ-17-FIG-1', original_filename: 'pump.png' })]
  );

  it('matches on the reference or the name, case-insensitively', () => {
    expect(matchReferences(candidates, 'fig').map((c) => c.ref)).toEqual(['REQ-17-FIG-1']);
    expect(matchReferences(candidates, 'rotation').map((c) => c.ref)).toEqual(['TC-4']);
    expect(matchReferences(candidates, 'tc-').map((c) => c.ref)).toEqual(['TC-4']);
  });

  it('offers everything for an empty query and nothing for a miss', () => {
    expect(matchReferences(candidates, '')).toHaveLength(2);
    expect(matchReferences(candidates, 'nothing')).toEqual([]);
  });
});

describe('applyReference', () => {
  it('replaces what was typed and leaves the caret past the reference', () => {
    const text = 'as shown in #RE';
    const query = activeReferenceQuery(text, text.length)!;
    const got = applyReference(text, query, text.length, 'REQ-17-FIG-1');
    expect(got.text).toBe('as shown in #REQ-17-FIG-1 ');
    expect(got.caret).toBe(got.text.length);
  });

  it('keeps the text that followed the caret', () => {
    const text = 'see #RE for the rest';
    const caret = 7; // just after "#RE"
    const query = activeReferenceQuery(text, caret)!;
    const got = applyReference(text, query, caret, 'TC-4');
    expect(got.text).toBe('see #TC-4  for the rest');
    // The caret sits after the inserted reference, not at the end.
    expect(got.text.slice(0, got.caret)).toBe('see #TC-4 ');
  });
});

// A citation in prose should be followed, not retyped into the search box.
// The rewrite has to be conservative: bodies are markdown, and "# Heading" is
// far more common than a reference.
describe('linkifyReferences', () => {
  it('links a reference in prose', () => {
    expect(linkifyReferences('as shown in #REQ-17-FIG-1 above')).toBe(
      `as shown in [#REQ-17-FIG-1](${REFERENCE_SCHEME}REQ-17-FIG-1) above`
    );
    expect(linkifyReferences('#TC-4 covers it')).toBe(
      `[#TC-4](${REFERENCE_SCHEME}TC-4) covers it`
    );
  });

  it('links every reference in a body', () => {
    const got = linkifyReferences('#REQ-1 and #HAZ-2-FIG-3');
    expect(got).toContain(`${REFERENCE_SCHEME}REQ-1`);
    expect(got).toContain(`${REFERENCE_SCHEME}HAZ-2-FIG-3`);
  });

  it('leaves markdown headings and bare hashes alone', () => {
    for (const body of ['# Heading', '## Sub heading', 'a # on its own', '#not-a-ref']) {
      expect(linkifyReferences(body)).toBe(body);
    }
  });

  it('keeps trailing punctuation out of the link', () => {
    expect(linkifyReferences('see #REQ-12.')).toBe(
      `see [#REQ-12](${REFERENCE_SCHEME}REQ-12).`
    );
  });

  it('does not touch a # inside a word', () => {
    expect(linkifyReferences('PR#12 and http://x/y#REQ-1')).toBe('PR#12 and http://x/y#REQ-1');
  });
});

describe('referenceFromHref', () => {
  it('reads the reference out of an intercepted link', () => {
    expect(referenceFromHref(`${REFERENCE_SCHEME}REQ-17-FIG-1`)).toBe('REQ-17-FIG-1');
  });

  it('reports nothing for an ordinary link', () => {
    expect(referenceFromHref('https://example.com')).toBe('');
    expect(referenceFromHref(undefined)).toBe('');
  });
});
