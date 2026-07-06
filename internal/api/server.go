package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/branding"
	"github.com/xiqi/wispbox/internal/security"
	webserve "github.com/xiqi/wispbox/internal/web"
	webassets "github.com/xiqi/wispbox/web"
)

// Handler assembles the complete HTTPS-side handler: APIs plus the embedded
// frontend with SPA routing.
func (a *App) Handler() (http.Handler, error) {
	mux := http.NewServeMux()
	a.MailH.Mount(mux)
	a.AdminH.Mount(mux)
	a.SetupH.Mount(mux)
	mux.HandleFunc("GET /api/brand", a.brand)

	// Unmatched /api paths return JSON 404 instead of the SPA shell.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	dist, err := webassets.Dist()
	if err != nil {
		return nil, fmt.Errorf("embedded frontend unavailable: %w", err)
	}
	mux.Handle("/", webserve.Handler(dist, func(r *http.Request) bool {
		return a.Store.IsInitialized(r.Context())
	}))

	return a.recoverer(security.Headers(mux)), nil
}

func (a *App) brand(w http.ResponseWriter, r *http.Request) {
	httpjson.Write(w, http.StatusOK, map[string]any{"brand": branding.Current(r.Context(), a.Store)})
}

// httpHandler is what listens on port 80 (8080 in dev): ACME challenges
// always; in production everything else redirects to HTTPS, while dev mode
// serves the full app over plain HTTP for convenience.
func (a *App) httpHandler(full http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
			token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
			if keyAuth, ok := a.Solver.Lookup(token); ok {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte(keyAuth))
				return
			}
			http.NotFound(w, r)
			return
		}
		if a.Cfg.IsDev() {
			full.ServeHTTP(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusMovedPermanently)
	})
}

func (a *App) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.Log.Error("panic serving request", "path", r.URL.Path, "panic", rec)
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Serve runs both listeners and the background loops until ctx is canceled.
func (a *App) Serve(ctx context.Context) error {
	handler, err := a.Handler()
	if err != nil {
		return err
	}

	// The fallback certificate guarantees HTTPS always has something to
	// serve, even before the first real certificate is issued.
	primary := a.Store.GetSettingDefault(ctx, "primary_hostname", "")
	if err := a.CertManager.EnsureFallback(ctx, primary); err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              a.Cfg.HTTPAddr,
		Handler:           a.httpHandler(handler),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	httpsSrv := &http.Server{
		Addr:              a.Cfg.HTTPSAddr,
		Handler:           handler,
		TLSConfig:         a.CertManager.TLSConfig(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	errCh := make(chan error, 2)
	go func() {
		a.Log.Info("HTTP listener started", "addr", a.Cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http listener: %w", err)
		}
	}()
	go func() {
		a.Log.Info("HTTPS listener started", "addr", a.Cfg.HTTPSAddr)
		if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("https listener: %w", err)
		}
	}()

	// Background loops.
	renewInterval := time.Hour
	if a.Cfg.IsDev() {
		renewInterval = time.Minute
	}
	go a.CertManager.RunRenewalLoop(ctx, renewInterval)
	go a.sessionSweeper(ctx)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		a.Log.Error("listener failed", "error", err)
		shutdown(httpSrv, httpsSrv)
		return err
	}
	shutdown(httpSrv, httpsSrv)
	return nil
}

func shutdown(servers ...*http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(ctx)
	}
}

func (a *App) sessionSweeper(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Sessions.Sweep(ctx)
		}
	}
}
