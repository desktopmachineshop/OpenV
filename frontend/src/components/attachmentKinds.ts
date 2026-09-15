import { Attachment, AttachmentKind } from '../api/client';

// How the app talks about an attached file.
//
// The kind comes from the server, which derives it from the MIME type, so
// nothing here re-decides what something is. What lives here is presentation:
// what to call a format in a sentence, what a tile shows when there is no
// thumbnail to show, and which of the model formats has a preview.

/** Extension of a name, lowercased and with its dot, or '' when it has none. */
export const extensionOf = (a: Pick<Attachment, 'original_filename' | 'filename'>): string => {
  const name = a.original_filename || a.filename || '';
  const dot = name.lastIndexOf('.');
  return dot > 0 ? name.slice(dot).toLowerCase() : '';
};

/**
 * Whether this is an STL, the one CAD format with a preview.
 *
 * By extension as well as MIME type: an STL uploaded before the catalogue
 * existed, or handed over by a browser that guessed, can still be read.
 */
export const isStl = (a: Attachment): boolean =>
  a.mime_type === 'model/stl' || extensionOf(a) === '.stl';

/** What a format is called in a sentence: "STEP model", "PDF", "PNG image". */
export const formatLabel = (a: Attachment): string => {
  const ext = extensionOf(a).replace('.', '').toUpperCase();
  const named: Record<string, string> = {
    STEP: 'STEP model',
    STP: 'STEP model',
    IGES: 'IGES model',
    IGS: 'IGES model',
    STL: 'STL mesh',
    '3MF': '3MF model',
    OBJ: 'OBJ mesh',
    PLY: 'PLY mesh',
    GLTF: 'glTF model',
    GLB: 'glTF model',
    DXF: 'DXF drawing',
    DWG: 'DWG drawing',
    SLDPRT: 'SolidWorks part',
    SLDASM: 'SolidWorks assembly',
    SLDDRW: 'SolidWorks drawing',
    IPT: 'Inventor part',
    IAM: 'Inventor assembly',
    IDW: 'Inventor drawing',
    CATPART: 'CATIA part',
    CATPRODUCT: 'CATIA product',
    F3D: 'Fusion 360 model',
    X_T: 'Parasolid model',
    X_B: 'Parasolid model',
    '3DM': 'Rhino model',
    SCAD: 'OpenSCAD source',
    PDF: 'PDF',
  };
  if (named[ext]) return named[ext];
  if (a.kind === 'image') return ext ? `${ext} image` : 'Image';
  return ext || 'File';
};

/** The glyph a tile shows when there is no thumbnail to show. */
export const kindGlyph = (kind: AttachmentKind): string => {
  switch (kind) {
    case 'document':
      return '📄';
    case 'model':
      return '📐';
    case 'image':
      return '🖼';
    default:
      return '📎';
  }
};

/** What a figure is called: its reference where it has one, else its filename. */
export const attachmentLabel = (a: Pick<Attachment, 'figure_ref' | 'filename'>): string =>
  a.figure_ref || a.filename;

/**
 * A file size a person can read.
 *
 * CAD files are the reason this exists: a reviewer deciding whether to fetch
 * an assembly on a hotel connection wants "84 MB", not 88080384.
 */
export const formatFileSize = (bytes: number): string => {
  if (!Number.isFinite(bytes) || bytes < 0) return '';
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
};

/**
 * What the file picker offers, mirroring the server's catalogue
 * (internal/domain/attachments/kind.go). The browser's picker is a
 * convenience, never the gate: the server decides what it will store.
 */
export const ACCEPTED_UPLOAD_TYPES = [
  'image/*',
  '.pdf',
  '.step', '.stp', '.iges', '.igs', '.stl', '.3mf', '.obj', '.ply', '.gltf', '.glb',
  '.dxf', '.dwg',
  '.sldprt', '.sldasm', '.slddrw', '.ipt', '.iam', '.idw', '.prt', '.asm',
  '.catpart', '.catproduct', '.f3d', '.x_t', '.x_b', '.3dm', '.scad',
].join(',');

/** Extensions the server accepts that a browser will not type as an image. */
const NON_IMAGE_EXTENSIONS = new Set(
  ACCEPTED_UPLOAD_TYPES.split(',').filter((t) => t.startsWith('.'))
);

/**
 * Whether a chosen file is worth sending.
 *
 * Deliberately permissive where the browser is unreliable: it types most CAD
 * formats as application/octet-stream, so the extension is what is checked.
 * The server refuses anything outside its catalogue regardless — this only
 * saves a member the round trip.
 */
export const isAcceptedUpload = (file: File): boolean => {
  if (file.type.startsWith('image/')) return true;
  const dot = file.name.lastIndexOf('.');
  return dot > 0 && NON_IMAGE_EXTENSIONS.has(file.name.slice(dot).toLowerCase());
};

/** What to tell someone whose file was refused before it was even sent. */
export const UNSUPPORTED_UPLOAD_MESSAGE =
  'That file type cannot be attached. Attach an image, a PDF, or a CAD file such as STEP, IGES, STL, 3MF, DXF or a native part file.';

/**
 * Release gates (REQ-137), named here beside what they gate.
 *
 * Both govern only what a member may WRITE. Reading is never gated: a figure
 * a colleague on the nightly channel attached opens for everybody, and a
 * "##" citation already in a description resolves for everybody, because a
 * gate that made an existing file unreadable or existing prose broken would
 * be a regression dressed as a release policy.
 */
export const ATTACHMENT_FORMATS_FEATURE = 'attachment-formats';
export const FIGURE_CITATIONS_FEATURE = 'figure-citations';
