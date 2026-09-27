// Command cachenova runs a Redis-compatible in-memory cache with optional
// append-only-file durability and a live web dashboard.
//
// CacheNova is a self-hosted cache/session/rate-limit store for small teams
// that want Redis semantics without paying for ElastiCache. It speaks RESP2,
// so redis-cli and every standard Redis client "just work", and it deploys as
// a single tiny container on AWS ECS/Fargate.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/portfolio/cachenova/internal/dashboard"
	"github.com/portfolio/cachenova/internal/server"
	"github.com/portfolio/cachenova/internal/store"
)

func main() {
	var (
		respAddr  = flag.String("resp", ":6380", "RESP (Redis protocol) listen address")
		httpAddr  = flag.String("http", ":8080", "dashboard/API listen address")
		aofPath   = flag.String("aof", "", "append-only-file path for durability (empty = disabled)")
		staticDir = flag.String("static", "web/static", "dashboard static files directory")
	)
	flag.Parse()

	st := store.New()
	defer st.Close()

	var aof *server.AOF
	if *aofPath != "" {
		a, err := server.OpenAOF(*aofPath)
		if err != nil {
			log.Fatalf("open aof: %v", err)
		}
		if err := a.Replay(st); err != nil {
			log.Fatalf("replay aof: %v", err)
		}
		aof = a
		defer aof.Close()
		log.Printf("AOF durability enabled at %s (state recovered)", *aofPath)
	}

	srv := server.New(st, aof)
	ln, err := net.Listen("tcp", *respAddr)
	if err != nil {
		log.Fatalf("listen resp: %v", err)
	}
	go func() {
		log.Printf("RESP server on %s (try: redis-cli -p %s)", *respAddr, portOf(*respAddr))
		if err := srv.Serve(ln); err != nil {
			log.Printf("resp server stopped: %v", err)
		}
	}()

	httpSrv := &http.Server{Addr: *httpAddr, Handler: dashboard.Handler(st, *staticDir)}
	go func() {
		log.Printf("dashboard on http://localhost%s", *httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
	_ = ln.Close()
}

func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return addr
}
