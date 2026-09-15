import { Attachment } from '../api/client';
import {
  ACCEPTED_UPLOAD_TYPES,
  attachmentLabel,
  extensionOf,
  formatFileSize,
  formatLabel,
  isAcceptedUpload,
  isStl,
  kindGlyph,
} from './attachmentKinds';

const att = (over: Partial<Attachment>): Attachment => ({
  id: 'a1',
  artifact_id: 'r1',
  filename: 'REQ-17-FIG-1.png',
  original_filename: 'pump.png',
  mime_type: 'image/png',
  file_path: '/uploads/x',
  file_size: 1024,
  kind: 'image',
  version: 1,
  created_at: '',
  ...over,
});

// A browser types most CAD formats as application/octet-stream, so anything
// that decides by MIME alone rejects the files this feature exists for.
const file = (name: string, type: string): File =>
  ({ name, type, size: 1000 } as File);

describe('isAcceptedUpload', () => {
  it('accepts a CAD file the browser could not name', () => {
    expect(isAcceptedUpload(file('manifold.STEP', 'application/octet-stream'))).toBe(true);
    expect(isAcceptedUpload(file('bracket.stl', ''))).toBe(true);
    expect(isAcceptedUpload(file('housing.SLDPRT', 'application/octet-stream'))).toBe(true);
  });

  it('accepts images and PDFs', () => {
    expect(isAcceptedUpload(file('pump.png', 'image/png'))).toBe(true);
    expect(isAcceptedUpload(file('scan', 'image/jpeg'))).toBe(true);
    expect(isAcceptedUpload(file('datasheet.pdf', 'application/pdf'))).toBe(true);
  });

  it('turns away what the server would refuse anyway', () => {
    expect(isAcceptedUpload(file('installer.exe', 'application/x-msdownload'))).toBe(false);
    expect(isAcceptedUpload(file('page.html', 'text/html'))).toBe(false);
    expect(isAcceptedUpload(file('everything.zip', 'application/zip'))).toBe(false);
    expect(isAcceptedUpload(file('mystery', 'application/octet-stream'))).toBe(false);
  });
});

describe('the file picker offer', () => {
  it('names the families a member is told they can attach', () => {
    expect(ACCEPTED_UPLOAD_TYPES).toContain('image/*');
    for (const ext of ['.pdf', '.step', '.stl', '.dxf', '.sldprt']) {
      expect(ACCEPTED_UPLOAD_TYPES).toContain(ext);
    }
  });
});

describe('formatLabel', () => {
  it('names a CAD format by what it is, not by its extension', () => {
    expect(formatLabel(att({ original_filename: 'manifold.stp', kind: 'model' }))).toBe('STEP model');
    expect(formatLabel(att({ original_filename: 'panel.dxf', kind: 'model' }))).toBe('DXF drawing');
    expect(formatLabel(att({ original_filename: 'housing.SLDPRT', kind: 'model' }))).toBe(
      'SolidWorks part'
    );
  });

  it('names the ordinary kinds too', () => {
    expect(formatLabel(att({ original_filename: 'datasheet.pdf', kind: 'document' }))).toBe('PDF');
    expect(formatLabel(att({ original_filename: 'pump.png' }))).toBe('PNG image');
  });

  it('falls back to the extension, then to something rather than nothing', () => {
    expect(formatLabel(att({ original_filename: 'part.xyz', kind: 'other' }))).toBe('XYZ');
    expect(formatLabel(att({ original_filename: '', filename: '', kind: 'other' }))).toBe('File');
  });
});

describe('isStl', () => {
  it('finds an STL by type or by name, so an older upload still previews', () => {
    expect(isStl(att({ mime_type: 'model/stl', original_filename: 'bracket.stl' }))).toBe(true);
    expect(
      isStl(att({ mime_type: 'application/octet-stream', original_filename: 'bracket.STL' }))
    ).toBe(true);
  });

  it('does not claim the formats it cannot read', () => {
    expect(isStl(att({ mime_type: 'model/step', original_filename: 'manifold.step' }))).toBe(false);
    expect(isStl(att({ mime_type: 'application/pdf', original_filename: 'd.pdf' }))).toBe(false);
  });
});

describe('formatFileSize', () => {
  it('reads as a person would say it', () => {
    expect(formatFileSize(512)).toBe('512 B');
    expect(formatFileSize(2048)).toBe('2.0 KB');
    expect(formatFileSize(25 * 1024 * 1024)).toBe('25 MB');
    expect(formatFileSize(1.5 * 1024 * 1024 * 1024)).toBe('1.5 GB');
  });

  it('says nothing rather than something wrong', () => {
    expect(formatFileSize(NaN)).toBe('');
    expect(formatFileSize(-1)).toBe('');
  });
});

describe('the small helpers', () => {
  it('names a figure by its reference, and by its filename when it has none', () => {
    expect(attachmentLabel({ figure_ref: 'REQ-17-FIG-1', filename: 'x.png' })).toBe('REQ-17-FIG-1');
    expect(attachmentLabel({ filename: 'x.png' })).toBe('x.png');
  });

  it('reads an extension without being fooled by a dot in the name', () => {
    expect(extensionOf(att({ original_filename: 'rev 2.1 manifold.STEP' }))).toBe('.step');
    expect(extensionOf(att({ original_filename: 'noextension', filename: 'f' }))).toBe('');
  });

  it('gives every kind a glyph, so no tile is blank', () => {
    for (const kind of ['image', 'document', 'model', 'other'] as const) {
      expect(kindGlyph(kind).length).toBeGreaterThan(0);
    }
  });
});
