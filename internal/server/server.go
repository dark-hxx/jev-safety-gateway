// Package server wires the config store, JEV client, filtering proxy and admin
// UI into two HTTP listeners: the proxy (public) and the admin (private).
package server

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"time"

	"jev-gateway/internal/admin"
	"jev-gateway/internal/config"
	"jev-gateway/internal/jev"
	"jev-gateway/internal/proxy"
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
}

// New constructs both HTTP servers.
func New(store *config.Store, webFS fs.FS, proxyAddr, adminAddr string) *Server {
	client := jev.New(keyAdapter{store})

	proxyHandler := proxy.New(store, client)
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	proxyMux.Handle("/", proxyHandler)

	adminHandler := admin.New(store, webFS)

	return &Server{
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
	errc := make(chan error, 2)
	go func() {
		log.Printf("proxy listening on %s", s.proxySrv.Addr)
		if err := s.proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errc <- err
		}
	}()
	go func() {
		log.Printf("admin listening on %s", s.adminSrv.Addr)
		if err := s.adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
