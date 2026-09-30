//go:build unix

package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// standInDatabase speaks just enough of the PostgreSQL wire protocol for a
// boot to show what password the server sends from the DB_* settings (#379,
// question 24): it asks every client for a cleartext password, records it,
// and refuses it as a real server refuses a wrong one, so the boot stops at
// its connect. It listens on a port no server or proxy of this process has
// had (listenUnhanded).
type standInDatabase struct {
	port      int
	mu        sync.Mutex
	passwords []string
}

// standInDatabasePortPlaceholder stands for the stand-in's port in a golden.
const standInDatabasePortPlaceholder = "<stand-in database port>"

func startStandInDatabase(t *testing.T) *standInDatabase {
	t.Helper()
	ln := listenUnhanded(t)
	s := &standInDatabase{port: ln.Addr().(*net.TCPAddr).Port}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.serve(c)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	return s
}

// env points DB_HOST and DB_PORT at the stand-in.
func (s *standInDatabase) env() map[string]string {
	return map[string]string{"DB_HOST": "127.0.0.1", "DB_PORT": strconv.Itoa(s.port)}
}

// serve reads the startup message, asks for a cleartext password
// (AuthenticationCleartextPassword), records the PasswordMessage's password
// and answers with the ErrorResponse a real server gives a wrong one.
func (s *standInDatabase) serve(c net.Conn) {
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
func (s *standInDatabase) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.passwords...)
}
