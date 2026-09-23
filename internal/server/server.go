// Package server wires the config store, JEV client, filtering proxy and admin
// UI into two HTTP listeners: the proxy (public) and the admin (private).
package server

import (
	"context"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	"jev-safety-gateway/internal/admin"
	"jev-safety-gateway/internal/config"
	"jev-safety-gateway/internal/jev"
	"jev-safety-gateway/internal/proxy"
)

// keyAdapter bridges *config.Store to jev.KeyProvider.
type keyAdapter struct{ s *config.Store }

func (a keyAdapter) EnabledKeys() ([]jev.Key, error) {
	ks, err := a.s.EnabledKeys()
	if err != nil {
		return nil, err
	}
	out := make([]jev.Key, 0, len(ks))
	for _, k := range ks {
		out = append(out, jev.Key{ID: k.ID, Key: k.Key})
	}
	return out, nil
}

func (a keyAdapter) MarkKeyUsed(id int64) { a.s.MarkKeyUsed(id) }

// Server holds the two listeners.
type Server struct {
	proxySrv *http.Server
	adminSrv *http.Server
	// info is handed to the admin handler, which serves it from /api/version.
	// Run fills in the addresses once the listeners are bound.
	info *admin.Info
}

// New constructs both HTTP servers. info may be nil; when it is not, Run
// populates its addresses with the endpoints that actually came up.
func New(store *config.Store, webFS fs.FS, proxyAddr, adminAddr string, info *admin.Info) *Server {
	client := jev.New(keyAdapter{store})

	proxyHandler := proxy.New(store, client)
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	proxyMux.Handle("/", proxyHandler)

	adminHandler := admin.New(store, webFS, info)

	return &Server{
		info: info,
		proxySrv: &http.Server{
			Addr:              proxyAddr,
			Handler:           proxyMux,
			ReadHeaderTimeout: 15 * time.Second,
		},
		adminSrv: &http.Server{
			Addr:              adminAddr,
			Handler:           adminHandler,
			ReadHeaderTimeout: 15 * time.Second,
		},
	}
}

// Run starts both listeners and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	// Bind explicitly rather than via ListenAndServe so the console can be told
	// the endpoints that actually came up: a configured ":8080" resolves to
	// whatever the kernel picked, and a port clash is then reported before
	// either server claims to be running.
	proxyLn, err := net.Listen("tcp", s.proxySrv.Addr)
	if err != nil {
		return err
	}
	adminLn, err := net.Listen("tcp", s.adminSrv.Addr)
	if err != nil {
		_ = proxyLn.Close()
		return err
	}
	if s.info != nil {
		s.info.ProxyAddr = displayAddr(proxyLn.Addr())
		s.info.AdminAddr = displayAddr(adminLn.Addr())
	}

	errc := make(chan error, 2)
	go func() {
		log.Printf("proxy listening on %s", proxyLn.Addr())
		if err := s.proxySrv.Serve(proxyLn); err != nil && err != http.ErrServerClosed {
			errc <- err
		}
	}()
	go func() {
		log.Printf("admin listening on %s", adminLn.Addr())
		if err := s.adminSrv.Serve(adminLn); err != nil && err != http.ErrServerClosed {
			errc <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.proxySrv.Shutdown(shutCtx)
	_ = s.adminSrv.Shutdown(shutCtx)
	return nil
}

// displayAddr renders a bound listener address for the console. A wildcard bind
// is reported as ":8080" rather than the "[::]:8080" the kernel hands back: the
// console is answering "which port is this daemon on", and the wildcard spelling
// is noise there.
func displayAddr(a net.Addr) string {
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String()
	}
	if host == "::" || host == "0.0.0.0" {
		host = ""
	}
	return net.JoinHostPort(host, port)
}
