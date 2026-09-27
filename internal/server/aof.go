package server

import (
	"bufio"
	"os"
	"sync"

	"github.com/portfolio/cachenova/internal/resp"
	"github.com/portfolio/cachenova/internal/store"
)

// AOF (append-only file) provides durability: every write command is appended
// to disk as RESP, and on startup the log is replayed to rebuild state. This
// mirrors Redis's default persistence mode and means a restarted container
// recovers its cache instead of coming up cold.
type AOF struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	path string
}

// OpenAOF opens (or creates) the append-only file at path.
func OpenAOF(path string) (*AOF, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &AOF{f: f, w: bufio.NewWriter(f), path: path}, nil
}

// Append writes a raw RESP-encoded command to the log and flushes it.
func (a *AOF) Append(raw []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.w.Write(raw); err != nil {
		return err
	}
	return a.w.Flush()
}

// Close flushes and closes the file.
func (a *AOF) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.w.Flush()
	return a.f.Close()
}

// Replay reads the whole AOF and re-applies write commands to the store,
// reconstructing state after a restart.
func (a *AOF) Replay(s *store.Store) error {
	if _, err := a.f.Seek(0, 0); err != nil {
		return err
	}
	rd := resp.NewReader(a.f)
	for {
		v, err := rd.ReadValue()
		if err != nil {
			break // EOF or truncated tail — stop replaying
		}
		if v.Type != '*' || len(v.Array) == 0 {
			continue
		}
		applyReplay(s, v.Array)
	}
	// Reposition writer at end for subsequent appends.
	if _, err := a.f.Seek(0, 2); err != nil {
		return err
	}
	return nil
}

// applyReplay applies a single decoded command array to the store during
// replay. Only mutating commands are relevant here.
func applyReplay(s *store.Store, args []resp.Value) {
	if len(args) == 0 {
		return
	}
	cmd := upper(string(args[0].Bulk))
	switch cmd {
	case "SET":
		if len(args) >= 3 {
			s.Set(string(args[1].Bulk), args[2].Bulk, 0)
		}
	case "DEL":
		keys := make([]string, 0, len(args)-1)
		for _, a := range args[1:] {
			keys = append(keys, string(a.Bulk))
		}
		s.Del(keys...)
	case "INCR":
		if len(args) >= 2 {
			_, _ = s.Incr(string(args[1].Bulk), 1)
		}
	case "DECR":
		if len(args) >= 2 {
			_, _ = s.Incr(string(args[1].Bulk), -1)
		}
	}
}
