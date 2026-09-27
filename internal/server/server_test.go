package server

import (
	"net"
	"testing"
	"time"

	"github.com/portfolio/cachenova/internal/resp"
	"github.com/portfolio/cachenova/internal/store"
)

// startTestServer spins up a CacheNova server on a random port and returns a
// connected client reader/writer for issuing RESP commands.
func startTestServer(t *testing.T) net.Conn {
	t.Helper()
	st := store.New()
	srv := New(st, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { ln.Close(); st.Close() })

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// cmd sends a RESP command array and returns the decoded reply.
func cmd(t *testing.T, conn net.Conn, rd *resp.Reader, parts ...string) resp.Value {
	t.Helper()
	elems := make([][]byte, len(parts))
	for i, p := range parts {
		elems[i] = resp.EncodeBulk([]byte(p))
	}
	if _, err := conn.Write(resp.EncodeArray(elems)); err != nil {
		t.Fatal(err)
	}
	v, err := rd.ReadValue()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSetGetDelExists(t *testing.T) {
	conn := startTestServer(t)
	rd := resp.NewReader(conn)

	if v := cmd(t, conn, rd, "SET", "user:1", "alice"); v.Str != "OK" {
		t.Fatalf("SET expected OK, got %q", v.Str)
	}
	if v := cmd(t, conn, rd, "GET", "user:1"); string(v.Bulk) != "alice" {
		t.Fatalf("GET expected alice, got %q", v.Bulk)
	}
	if v := cmd(t, conn, rd, "EXISTS", "user:1"); v.Int != 1 {
		t.Fatalf("EXISTS expected 1, got %d", v.Int)
	}
	if v := cmd(t, conn, rd, "DEL", "user:1"); v.Int != 1 {
		t.Fatalf("DEL expected 1, got %d", v.Int)
	}
	if v := cmd(t, conn, rd, "GET", "user:1"); !v.IsNil {
		t.Fatalf("GET after DEL expected nil, got %q", v.Bulk)
	}
}

func TestIncrDecr(t *testing.T) {
	conn := startTestServer(t)
	rd := resp.NewReader(conn)

	if v := cmd(t, conn, rd, "INCR", "hits"); v.Int != 1 {
		t.Fatalf("INCR expected 1, got %d", v.Int)
	}
	if v := cmd(t, conn, rd, "INCR", "hits"); v.Int != 2 {
		t.Fatalf("INCR expected 2, got %d", v.Int)
	}
	if v := cmd(t, conn, rd, "DECR", "hits"); v.Int != 1 {
		t.Fatalf("DECR expected 1, got %d", v.Int)
	}
}

func TestExpiry(t *testing.T) {
	conn := startTestServer(t)
	rd := resp.NewReader(conn)

	cmd(t, conn, rd, "SET", "session", "token", "PX", "80")
	if v := cmd(t, conn, rd, "GET", "session"); string(v.Bulk) != "token" {
		t.Fatalf("expected token before expiry, got %q", v.Bulk)
	}
	time.Sleep(150 * time.Millisecond)
	if v := cmd(t, conn, rd, "GET", "session"); !v.IsNil {
		t.Fatalf("expected nil after expiry, got %q", v.Bulk)
	}
}

func TestPing(t *testing.T) {
	conn := startTestServer(t)
	rd := resp.NewReader(conn)
	if v := cmd(t, conn, rd, "PING"); v.Str != "PONG" {
		t.Fatalf("PING expected PONG, got %q", v.Str)
	}
}
