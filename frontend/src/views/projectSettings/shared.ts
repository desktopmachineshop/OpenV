import type React from 'react';
import type { AttributeDataType } from '../../api/client';

// What the ProjectSettings shell and its tabs share. The tabs are props-only,
// so the data shapes they show reach them from here as types and no tab
// imports the API layer.
export type {
  ArtifactTypeDef,
  AttributeDataType,
  AttributeDefinition,
  OrgTeam,
  Party,
  Project,
  ProjectMember,
  RepoConnection,
  ShareLink,
  ShareLinkRole,
  TeamGrant,
  User,
} from '../../api/client';

/** A state setter the shell passes down with the value it sets. */
export type Setter<T> = React.Dispatch<React.SetStateAction<T>>;

export const ATTRIBUTE_DATA_TYPES: AttributeDataType[] = ['text', 'number', 'date', 'enum', 'boolean'];

export interface AttributeForm {
  key: string;
  label: string;
  data_type: AttributeDataType;
  enum_values: string;
  applies_to_type: string;
  required: boolean;
}

export const emptyAttributeForm: AttributeForm = {
  key: '',
  label: '',
  data_type: 'text',
  enum_values: '',
  applies_to_type: '',
  required: false,
};

export const th: React.CSSProperties = {
  textAlign: 'left',
  fontSize: 12,
  color: 'var(--text-muted)',
  padding: '8px 10px',
  borderBottom: '1px solid var(--border-soft)',
};

export const td: React.CSSProperties = {
  padding: '8px 10px',
  fontSize: 13,
  color: 'var(--text)',
  borderBottom: '1px solid var(--surface-inset)',
};

export interface RepoForm {
  id: string;
  name: string;
  remote_url: string;
  default_branch: string;
}

export const emptyRepoForm: RepoForm = { id: '', name: '', remote_url: '', default_branch: 'main' };
