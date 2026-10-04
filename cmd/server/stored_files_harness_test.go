//go:build unix

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestStoredFilesLeaveWithTheirRows runs the real server, four boots on a
// database of its own (OPENV_TEST_DATABASE_URL; skipped when unset) and one
// uploads directory, and watches the files there:
//
//   - deleting a figure removes the file of every version of it (#379 bug
//     145: the earlier versions' stayed);
//   - at the next boot, with OPENV_UPLOAD_SWEEP=off, nothing is swept or
//     recorded (question 55);
//   - at the boot after, as on the first boot after this update on a
//     database from before it, the sweep removes the stored files no row
//     names, a logo no workspace has and a picture no account has, and
//     keeps the rest (questions 48 and 56); and the purge, at start,
//     removes the files of the workspace it purges, its figures with every
//     version, its evidence and its logo (bug 143: they all stayed);
//   - the boot after that sweeps no more: a file no row names is kept.
func TestStoredFilesLeaveWithTheirRows(t *testing.T) {
	bin := serverBinary(t)
	s, db := bootServer(t, bin, nil)
	uploads := filepath.Join(s.tmp, "uploads")
	conn, err := sql.Open("postgres", db.url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var session *http.Cookie
	send := func(method, path, contentType string, body []byte) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, s.base+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if session != nil {
			req.AddCookie(session)
		}
		resp, err := s.client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, s.output())
		}
		answer, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		for _, c := range resp.Cookies() {
			if c.Name == "openv_session" {
				session = c
			}
		}
		return resp.StatusCode, answer
	}
	call := func(method, path, body string, want int, into interface{}) {
		t.Helper()
		contentType := ""
		if body != "" {
			contentType = "application/json"
		}
		status, answer := send(method, path, contentType, []byte(body))
		if status != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, status, answer, want)
		}
		if into != nil {
			if err := json.Unmarshal(answer, into); err != nil {
				t.Fatalf("%s %s: %v in %s", method, path, err, answer)
			}
		}
	}
	upload := func(path, filename, mime string, content []byte, fields map[string]string, into interface{}) {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for k, v := range fields {
			if err := form.WriteField(k, v); err != nil {
				t.Fatal(err)
			}
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
		header.Set("Content-Type", mime)
		part, err := form.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(content)
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		status, answer := send(http.MethodPost, path, form.FormDataContentType(), body.Bytes())
		if status != http.StatusCreated && status != http.StatusOK {
			t.Fatalf("upload to %s: %d %s", path, status, answer)
		}
		if into != nil {
			if err := json.Unmarshal(answer, into); err != nil {
				t.Fatalf("upload to %s: %v in %s", path, err, answer)
			}
		}
	}
	picture := func(shade uint8) []byte {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.Set(0, 0, color.RGBA{shade, shade, shade, 255})
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	// stored lists the uploads directory, a subdirectory's files as
	// dir/name.
	stored := func() []string {
		t.Helper()
		var names []string
		err := filepath.WalkDir(uploads, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(uploads, path)
			names = append(names, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		sort.Strings(names)
		return names
	}
	// plant writes a file in the uploads directory, last changed two hours
	// ago, as one left behind by a delete before this update would be.
	plant := func(name string) string {
		t.Helper()
		path := filepath.Join(uploads, name)
		if err := os.WriteFile(path, []byte("left behind"), 0o644); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		return name
	}
	reboot := func(env map[string]string) {
		t.Helper()
		if _, _, err := s.terminate(15 * time.Second); err != nil {
			t.Fatalf("stop the server: %v\n%s", err, s.output())
		}
		session = nil
		extra := map[string]string{"UPLOADS_DIR": uploads}
		for k, v := range env {
			extra[k] = v
		}
		next, err := startServer(t, bin, db, extra)
		if err != nil {
			t.Fatalf("boot again: %v\n%s", err, next.output())
		}
		s = next
	}
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := conn.Exec(query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}

	body, _ := json.Marshal(map[string]string{"email": harnessEmail, "password": harnessPassword, "name": "Boot Harness"})
	if status, answer := send(http.MethodPost, "/api/v1/auth/register", "application/json", body); status != http.StatusOK || session == nil {
		t.Fatalf("register: %d %s\n%s", status, answer, s.output())
	}
	var workspaces struct {
		ActiveOrg string `json:"active_org"`
	}
	call(http.MethodGet, "/api/v1/orgs", "", http.StatusOK, &workspaces)
	var project, requirement, other, bundle struct {
		ID string `json:"id"`
	}
	call(http.MethodPost, "/api/v1/projects", `{"name":"Rig"}`, http.StatusCreated, &project)
	call(http.MethodPost, "/api/v1/artifacts", `{"project_id":"`+project.ID+`","type":"requirement","title":"Brake in time"}`,
		http.StatusCreated, &requirement)
	call(http.MethodPost, "/api/v1/artifacts", `{"project_id":"`+project.ID+`","type":"requirement","title":"Stop"}`,
		http.StatusCreated, &other)

	// Bug 145: a figure in two versions, deleted.
	var doomed, kept struct {
		ID string `json:"id"`
	}
	upload("/api/v1/attachments/upload", "brake.png", "image/png", picture(10), map[string]string{"artifact_id": requirement.ID}, &doomed)
	upload("/api/v1/attachments/"+doomed.ID+"/versions", "brake-v2.png", "image/png", picture(20), nil, nil)
	if got := stored(); len(got) != 2 {
		t.Fatalf("stored after two versions of a figure: %q, want two files", got)
	}
	call(http.MethodDelete, "/api/v1/attachments/"+doomed.ID, "", http.StatusNoContent, nil)
	if got := stored(); len(got) != 0 {
		t.Errorf("stored after the figure's delete: %q, want nothing (#379 bug 145: the first version's file stayed)", got)
	}

	// What the purge will take: a figure in two versions, an evidence file
	// and the workspace's logo.
	upload("/api/v1/attachments/upload", "stop.png", "image/png", picture(30), map[string]string{"artifact_id": other.ID}, &kept)
	upload("/api/v1/attachments/"+kept.ID+"/versions", "stop-v2.png", "image/png", picture(40), nil, nil)
	call(http.MethodPost, "/api/v1/projects/"+project.ID+"/evidence-bundles", `{"title":"Bench logs"}`, http.StatusCreated, &bundle)
	upload("/api/v1/evidence-bundles/"+bundle.ID+"/files", "bench.log", "text/plain", []byte("stop at 12 ms\n"), nil, nil)
	upload("/api/v1/orgs/"+workspaces.ActiveOrg+"/logo", "logo.png", "image/png", picture(50), nil, nil)
	workspaceFiles := stored()
	if len(workspaceFiles) != 4 {
		t.Fatalf("stored before the purge: %q, want four files (two versions of a figure, an evidence file, the logo)", workspaceFiles)
	}

	// The account's own profile picture, which stays.
	var me struct {
		ID string `json:"id"`
	}
	upload("/api/v1/me/avatar", "me.png", "image/png", picture(60), nil, &me)
	avatar := "avatars/" + me.ID + ".png"

	// Questions 48 and 56: what deletes before this update left behind, on
	// a database the sweep has not yet run on (the first boot after the
	// update on a database from before it), and what is not the server's.
	orphans := []string{
		plant(uuid.New().String() + "_deleted project.png"), plant("evidence-" + uuid.New().String()),
		plant("org-logos/" + uuid.New().String() + ".png"), plant("avatars/" + uuid.New().String() + ".webp"),
	}
	notOurs := plant("operator notes.txt")
	exec(`DELETE FROM boot_tasks`)

	// Question 55: with the sweep off, the boot sweeps and records nothing.
	before := stored()
	reboot(map[string]string{"OPENV_UPLOAD_SWEEP": "off"})
	if got := stored(); strings.Join(got, "\n") != strings.Join(before, "\n") {
		t.Errorf("stored after a boot with the sweep off: %q, want everything as it was: %q", got, before)
	}
	if !strings.Contains(string(s.stderr.Bytes()), `msg="upload sweep: off (OPENV_UPLOAD_SWEEP=off); nothing swept or recorded"`) {
		t.Errorf("the boot log does not say the sweep is off:\n%s", s.stderr.Bytes())
	}
	var records int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM boot_tasks`).Scan(&records); err != nil || records != 0 {
		t.Errorf("boot_tasks after a boot with the sweep off: %d rows, %v; want none", records, err)
	}

	// Bug 143: the workspace was deleted 31 days ago.
	exec(`UPDATE organizations SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, workspaces.ActiveOrg)
	reboot(nil)
	want := []string{avatar, notOurs}
	deadline := time.Now().Add(15 * time.Second)
	for got := stored(); len(got) > len(want) && time.Now().Before(deadline); got = stored() {
		time.Sleep(100 * time.Millisecond) // the purge runs in a goroutine at start
	}
	if got := stored(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("stored after the boot: %q, want only %q (the sweep took %q, the purge %q)\n%s", got, want, orphans, workspaceFiles, s.output())
	}
	log := string(s.stderr.Bytes())
	for _, name := range orphans {
		path := filepath.Join(uploads, filepath.FromSlash(name))
		if !strings.Contains(log, " path="+path+" ") && !strings.Contains(log, ` path="`+path+`"`) {
			t.Errorf("the boot log does not name %s as removed by the sweep:\n%s", name, log)
		}
	}
	for _, line := range []string{
		`msg="upload sweep: removed a logo no workspace has"`, `msg="upload sweep: removed a profile picture no account has"`,
		`msg="upload sweep: done"`, " removed=2 logos_removed=1 avatars_removed=1 bytes=44 ",
	} {
		if !strings.Contains(log, line) {
			t.Errorf("the boot log does not hold %s:\n%s", line, log)
		}
	}
	if !strings.Contains(log, `msg="purged expired deleted workspaces" count=1`) || !strings.Contains(log, "files_removed=4") {
		t.Errorf("the boot log does not count the four files the purge removed:\n%s", log)
	}

	// The sweep ran once: a file no row names, left after it, stays.
	later := plant(uuid.New().String() + "_after the sweep.png")
	reboot(nil)
	if got := stored(); len(got) != 3 || !strings.Contains(strings.Join(got, "\n"), later) {
		t.Errorf("stored after the next boot: %q, want %q kept beside %q", got, later, want)
	}
	if strings.Contains(string(s.stderr.Bytes()), "upload sweep") {
		t.Errorf("the boot after the sweep's swept again:\n%s", s.stderr.Bytes())
	}
}
