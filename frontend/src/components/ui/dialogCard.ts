import React from 'react';

/**
 * The card of a centred dialog. Desktop dialogs ask for a width in pixels;
 * on a phone that width is ignored and the card fills the screen minus a
 * small margin, with tighter padding so the content keeps its room. Every
 * dialog shell (Modal, ConfirmDialog, PromptDialog and the bespoke panels)
 * goes through here so they agree.
 */
export const dialogCardStyle = (isPhone: boolean, width: number, padding = 24): React.CSSProperties =>
  isPhone
    ? {
        width: '100%',
        maxWidth: 'calc(100vw - 16px)',
        maxHeight: 'calc(100vh - 32px)',
        // Prefer the dynamic viewport where supported: the URL bar comes and goes.
        ...({ maxHeight: 'calc(100dvh - 32px)' } as React.CSSProperties),
        overflowY: 'auto',
        padding: 16,
        borderRadius: 8,
        margin: 0,
      }
    : {
        width,
        maxWidth: 'calc(100vw - 40px)',
        maxHeight: 'calc(100vh - 60px)',
        overflowY: 'auto',
        padding,
        borderRadius: 8,
        margin: 0,
      };
