import { LINK_TYPE_RULES } from '../generated/contract';

// Link type configuration with directional constraints
export interface LinkTypeRule {
  type: string;
  label: string;
  inverseLabel: string;
  allowedFromTypes: readonly string[]; // Source artifact types ('*' for all)
  allowedToTypes: readonly string[];   // Target artifact types ('*' for all)
  description: string;
}

// The link types with their labels, directions and descriptions (the
// description is the link picker's tooltip): links.GetLinkTypeRules
// (internal/domain/links), read from the generated contract (refactor plan
// X4b) with Go's text and in Go's order. No rule needs a UI override: the
// refines tooltip, quirk Q6, has carried Go's whole description since the
// fix for #379 bug 80.
export const linkTypeRules: LinkTypeRule[] = [...LINK_TYPE_RULES];

// Helper function to get available link types for a source artifact type
export function getAvailableLinkTypes(sourceArtifactType: string): LinkTypeRule[] {
  return linkTypeRules.filter(rule => 
    rule.allowedFromTypes.includes('*') || 
    rule.allowedFromTypes.includes(sourceArtifactType)
  );
}

// Helper function to get allowed target artifact types for a link type
export function getAllowedTargetTypes(linkType: string): readonly string[] {
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
