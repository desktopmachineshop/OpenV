package attachments

import (
	"bytes"
	"path"
	"strings"
)

// What an artifact may carry, and what the platform does with it.
//
// A figure started as an image and is now any file a requirement points at: a
// drawing, the supplier datasheet the tolerance came from, the STEP model the
// interface is defined against. They share one numbering sequence and one
// reference shape (REQ-17-FIG-3) because they share the thing that matters —
// a citable, never-reissued name for one attached file — and splitting the
// counter would mean two artifacts' worth of bookkeeping for no reader's
// benefit.
//
// What differs is what can be DONE with the bytes, and that is what Kind
// names: an image can be rendered inline and embedded in a generated
// document; a PDF can be read in a viewer but never embedded as a picture; a
// CAD model is opened in the tool that owns the format. Every consumer that
// used to assume "attachment means image" asks Kind instead.

// Kind is the family a figure's file belongs to.
type Kind string

const (
	// KindImage is a raster or vector picture: the only kind a generated
	// report can embed, and the only kind served inline.
	KindImage Kind = "image"
	// KindDocument is a paginated document — today, PDF.
	KindDocument Kind = "document"
	// KindModel is CAD: a solid model, an assembly or a 2D drawing file.
	KindModel Kind = "model"
	// KindOther is an accepted file that is none of the above.
	KindOther Kind = "other"
)

// format is one entry in the accepted catalogue.
type format struct {
	// mime is the canonical type the platform records, whatever the browser
	// declared. Browsers guess from the extension and send
	// application/octet-stream for everything they do not recognise, which is
	// most CAD formats, so the extension is what decides.
	mime string
	kind Kind
	// magic, when set, is a prefix the file's bytes must start with. It is a
	// "did you upload what you said" check rather than a security control:
	// nothing but an image is ever rendered on the API origin, and every
	// other kind is handed over as an opaque download. Formats with no
	// dependable signature — most vendor CAD containers — leave it empty
	// and are accepted on their extension.
	magic []string
}

// catalogue maps a lowercased file extension to what the platform will do
// with it. Extensions rather than MIME types are the key because the
// extension is the one thing about a CAD upload a browser reports reliably.
var catalogue = map[string]format{
	// Images. The magic numbers are checked by the image sniffer, which also
	// has to deal with SVG having none.
	".jpg":  {mime: "image/jpeg", kind: KindImage},
	".jpeg": {mime: "image/jpeg", kind: KindImage},
	".png":  {mime: "image/png", kind: KindImage},
	".gif":  {mime: "image/gif", kind: KindImage},
	".webp": {mime: "image/webp", kind: KindImage},
	".svg":  {mime: "image/svg+xml", kind: KindImage},
	".tif":  {mime: "image/tiff", kind: KindImage},
	".tiff": {mime: "image/tiff", kind: KindImage},
	".bmp":  {mime: "image/bmp", kind: KindImage},

	// Documents.
	".pdf": {mime: "application/pdf", kind: KindDocument, magic: []string{"%PDF-"}},

	// Neutral CAD interchange, which is what a supplier is most likely to be
	// sent and the only CAD a reviewer outside the design team can open.
	".step": {mime: "model/step", kind: KindModel, magic: []string{"ISO-10303-21"}},
	".stp":  {mime: "model/step", kind: KindModel, magic: []string{"ISO-10303-21"}},
	".iges": {mime: "model/iges", kind: KindModel},
	".igs":  {mime: "model/iges", kind: KindModel},
	// STL has two encodings and the binary one opens with 80 free-form bytes,
	// so only the ASCII form can be recognised; the sniffer lets a binary STL
	// through on its extension.
	".stl":  {mime: "model/stl", kind: KindModel},
	".3mf":  {mime: "model/3mf", kind: KindModel, magic: []string{"PK\x03\x04"}},
	".obj":  {mime: "model/obj", kind: KindModel},
	".ply":  {mime: "model/ply", kind: KindModel},
	".gltf": {mime: "model/gltf+json", kind: KindModel},
	".glb":  {mime: "model/gltf-binary", kind: KindModel, magic: []string{"glTF"}},

	// 2D drawings.
	".dxf": {mime: "image/vnd.dxf", kind: KindModel},
	".dwg": {mime: "image/vnd.dwg", kind: KindModel, magic: []string{"AC10", "AC1", "MC0"}},

	// Native formats. No dependable signature and no viewer; they are stored
	// so that the file a team actually works from is the one the requirement
	// points at, and downloaded to be opened in the tool that owns it.
	".sldprt":     {mime: "application/octet-stream", kind: KindModel},
	".sldasm":     {mime: "application/octet-stream", kind: KindModel},
	".slddrw":     {mime: "application/octet-stream", kind: KindModel},
	".ipt":        {mime: "application/octet-stream", kind: KindModel},
	".iam":        {mime: "application/octet-stream", kind: KindModel},
	".idw":        {mime: "application/octet-stream", kind: KindModel},
	".prt":        {mime: "application/octet-stream", kind: KindModel},
	".asm":        {mime: "application/octet-stream", kind: KindModel},
	".catpart":    {mime: "application/octet-stream", kind: KindModel},
	".catproduct": {mime: "application/octet-stream", kind: KindModel},
	".f3d":        {mime: "application/octet-stream", kind: KindModel},
	".x_t":        {mime: "application/octet-stream", kind: KindModel},
	".x_b":        {mime: "application/octet-stream", kind: KindModel},
	".3dm":        {mime: "application/octet-stream", kind: KindModel},
	".scad":       {mime: "text/plain", kind: KindModel},
}

