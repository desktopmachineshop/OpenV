// Link type configuration with directional constraints
export interface LinkTypeRule {
  type: string;
  label: string;
  inverseLabel: string;
  allowedFromTypes: string[]; // Source artifact types
  allowedToTypes: string[];   // Target artifact types
  description: string;
}

export const linkTypeRules: LinkTypeRule[] = [
  {
    type: 'verifies',
    label: 'verifies',
    inverseLabel: 'verified by',
    allowedFromTypes: ['test-case'],
    allowedToTypes: ['requirement'],
    description: 'A test case demonstrates that a requirement is met',
  },
  {
    type: 'satisfies',
    label: 'satisfies',
    inverseLabel: 'satisfied by',
    allowedFromTypes: ['design-item'],
    allowedToTypes: ['requirement'],
    description: 'A design element implements or fulfills a requirement',
  },
  {
    type: 'mitigates',
    label: 'mitigates',
    inverseLabel: 'mitigated by',
    allowedFromTypes: ['design-item'],
    allowedToTypes: ['hazard'],
    description: 'A design element reduces or eliminates a hazard',
  },
  {
    type: 'decomposes-to',
    label: 'decomposes to',
    inverseLabel: 'decomposed from',
    allowedFromTypes: ['requirement'],
    allowedToTypes: ['requirement'],
    description: 'A high-level requirement is broken down into more specific sub-requirements',
  },
  {
    type: 'impacts',
    label: 'impacts',
    inverseLabel: 'impacted by',
    allowedFromTypes: ['*'], // All types
    allowedToTypes: ['*'],   // All types
    description: 'A change to one artifact affects another',
  },
  {
    type: 'relates-to',
    label: 'relates-to',
    inverseLabel: 'relates-to',
    allowedFromTypes: ['*'], // All types
    allowedToTypes: ['*'],   // All types
    description: 'A loose, non-semantic association between artifacts',
  },
];

// Helper function to get available link types for a source artifact type
export function getAvailableLinkTypes(sourceArtifactType: string): LinkTypeRule[] {
  return linkTypeRules.filter(rule => 
    rule.allowedFromTypes.includes('*') || 
    rule.allowedFromTypes.includes(sourceArtifactType)
  );
}

// Helper function to get allowed target artifact types for a link type
export function getAllowedTargetTypes(linkType: string): string[] {
  const rule = linkTypeRules.find(r => r.type === linkType);
  return rule ? rule.allowedToTypes : [];
}

// Helper function to get the display label for a link (with inverse support)
export function getLinkTypeLabel(linkType: string, isIncoming: boolean): string {
  const rule = linkTypeRules.find(r => r.type === linkType);
  if (!rule) return linkType;
  return isIncoming ? rule.inverseLabel : rule.label;
}

// Helper function to check if a link is valid
export function isLinkValid(linkType: string, fromArtifactType: string, toArtifactType: string): boolean {
  const rule = linkTypeRules.find(r => r.type === linkType);
  if (!rule) return false;
  
  const fromAllowed = rule.allowedFromTypes.includes('*') || rule.allowedFromTypes.includes(fromArtifactType);
  const toAllowed = rule.allowedToTypes.includes('*') || rule.allowedToTypes.includes(toArtifactType);
  
  return fromAllowed && toAllowed;
}
