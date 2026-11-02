// Command dashboard is the CSEED officer dashboard server: the JSON API, the Discord sign-in
// and the static front end in one binary, backed by one SQLite file.
//
//	dashboard [serve]      run the server (default)
//	dashboard seed [-reset] load the sample data (FY2027), optionally into a fresh database
//	dashboard migrate      apply pending migrations and exit
//	dashboard healthcheck  exit 0 if the local server answers /api/health (container HEALTHCHECK)
//	dashboard version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"officerdashboard/internal/api"
	"officerdashboard/internal/auth"
	"officerdashboard/internal/cfsync"
	"officerdashboard/internal/cloudflare"
	"officerdashboard/internal/files"
	"officerdashboard/internal/notify"
	"officerdashboard/internal/purchases"
	"officerdashboard/internal/seed"
	"officerdashboard/internal/store"
	"officerdashboard/internal/web"
)

// version is stamped by the build: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)) }

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var err error
	switch cmd {
	case "serve":
		err = serve(args, getenv, log)
	case "seed":
		err = seedCmd(args, getenv, stdout, log)
	case "migrate":
		err = migrateCmd(args, getenv, stdout)
	case "provision-leadership":
		err = leadershipCmd(args, getenv, stdout)
	case "healthcheck":
		err = healthcheck(args, getenv)
	case "version":
		fmt.Fprintln(stdout, version)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\nusage: dashboard [serve|seed|migrate|provision-leadership|version]\n", cmd)
		return 2
	}
	if err != nil {
		log.Error("fatal", "error", err)
		return 1
	}
	return 0
}

// settings is the process configuration: environment variables, overridable by flags.
type settings struct {
	Cloudflare                       cloudflare.Config
	Addr, DBPath, AppURL, StaticDir  string
	StaticFallback                   string
	StaticFallbackStatus             int
	DiscordClientID, DiscordSecret   string
	BootstrapDirector                string
	BootstrapOwnerName               string
	WebhookURL                       string
	ServiceAccountJSON, DriveRoot    string
	ReceiptsDir                      string
	DevAuth                          bool
	TolerancePercent, ToleranceCents string
}

func loadSettings(fs *flag.FlagSet, args []string, getenv func(string) string) (settings, error) {
	env := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	s := settings{Cloudflare: cloudflare.FromEnv(getenv)}
	fs.StringVar(&s.Addr, "addr", env("ADDR", ":8090"), "listen address (ADDR)")
	fs.StringVar(&s.DBPath, "db", env("DB_PATH", "./data/dashboard.db"), "SQLite file (DB_PATH)")
	fs.StringVar(&s.AppURL, "app-url", env("APP_URL", ""), "public origin, used for OAuth redirects and links (APP_URL)")
	fs.StringVar(&s.StaticDir, "static", env("STATIC_DIR", ""), "serve the front end from this directory instead of the embedded build (STATIC_DIR)")
	fs.StringVar(&s.StaticFallback, "static-fallback", env("STATIC_FALLBACK", "404.html"), "page for unknown paths, relative to the front-end root (STATIC_FALLBACK)")
	fs.IntVar(&s.StaticFallbackStatus, "static-fallback-status", atoiOr(env("STATIC_FALLBACK_STATUS", ""), 404), "status code of the fallback page (STATIC_FALLBACK_STATUS)")
	if err := fs.Parse(args); err != nil {
		return s, err
	}
	s.DiscordClientID, s.DiscordSecret = env("DISCORD_CLIENT_ID", ""), env("DISCORD_CLIENT_SECRET", "")
	s.BootstrapDirector = env("BOOTSTRAP_OWNER_DISCORD_ID", env("BOOTSTRAP_DIRECTOR_DISCORD_ID", ""))
	s.BootstrapOwnerName = env("BOOTSTRAP_OWNER_NAME", "")
	s.WebhookURL = env("DISCORD_WEBHOOK_URL", "")
	s.ServiceAccountJSON = env("GOOGLE_SERVICE_ACCOUNT_JSON", "")
	s.DriveRoot = env("DRIVE_ROOT_FOLDER_ID", env("GOOGLE_DRIVE_FOLDER_ID", ""))
	s.ReceiptsDir = env("RECEIPTS_DIR", "")
	s.DevAuth = env("DEV_AUTH", "") == "1"
	s.TolerancePercent, s.ToleranceCents = env("AGENT_SETTLEMENT_TOLERANCE_PERCENT", ""), env("AGENT_SETTLEMENT_TOLERANCE_CENTS", "")
	if s.AppURL == "" {
		port := s.Addr
		if i := strings.LastIndex(port, ":"); i >= 0 {
			port = port[i+1:]
		}
		s.AppURL = "http://localhost:" + port
	}
	s.AppURL = strings.TrimRight(s.AppURL, "/")
	return s, nil
}

func atoiOr(s string, def int) int {
	var n int
	if _, err := fmt.Sscan(s, &n); err != nil || n == 0 {
		return def
	}
	return n
}

func openDB(path string) (*store.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}
	return store.Open(path)
}

