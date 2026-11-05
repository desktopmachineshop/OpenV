import type { FeatureKey } from '../../generated/contract';

/**
 * The requirements page's Table (the document's hierarchy with every column,
 * folding under its headings) and Tree (the diagram). Without it the page
 * keeps Table, Tree (the list-style tree) and Layers.
 */
export const REQUIREMENTS_TREE_FEATURE: FeatureKey = 'requirements-tree';

export type RequirementsView = 'table' | 'tree' | 'layers';

/**
 * The view a ?view= value names; the table when it names none. With the
 * Table and Tree views on, Layers is gone and an old ?view=layers link opens
 * the Tree diagram, which covers what it showed; ?view=tree opens the diagram.
 */
export const viewFromParam = (raw: string | null, treeViews = false): RequirementsView => {
  if (treeViews) return raw === 'tree' || raw === 'layers' ? 'tree' : 'table';
  return raw === 'tree' || raw === 'layers' ? raw : 'table';
};
