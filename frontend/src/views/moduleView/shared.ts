import type React from 'react';

// What the ModuleView shell and its panes share. The panes are props-only, so
// the data shapes they show reach them from here as types and no pane imports
// the API layer.
export type { Artifact, Baseline } from '../../api/client';

/** A state setter the shell passes down with the value it sets. */
export type Setter<T> = React.Dispatch<React.SetStateAction<T>>;

/** A row of the filter panel as the shell keeps it, with the id React keys it by. */
export type FilterPanelRow = { id: string; field: string; value: string; comparator: string };

/** A saved filter preset: its name and the search and filters it holds, as JSON. */
export type FilterPreset = { name: string; data: string };
