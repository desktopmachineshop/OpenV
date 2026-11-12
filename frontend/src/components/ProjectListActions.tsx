import React from 'react';
import { ActionMenu } from './ui/ActionMenu';

interface Props {
  /** The project's name, for the menu's accessible name. */
  name: string;
  /** One ⋯ menu of named actions (ui-refresh) rather than four glyphs. */
  menu: boolean;
  onTemplate: (e: React.MouseEvent) => void;
  onEdit: (e: React.MouseEvent) => void;
  onDownload: () => void;
  onDelete: () => void;
}

// The handlers stop the card's own click; a menu entry has no event to stop,
// since the menu's wrapper already kept the click from the card.
const NO_EVENT = { stopPropagation() {} } as React.MouseEvent;

/**
 * What can be done to a project from its row on the projects page: save it
 * as a template, edit it, download it, delete it.
 */
export const ProjectActions: React.FC<Props> = ({ name, menu, onTemplate, onEdit, onDownload, onDelete }) => {
  if (menu) {
    return (
      <div className="project-actions" onClick={(e) => e.stopPropagation()} data-help-key="projects.download">
        <ActionMenu
          label={`Actions for ${name}`}
          triggerStyle={{ minWidth: 32, minHeight: 32, border: 'none', background: 'none', color: 'var(--text-muted)', fontSize: 18, cursor: 'pointer', borderRadius: 6 }}
          items={[
            { key: 'edit', label: 'Edit project', onSelect: () => onEdit(NO_EVENT) },
            { key: 'download', label: 'Download', onSelect: onDownload },
            { key: 'template', label: 'Save as template', onSelect: () => onTemplate(NO_EVENT) },
            { key: 'delete', label: 'Delete project', onSelect: onDelete, danger: true },
          ]}
        />
      </div>
    );
  }
  return (
    <div className="project-actions">
      <button className="icon-btn template-btn" onClick={onTemplate} title="Save as template">
        ⧉
      </button>
      <button className="icon-btn edit-btn" onClick={onEdit} title="Edit project">
        ✎
      </button>
      <button
        className="icon-btn export-btn"
        onClick={(e) => {
          e.stopPropagation();
          onDownload();
        }}
        title="Download this project — choose a format, sections and attachments"
        data-help-key="projects.download"
      >
        ↓
      </button>
      <button
        className="icon-btn delete-btn"
        onClick={(e) => {
          e.stopPropagation();
          onDelete();
        }}
        title="Delete project"
      >
        ✕
      </button>
    </div>
  );
};
