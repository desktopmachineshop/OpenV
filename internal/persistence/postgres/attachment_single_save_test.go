package postgres

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// A figure is stored one way: SaveWithFigureRef, through the attachment
// service's CreateFigure, which numbers it from its artifact's counter and
// records its first version (#379 bug 159). AttachmentRepository.Save and
// the service's CreateAttachment, which wrapped it, stored a bare row with
// no number, no version and no check of the artifact's project; nothing
// called either, so they are gone rather than given the same guard, and a
// way round the guard cannot come back unnoticed.
func TestAFigureIsStoredOnlyThroughSaveWithFigureRef(t *testing.T) {
	type bareSave interface {
		Save(*attachments.Attachment) error
	}
	type bareCreate interface {
		CreateAttachment(*attachments.Attachment) error
	}
	if _, ok := NewAttachmentRepository(nil).(bareSave); ok {
		t.Error("AttachmentRepository has Save, which stores a figure without SaveWithFigureRef's number, version and checks")
	}
	if _, ok := attachments.NewDefaultService(nil).(bareCreate); ok {
		t.Error("the attachment service has CreateAttachment, a way to store a figure round CreateFigure")
	}
}
