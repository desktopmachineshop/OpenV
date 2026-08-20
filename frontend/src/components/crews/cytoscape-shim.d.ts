// Minimal ambient typings for cytoscape (no @types/cytoscape in package.json).
// The library is driven imperatively; a permissive typing keeps tsc happy
// without pulling in a full DefinitelyTyped dependency.
declare module 'cytoscape' {
  export type Core = any;
  export type EventObject = any;
  const cytoscape: (options?: any) => Core;
  export default cytoscape;
}
