// An artifact opened in full, over the page, as a task opens from its open
// mark: what the artifact says in one card and its History in a card of
// its own beside it (beneath it where the window is narrow). Editing stays
// in the artifact's pane, which this is a wider reading of.
import React from 'react';
import type { Artifact } from '../api/client';
import { ArtifactDetails } from './ArtifactDetails';
import { ArtifactHistory } from './ArtifactHistory';
import { Modal } from './ui';

export type ArtifactFullProps = Omit<React.ComponentProps<typeof ArtifactDetails>, 'showHistory' | 'hideDescription' | 'previewVersion' | 'onClosePreview'> & {
  artifact: Artifact;
  onClose: () => void;
};

export const ArtifactFull: React.FC<ArtifactFullProps> = ({ onClose, ...details }) => (
  <Modal title={`${details.artifact.ref} ${details.artifact.title}`} width={1180} onClose={onClose}>
    <div data-artifact-full={details.artifact.id} style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(340px, 100%), 1fr))', gap: 12, alignItems: 'start' }}>
      <ArtifactDetails {...details} />
      <ArtifactHistory variant="card" artifactId={details.artifact.id} artifactVersion={details.artifact.version} projectId={details.projectId} onReferenceClick={details.onReferenceClick} />
    </div>
  </Modal>
);