// imageMimes is the set a declared Content-Type may name directly. An image is
// the one kind a browser types reliably, and the one kind whose declared type
// has consequences — it decides how the file is later served — so it is
// accepted from the upload as well as from the extension.
var imageMimes = map[string]bool{
	"image/jpeg":    true,
	"image/png":     true,
	"image/gif":     true,
	"image/webp":    true,
	"image/svg+xml": true,
	"image/tiff":    true,
	"image/bmp":     true,
}

// KindForMime is the family a recorded MIME type belongs to. It is what every
// reader — exports, reports, the API's own serving rules — asks instead of
// assuming an attachment is an image.
func KindForMime(mime string) Kind {
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	switch {
	case strings.HasPrefix(m, "image/vnd.dxf"), strings.HasPrefix(m, "image/vnd.dwg"):
		// A DXF is a drawing file, not a picture: nothing can render it as
		// one, so it must not be classed with the images.
		return KindModel
	case strings.HasPrefix(m, "image/"):
		return KindImage
	case m == "application/pdf":
		return KindDocument
	case strings.HasPrefix(m, "model/"):
		return KindModel
	default:
		return KindOther
	}
}

// IsImage reports whether a recorded MIME type is a picture: renderable in the
// browser and embeddable in a generated document. Everything that used to
// assume an attachment was an image now asks this.
func IsImage(mime string) bool { return KindForMime(mime) == KindImage }

// AcceptUpload decides whether a file may be attached and under what type.
//
// The extension leads: it is the only thing a browser reports dependably for
// CAD, where the declared type is almost always application/octet-stream. A
// declared image type is honoured for an image whose name says nothing,
// because that is the case where the browser does know. Anything the
// catalogue does not name is refused — an attachment is a file other members
// will be handed, so the set of what can be stored is a decision rather than
// whatever arrives.
func AcceptUpload(declaredMime, filename string) (mime string, kind Kind, ok bool) {
	ext := strings.ToLower(path.Ext(filename))
	if f, found := catalogue[ext]; found {
		return f.mime, f.kind, true
	}
	declared := strings.ToLower(strings.TrimSpace(declaredMime))
	if i := strings.IndexByte(declared, ';'); i >= 0 {
		declared = strings.TrimSpace(declared[:i])
	}
	if imageMimes[declared] {
		return declared, KindImage, true
	}
	return "", "", false
}

// ContentMatchesFormat checks a non-image upload's leading bytes against the
// signature its format carries, where it has one. It is deliberately not the
// gate that keeps the platform safe — that is the serving policy, which hands
// every non-image over as an opaque download — but it catches the ordinary
// mistake of a file renamed to the wrong extension before a reviewer
// downloads it and finds out.
//
// A format with no signature, and any empty signature list, passes: some
// formats genuinely have nothing to check, and refusing them would mean
// refusing most CAD.
func ContentMatchesFormat(filename string, data []byte) bool {
	if len(data) == 0 {
		return false
	}
	f, found := catalogue[strings.ToLower(path.Ext(filename))]
	if !found || len(f.magic) == 0 {
		return true
	}
	for _, m := range f.magic {
		if bytes.HasPrefix(data, []byte(m)) {
			return true
		}
	}
	return false
}

// AcceptedExtensions lists every extension the catalogue names, so the API can
// tell a client what it may offer without the list being written out twice.
func AcceptedExtensions() []string {
	out := make([]string, 0, len(catalogue))
	for ext := range catalogue {
		out = append(out, ext)
	}
	return out
}
