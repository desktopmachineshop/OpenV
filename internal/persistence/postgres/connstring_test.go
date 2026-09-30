package postgres

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// standInPostgres speaks just enough of the PostgreSQL wire protocol to see
// what password a connection string delivers: it asks every client for a
// cleartext password, records it, and refuses it.
type standInPostgres struct {
	host, port string
	mu         sync.Mutex
	passwords  []string
}

func startStandInPostgres(t *testing.T) *standInPostgres {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &standInPostgres{}
	s.host, s.port, _ = net.SplitHostPort(ln.Addr().String())
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

// serve reads the startup message, asks for a cleartext password
// (AuthenticationCleartextPassword), records the PasswordMessage's password
// and answers with the error a real server gives a wrong one.
func (s *standInPostgres) serve(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(c)
	var n int32
	if binary.Read(r, binary.BigEndian, &n) != nil || n < 8 || n > 1<<16 {
		return
	}
	if _, err := io.CopyN(io.Discard, r, int64(n-4)); err != nil {
		return
	}
	if _, err := c.Write([]byte{'R', 0, 0, 0, 8, 0, 0, 0, 3}); err != nil {
		return
	}
	if typ, err := r.ReadByte(); err != nil || typ != 'p' {
		return
	}
	if binary.Read(r, binary.BigEndian, &n) != nil || n < 5 || n > 1<<16 {
		return
	}
	body := make([]byte, n-4)
	if _, err := io.ReadFull(r, body); err != nil {
		return
	}
	s.mu.Lock()
	s.passwords = append(s.passwords, strings.TrimSuffix(string(body), "\x00"))
	s.mu.Unlock()
	msg := "SFATAL\x00VFATAL\x00C28P01\x00Mpassword authentication failed for user \"postgres\"\x00\x00"
	out := binary.BigEndian.AppendUint32([]byte{'E'}, uint32(4+len(msg)))
	_, _ = c.Write(append(out, msg...))
}

// received is every password the stand-in was sent, in order.
func (s *standInPostgres) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.passwords...)
}

// TestConnStringPassesThePasswordExactlyAsSet: DB_PASSWORD is a credential,
// used exactly as set (#379, question 24), so the connection string the
// server builds from the DB_* settings delivers it byte for byte: spaces or
// a line break around it, a space, a quote or a backslash in it, or text
// that reads like more keys. Unquoted, lib/pq dropped the first two, cut
// the value at the third and read the backslash as an escape.
func TestConnStringPassesThePasswordExactlyAsSet(t *testing.T) {
	db := startStandInPostgres(t)
	passwords := []string{"secret", "secret\n", " secret", "\tsecret\r\n", "se cret", `se'cr\et`, "  ",
		"x host=elsewhere sslmode=require"}
	for _, pw := range passwords {
		_, err := Connect(ConnString(db.host, db.port, "postgres", pw, "openv"))
		if err == nil || !strings.Contains(err.Error(), "password authentication failed") {
			t.Errorf("password %q: connect error %v, want the stand-in's refusal", pw, err)
		}
	}
	got := db.received()
	if strings.Join(got, "\x00") != strings.Join(passwords, "\x00") {
		t.Errorf("the stand-in received the passwords\n  %q\nwant them exactly as set\n  %q", got, passwords)
	}
}
