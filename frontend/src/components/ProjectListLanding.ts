// Arriving in the app, as opposed to opening the projects list on purpose.
// Signing in and the app's root send the member to /projects with this
// navigation state; with the redesign on, the list then opens a project's
// Home (the Home design has no separate start page). The rail's Projects,
// Home's "All projects" and the top bar link to /projects without it, so
// they still show the list. It is history state, not a query parameter, so
// no address changes and a reload of the list stays on the list.

export const LANDING_STATE = { landing: true } as const;

/** Whether a location's state marks an arrival in the app. */
export const isLanding = (state: unknown): boolean =>
  typeof state === 'object' && state !== null && (state as { landing?: unknown }).landing === true;
