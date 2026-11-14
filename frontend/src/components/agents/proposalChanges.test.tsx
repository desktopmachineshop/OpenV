import { renderToStaticMarkup } from 'react-dom/server';
import type { Artifact, Proposal } from '../../api/client';
import { ProposalChanges } from './proposalChanges';

// What an approver reads before approving: only what the proposal changes.

const artifact = {
  id: 'a1',
  project_id: 'p1',
  type: 'requirement',
  ref: 'REQ-6',
  title: 'Surface finish',
  body: 'The mill shall leave Ra 1.6 µm or better.',
  attributes: { priority: 'should', verification_method: 'test' },
} as unknown as Artifact;

// The changes listed, without the raw payload folded under them.
const card = (payload: Record<string, unknown>) =>
  renderToStaticMarkup(
    <ProposalChanges
      proposal={{ id: 'x', run_id: 'r', project_id: 'p1', op: 'update_artifact', target_id: 'a1', payload, status: 'pending' } as unknown as Proposal}
      ctx={{ artifacts: { a1: artifact }, links: {} }}
    />,
  ).split('<details')[0];

describe('ProposalChanges', () => {
  it('shows only the text of an update that sends attributes as null, which leaves them as they are', () => {
    const html = card({ attributes: null, body: 'The mill shall leave no more than Ra 1.6 µm.' });
    expect(html).toContain('The mill shall leave no more than Ra 1.6 µm.');
    expect(html).not.toContain('attributes');
    expect(html).not.toContain('verification_method');
  });

  it('still shows an attribute an update does change', () => {
    const html = card({ attributes: { priority: 'must' } });
    expect(html).toContain('priority');
    expect(html).toContain('must');
  });
});
