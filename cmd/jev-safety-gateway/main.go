// Command jev-safety-gateway is a JEV-backed API request filtering reverse proxy.
//
// It reads each incoming request, extracts the user input based on the API
// path, evaluates it with the TypeSafe/JEV model, and forwards safe requests to
// the configured upstream while blocking harmful ones with HTTP 403.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"jev-safety-gateway/internal/config"
	"jev-safety-gateway/internal/logx"
	"jev-safety-gateway/internal/server"
	"jev-safety-gateway/web"
)

func main() {
	// Before anything else: when the Windows SCM starts this process there is no
	// console and stderr is discarded, so JEV_LOG_FILE is the only record of
	// what it says. The installer sets it; on Linux and in Docker the log goes
	// to journald or the container runtime and the variable stays unset.
	if _, err := logx.InitFile(os.Getenv("JEV_LOG_FILE")); err != nil {
		log.Fatalf("open log file: %v", err)
	}

	if err := runMain(); err != nil {
		log.Fatalf("server: %v", err)
	}
	log.Println("shutdown complete")
}

// serve opens the store, wires the two listeners and blocks until ctx is
// cancelled. It is the whole program minus process-level concerns (signal
// handling, service control), which is what lets the Windows service path in
// entry_windows.go reuse it unchanged.
func serve(ctx context.Context) error {
	// The DB default is relative to the working directory so a local run lands in
	// <cwd>/data/ and needs no override. Docker pins the absolute /data path via
	// ENV (see Dockerfile); the systemd unit and the Windows service installer
	// each set an absolute path of their own.
	dbPath := env("JEV_DB_PATH", "./data/jev-safety-gateway.db")
	proxyAddr := env("JEV_PROXY_ADDR", ":8080")
	// Loopback by default: the console has no protection beyond its own login,
	// and a bare-metal install that binds every interface puts it on the public
	// internet. Docker overrides this to :8081 (see Dockerfile) because the port
	// mapping, not the bind, is what keeps it private there.
	adminAddr := env("JEV_ADMIN_ADDR", "127.0.0.1:8081")

	// Verbose per-request tracing for development.
	logx.Debug = truthy(os.Getenv("JEV_DEBUG"))
	if logx.Debug {
		log.Println("debug logging enabled (JEV_DEBUG)")
	}

	store, err := config.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	// Optional bootstrap from environment on first run.
	bootstrap(store)

	// The admin console reports the bound endpoints, so hand it a value that
	// outlives this call: Run fills the addresses in once it has listened.
	info := buildInfo()
	srv := server.New(store, web.FS(), proxyAddr, adminAddr, &info)

	return srv.Run(ctx)
}

// consoleContext is the foreground-process cancellation source: Ctrl+C or, on
// Linux, SIGTERM from systemd.
func consoleContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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

func truthy(v string) bool {
	switch v {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true
	}
	return false
}
