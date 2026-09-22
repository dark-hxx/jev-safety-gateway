// Command gateway is a JEV-backed API request filtering reverse proxy.
//
// It reads each incoming request, extracts the user input based on the API
// path, evaluates it with the TypeSafe/JEV model, and forwards safe requests to
// the configured upstream while blocking harmful ones with HTTP 403.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"jev-gateway/internal/config"
	"jev-gateway/internal/server"
	"jev-gateway/web"
)

func main() {
	dbPath := env("JEV_DB_PATH", "/data/gateway.db")
	proxyAddr := env("JEV_PROXY_ADDR", ":8080")
	adminAddr := env("JEV_ADMIN_ADDR", ":8081")

	store, err := config.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Optional bootstrap from environment on first run.
	bootstrap(store)

	srv := server.New(store, web.FS(), proxyAddr, adminAddr)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
	log.Println("shutdown complete")
}

// bootstrap applies optional first-run configuration from environment variables:
// upstream URL, an initial JEV key, and the admin password. Existing values are
// never overwritten.
func bootstrap(store *config.Store) {
	set := store.Settings()
	changed := false
	if v := os.Getenv("JEV_UPSTREAM_URL"); v != "" && set.UpstreamBaseURL == "" {
		set.UpstreamBaseURL = v
		changed = true
	}
	if v := os.Getenv("JEV_BASE_URL"); v != "" {
		set.JEVBaseURL = v
		changed = true
	}
	if changed {
		if err := store.UpdateSettings(set); err != nil {
			log.Printf("bootstrap settings: %v", err)
		}
	}

	if v := os.Getenv("JEV_API_KEY"); v != "" {
		keys, _ := store.EnabledKeys()
		if len(keys) == 0 {
			if _, err := store.AddKey("bootstrap", v); err != nil {
				log.Printf("bootstrap key: %v", err)
			}
		}
	}

	if v := os.Getenv("JEV_ADMIN_PASSWORD"); v != "" && store.AdminHash() == "" {
		if err := store.SetAdminPasswordPlain(v); err != nil {
			log.Printf("bootstrap admin password: %v", err)
		}
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
