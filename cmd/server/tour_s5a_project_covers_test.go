//go:build unix

package main

import (
	"bytes"
	"testing"
)

// TestTourS5aProjectCovers is the S5a tour's project covers area: a
// project's cover photo (invariants I3, I4, I8 (the image's Cache-Control,
// Content-Disposition and nosniff) and I16 (migration 0091's
// project_covers); feature key project-covers). Its golden is
// testdata/tour/s5a/project_covers.json.
//
// The owner, an ordinary account in its personal (nightly) workspace, keeps
// one project. The area walks a cover's life: the image fetched before
// there is one, the uploads the server refuses (an SVG, text declared as a
// PNG, a GIF declared as a PNG, a form with no file, a file over the 5 MiB
// cap), a PNG stored and read back with the project and its list, which then
// carry cover_updated_at, a WebP replacing it, the image fetched as its
// stored type, removal (twice: nothing to remove is no error) and the image
// gone. An outsider, an account in a workspace of its own, finds no project
// to read or change, and a request with no session is refused before
// anything is read. Generating a cover from a prompt is refused for an
// empty prompt and, since the tour's workspace has no OpenAI key, answered
// 409 with where to add one: the tour never calls a provider.
func TestTourS5aProjectCovers(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5a",
		key:   "project_covers",
		about: "Project cover photos: upload (each refusal, then a PNG and a WebP replacing it), the image as its " +
			"stored type, cover_updated_at on the project and its list, generation's refusals, removal, and the guards.",
		run: projectCoversTour,
	})
}

// projectCoversFile sends a multipart form whose "file" part holds data,
// declared as contentType.
func projectCoversFile(name, contentType string, data []byte) tourOpt {
	return rawBody(multipartForm(nil, tourFormFile{field: "file", name: name, contentType: contentType, data: data}))
}

func projectCoversTour(tr *tour) {
	o := tr.owner
	outsider := tr.register("outsider", "Tour Outsider",
		"an ordinary account in a workspace of its own, not a member of the owner's project")
	p := at("id", "{{project}}")
	cover := "POST /api/v1/projects/{id}/cover"

	tr.setup("the project", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour cover"}`)).capture("project", "/id")

	tr.step("the cover before there is one: 404", o, "GET /api/v1/projects/{id}/cover", p)
	tr.step("the project before it has a cover: no cover_updated_at", o, "GET /api/v1/projects/{id}", p)

	// Uploads the server refuses; none stores anything.
	tr.step("an SVG: refused by its declared type", o, cover, p,
		projectCoversFile("cover.svg", "image/svg+xml", []byte(tourSVG)))
	tr.step("text declared as a PNG: its bytes are no image", o, cover, p,
		projectCoversFile("cover.png", "image/png", []byte("not an image")))
	tr.step("a GIF declared as a PNG: an image, but not of the declared type", o, cover, p,
		projectCoversFile("cover.png", "image/png", []byte(tourGIF)))
	tr.step("a form with no file", o, cover, p, rawBody(multipartForm([][2]string{{"note", "no file here"}})))
	tr.step("a file of 5 MiB and a byte: over the cover's cap", o, cover, p,
		projectCoversFile("cover.png", "image/png", append([]byte(tourPNG[:16]), bytes.Repeat([]byte{0}, 5<<20+1-16)...)))
	tr.step("still no cover: the refused uploads stored nothing", o, "GET /api/v1/projects/{id}/cover", p)

	// The cover's life.
	tr.step("upload a PNG: the project, now with cover_updated_at", o, cover, p,
		projectCoversFile("cover.png", "image/png", []byte(tourPNG)))
	tr.step("fetch it: the PNG, inline, nosniff, privately cacheable for a day", o, "GET /api/v1/projects/{id}/cover", p)
	tr.step("the project carries cover_updated_at", o, "GET /api/v1/projects/{id}", p)
	tr.step("and so does its entry in the list", o, "GET /api/v1/projects")
	tr.step("replace it with a WebP", o, cover, p, projectCoversFile("cover.webp", "image/webp", []byte(tourWebP)))
	tr.step("fetch it: now image/webp", o, "GET /api/v1/projects/{id}/cover", p)

	// The guards: a project the caller cannot reach is one no row has (I3).
	tr.step("the outsider fetches the cover: 404", outsider, "GET /api/v1/projects/{id}/cover", p)
	tr.step("the outsider uploads one: 404, nothing stored", outsider, cover, p,
		projectCoversFile("cover.png", "image/png", []byte(tourPNG)))
	tr.step("the outsider removes it: 404", outsider, "DELETE /api/v1/projects/{id}/cover", p)
	tr.step("no session: 401", tr.anon, "GET /api/v1/projects/{id}/cover", p)
	tr.step("a project that does not exist: 404", o, "GET /api/v1/projects/{id}/cover", at("id", "{{phantom}}"))
	tr.step("still the WebP: the refused calls changed nothing", o, "GET /api/v1/projects/{id}/cover", p)

	// Generation, from a prompt with the workspace's OpenAI key: this
	// workspace has none, so the request is answered with what to do
	// before any provider is called; a bad prompt is refused first.
	gen := "POST /api/v1/projects/{id}/cover/generate"
	tr.step("generate with an empty prompt: 400", o, gen, p, jsonBody(`{"prompt":"  "}`))
	tr.step("generate with a malformed body: 400", o, gen, p, jsonBody(`{`))
	tr.step("generate without a workspace OpenAI key: 409, saying where to add one", o, gen, p,
		jsonBody(`{"prompt":"A cover for the tour, wide shot, no text"}`))
	tr.step("the outsider generates one: 404", outsider, gen, p, jsonBody(`{"prompt":"x"}`))
	tr.step("still the WebP: no generation stored anything", o, "GET /api/v1/projects/{id}/cover", p)

	// Removal.
	tr.step("remove it: the project, without cover_updated_at", o, "DELETE /api/v1/projects/{id}/cover", p)
	tr.step("remove it again: nothing to remove, 200 all the same", o, "DELETE /api/v1/projects/{id}/cover", p)
	tr.step("fetch it once removed: 404", o, "GET /api/v1/projects/{id}/cover", p)
}
