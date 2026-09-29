# Moving from the current theme

The app and website today use `frontend/src/theme.css`, whose colours come from the 2013 Flat UI palette. Replace each old custom property with the new token below, then delete the old one. Where the old token did two jobs, the table says which new token to pick for each.

| Old (`theme.css`) | New token | Note |
| --- | --- | --- |
| `--bg-app` | `bg` | |
| `--surface` | `surface` | |
| `--surface-alt` | `surface-hover` | Alternate rows go away; use hover and selection instead. |
| `--surface-inset` | `surface-sunken` | Table headers, code wells. |
| `--surface-hover` | `surface-hover` | |
| `--border`, `--border-soft` | `border` | Hairlines. |
| `--border` on inputs and buttons | `border-control` | Needs 3:1. |
| `--neutral-soft` | `neutral-chip` | |
| `--neutral-mid`, `--neutral` | `border-control` or `text-muted` | Grey buttons become `ov-btn` secondary. |
| `--text` | `text` | |
| `--text-secondary`, `--text-body` | `text-secondary` | |
| `--text-muted` | `text-muted` | |
| `--accent`, `--accent-alt` | `primary` | `--accent-alt` retires. |
| `--accent-strong` | `primary-hover` | |
| `--accent-text` | `link` or `text-selected` | |
| `--accent-fg` | `text-on-primary` | Dark ink in the dark theme. |
| `--success`, `--success-bright`, `--success-strong`, `--success-text` | `success` | Chips use `success-bg` and `success-border`. |
| `--danger`, `--danger-strong` | `danger` | |
| `--warning`, `--warning-bright`, `--warning-strong`, `--warning-text` | `warning` | |
| `--purple`, `--purple-soft`, `--purple-strong` | `agent` | |
| `--tint-blue`, `--tint-blue-border` | `info-bg` | |
| `--tint-green`, `--tint-green-border` | `success-bg`, `success-border` | |
| `--tint-red`, `--tint-red-border` | `danger-bg`, `danger-border` | |
| `--tint-yellow`, `--tint-yellow-border` | `warning-bg`, `warning-border` | |
| `--tint-purple`, `--tint-purple-border` | `agent-bg`, `agent-border` | |
| `--overlay` | `overlay` | |
| `--sidebar-bg`, `--sidebar-menu-bg` | `surface` | The sidebar becomes light. |
| `--sidebar-border` | `border` | |
| `--sidebar-text`, `--sidebar-text-dim`, `--sidebar-text-faint` | `text-secondary`, `text-muted` | |
| `--code-block-bg`, `--code-block-text` | `surface-sunken`, `text` | Code blocks become light in the light theme. |

## Classes

| Old | New |
| --- | --- |
| `.button` (green fill) | `ov-btn ov-btn--primary` |
| `.button-secondary` (grey fill) | `ov-btn` |
| `.nav button` (blue fill) | `ov-btn` or `ov-btn--ghost` |
| `.card` | `ov-panel` |
| Inline status chip styles (`chipStyle`, `bandStyles`) | `ov-chip ov-chip--<state>` |

## Order of work
1. Add IBM Plex and the new `tokens.css` beside the old theme, and alias the old custom properties to the new tokens so nothing breaks.
2. Move shared pieces first: buttons, chips, refs, inputs, the sidebar and top bar.
3. Replace inline hex values and emoji in components screen by screen, starting with Requirements.
4. Retake the website screenshots once the Requirements screen is done.
5. Delete the old aliases.