// buildServer wires configuration into an api.Server.
func buildServer(s settings, db *store.DB, log *slog.Logger) (*api.Server, error) {
	mismatch, err := db.BootstrapOwner(context.Background(), s.BootstrapDirector, s.BootstrapOwnerName, time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("provision dashboard owner: %w", err)
	}
	if mismatch {
		log.Warn("bootstrap owner configuration ignored: ownership already belongs to another account")
	}

	cfg := api.Config{
		Cloudflare: &cfsync.Service{Client: cloudflare.New(s.Cloudflare), DB: db, Log: log},
		DB:         db, Now: time.Now, AppURL: s.AppURL, Version: version, Log: log,
		Tolerance: purchases.ToleranceFromEnv(s.TolerancePercent, s.ToleranceCents),
		DevAuth:   s.DevAuth,
	}
	if s.DevAuth && !api.DevBuild {
		log.Warn("DEV_AUTH=1 is ignored: this binary was not built with -tags dev, so /auth/dev does not exist")
		cfg.DevAuth = false
	}
	if s.DiscordClientID != "" && s.DiscordSecret != "" {
		cfg.Discord = auth.NewDiscord(s.DiscordClientID, s.DiscordSecret, s.AppURL+"/auth/callback")
	} else {
		log.Warn("Discord sign-in is not configured (DISCORD_CLIENT_ID / DISCORD_CLIENT_SECRET)")
	}
	if s.WebhookURL != "" {
		cfg.Notifier = notify.NewDiscord(s.WebhookURL)
	} else {
		cfg.Notifier = notify.Console{Log: log}
	}
	switch {
	case s.ServiceAccountJSON != "" && s.DriveRoot != "":
		d, err := files.NewDrive(s.ServiceAccountJSON, s.DriveRoot, nil)
		if err != nil {
			return nil, err
		}
		cfg.Files = d
	case s.ServiceAccountJSON != "":
		log.Warn("GOOGLE_SERVICE_ACCOUNT_JSON is set without DRIVE_ROOT_FOLDER_ID: receipt uploads will answer 503")
		cfg.Files = files.Unavailable{}
	case s.ReceiptsDir != "":
		cfg.Files = files.Local{Root: s.ReceiptsDir}
	case cfg.DevAuth:
		cfg.Files = files.Local{Root: filepath.Join(filepath.Dir(s.DBPath), "receipts")}
	default:
		log.Warn("no receipt storage configured (set Drive credentials, or RECEIPTS_DIR for local disk): uploads answer 503")
		cfg.Files = files.Unavailable{}
	}
	var root fs.FS
	switch {
	case s.StaticDir != "":
		root = os.DirFS(s.StaticDir)
	default:
		if e, ok := web.Embedded(); ok {
			root = e
		}
	}
	if root != nil {
		cfg.Static = web.Handler(root, web.Options{Fallback: s.StaticFallback, FallbackStatus: s.StaticFallbackStatus})
	} else {
		log.Warn("no front end to serve (set STATIC_DIR, or build with -tags embedweb)")
	}
	return api.New(cfg), nil
}

func serve(args []string, getenv func(string) string, log *slog.Logger) error {
	started := time.Now()
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	s, err := loadSettings(fs, args, getenv)
	if err != nil {
		return err
	}
	if err := validateAppURL(s.AppURL); err != nil {
		return err
	}
	db, err := openDB(s.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	srv, err := buildServer(s, db, log)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{
		Addr: s.Addr, Handler: api.AccessLog(srv.Handler(), log),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 2 * time.Minute,
		MaxHeaderBytes: 1 << 16,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go sessionJanitor(ctx, db, log)
	go srv.CloudflareDaily(ctx)
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	log.Info("listening", "addr", s.Addr, "db", s.DBPath, "app_url", s.AppURL, "version", version, "dev_build", api.DevBuild, "startup_ms", time.Since(started).Milliseconds())
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdown)
	}
	return nil
}

// sessionJanitor deletes expired sessions hourly.
func sessionJanitor(ctx context.Context, db *store.DB, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			err := db.Write(ctx, func(w *store.Queries) error {
				n, err := w.PurgeExpiredSessions(ctx, time.Now().UnixMilli())
				if err == nil && n > 0 {
					log.Info("purged expired sessions", "count", n)
				}
				return err
			})
			if err != nil {
				log.Error("session purge failed", "error", err)
			}
		}
	}
}

func seedCmd(args []string, getenv func(string) string, stdout io.Writer, log *slog.Logger) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	reset := fs.Bool("reset", false, "delete the database first")
	s, err := loadSettings(fs, args, getenv)
	if err != nil {
		return err
	}
	if *reset {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(s.DBPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	db, err := openDB(s.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := seed.Apply(context.Background(), db); err != nil {
		return err
	}
	var users, events, requests, lines int
	for q, dst := range map[string]*int{
		`SELECT count(*) FROM users`: &users, `SELECT count(*) FROM events`: &events,
		`SELECT count(*) FROM purchase_requests`: &requests, `SELECT count(*) FROM journal_lines`: &lines,
	} {
		if err := db.R.QueryRow(q).Scan(dst); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "seeded %s: %d users, %d events, %d requests, %d journal lines (FY%d)\n", s.DBPath, users, events, requests, lines, seed.FiscalYear)
	return nil
}

func migrateCmd(args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	s, err := loadSettings(fs, args, getenv)
	if err != nil {
		return err
	}
	db, err := openDB(s.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	var n int
	if err := db.R.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %d migrations applied\n", s.DBPath, n)
	return nil
}

// healthcheck asks the server on this machine whether it is up; the runtime image has no curl.
func healthcheck(args []string, getenv func(string) string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	s, err := loadSettings(fs, args, getenv)
	if err != nil {
		return err
	}
	addr := s.Addr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get("http://" + addr + "/api/health")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok":true`) || !strings.Contains(string(body), `"db":true`) {
		return fmt.Errorf("unhealthy: %d %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// validateAppURL refuses to serve unless APP_URL is https (localhost excepted). The Secure
// cookie flag, the __Host- prefix and HSTS all follow the URL scheme, so an http:// URL in
// production would silently drop them.
func validateAppURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("APP_URL %q is not a URL", raw)
	}
	if u.Scheme == "https" {
		return nil
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		if u.Scheme == "http" {
			return nil
		}
	}
	return fmt.Errorf("APP_URL %q must be https (plain http is allowed only for localhost): cookies would lose Secure, __Host- and HSTS", raw)
}
