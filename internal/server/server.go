// Package server is CacheNova's RESP-speaking TCP server. It is wire-compatible
// with redis-cli and standard Redis client libraries.
package server

import (
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/portfolio/cachenova/internal/resp"
	"github.com/portfolio/cachenova/internal/store"
)

// Server accepts RESP connections and dispatches commands to the store.
type Server struct {
	store *store.Store
	aof   *AOF
}

// New builds a server over the given store and optional AOF (may be nil).
func New(s *store.Store, aof *AOF) *Server {
	return &Server{store: s, aof: aof}
}

// Serve accepts connections on ln until it is closed.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	rd := resp.NewReader(conn)
	for {
		v, err := rd.ReadValue()
		if err != nil {
			return
		}
		if v.Type != '*' || len(v.Array) == 0 {
			continue
		}
		reply := s.dispatch(v.Array)
		if _, err := conn.Write(reply); err != nil {
			return
		}
	}
}

// isWrite reports whether a command mutates state (and must be persisted).
func isWrite(cmd string) bool {
	switch cmd {
	case "SET", "DEL", "INCR", "DECR", "EXPIRE":
		return true
	}
	return false
}

// dispatch executes a command array and returns the RESP-encoded reply.
func (s *Server) dispatch(args []resp.Value) []byte {
	cmd := upper(string(args[0].Bulk))

	// Persist mutating commands to the AOF before applying, so a crash can't
	// leave an applied-but-unlogged write.
	if s.aof != nil && isWrite(cmd) {
		raw := reencode(args)
		if err := s.aof.Append(raw); err != nil {
			log.Printf("aof append: %v", err)
		}
	}

	switch cmd {
	case "PING":
		if len(args) > 1 {
			return resp.EncodeBulk(args[1].Bulk)
		}
		return resp.EncodeSimpleString("PONG")
	case "ECHO":
		if len(args) > 1 {
			return resp.EncodeBulk(args[1].Bulk)
		}
		return resp.EncodeError("ERR wrong number of arguments for 'echo'")
	case "SET":
		return s.cmdSet(args)
	case "GET":
		if len(args) != 2 {
			return resp.EncodeError("ERR wrong number of arguments for 'get'")
		}
		if val, ok := s.store.Get(string(args[1].Bulk)); ok {
			return resp.EncodeBulk(val)
		}
		return resp.EncodeBulk(nil)
	case "DEL":
		keys := bulkStrings(args[1:])
		return resp.EncodeInt(int64(s.store.Del(keys...)))
	case "EXISTS":
		keys := bulkStrings(args[1:])
		return resp.EncodeInt(int64(s.store.Exists(keys...)))
	case "INCR":
		return s.cmdIncrBy(args, 1)
	case "DECR":
		return s.cmdIncrBy(args, -1)
	case "TTL":
		if len(args) != 2 {
			return resp.EncodeError("ERR wrong number of arguments for 'ttl'")
		}
		return resp.EncodeInt(s.store.TTL(string(args[1].Bulk)))
	case "COMMAND", "CLIENT", "CONFIG", "HELLO":
		// Handshake/noise commands some clients send on connect.
		return resp.EncodeSimpleString("OK")
	default:
		return resp.EncodeError("ERR unknown command '" + cmd + "'")
	}
}

// cmdSet implements SET key value [EX seconds | PX millis].
func (s *Server) cmdSet(args []resp.Value) []byte {
	if len(args) < 3 {
		return resp.EncodeError("ERR wrong number of arguments for 'set'")
	}
	key := string(args[1].Bulk)
	val := args[2].Bulk
	var ttl time.Duration
	// Optional trailing options: EX <seconds> or PX <milliseconds>.
	for i := 3; i+1 < len(args); i += 2 {
		opt := upper(string(args[i].Bulk))
		amount, err := strconv.Atoi(string(args[i+1].Bulk))
		if err != nil {
			return resp.EncodeError("ERR value is not an integer or out of range")
		}
		switch opt {
		case "EX":
			ttl = time.Duration(amount) * time.Second
		case "PX":
			ttl = time.Duration(amount) * time.Millisecond
		}
	}
	s.store.Set(key, val, ttl)
	return resp.EncodeSimpleString("OK")
}

func (s *Server) cmdIncrBy(args []resp.Value, delta int64) []byte {
	if len(args) != 2 {
		return resp.EncodeError("ERR wrong number of arguments")
	}
	n, err := s.store.Incr(string(args[1].Bulk), delta)
	if err != nil {
		return resp.EncodeError("ERR value is not an integer or out of range")
	}
	return resp.EncodeInt(n)
}

// --- helpers ----------------------------------------------------------------

func upper(s string) string { return strings.ToUpper(s) }

func bulkStrings(vals []resp.Value) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = string(v.Bulk)
	}
	return out
}

// reencode turns a decoded command array back into RESP bytes for the AOF.
func reencode(args []resp.Value) []byte {
	elems := make([][]byte, len(args))
	for i, a := range args {
		elems[i] = resp.EncodeBulk(a.Bulk)
	}
	return resp.EncodeArray(elems)
}
