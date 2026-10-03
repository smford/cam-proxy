package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/smford/cam-proxy/internal/camera"
	"github.com/smford/cam-proxy/internal/config"
)

// Server wraps the HTTP server and camera manager.
type Server struct {
	httpServer *http.Server
	manager    *camera.Manager
	startTime  time.Time
}

// NewServer configures and builds the HTTP server with logging, security, and CORS middleware.
func NewServer(cfg config.ServerConfig, mgr *camera.Manager) *Server {
	mux := http.NewServeMux()
	startTime := time.Now()

	RegisterRoutes(mux, mgr, startTime)

	// Middleware pipeline: logging -> security headers -> CORS -> mux
	handler := BuildHandler(mux, cfg)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return &Server{
		httpServer: srv,
		manager:    mgr,
		startTime:  startTime,
	}
}

// BuildHandler wraps an http.Handler with standard cam-proxy middlewares:
// Logging -> SecurityHeaders -> CORS.
func BuildHandler(mux http.Handler, cfg config.ServerConfig) http.Handler {
	return loggingMiddleware(SecurityHeadersMiddleware(CORSMiddleware(cfg.CORS)(mux)))
}

// Handler returns the underlying http.Handler with all middlewares applied.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Start launches the HTTP server in a non-blocking goroutine.
func (s *Server) Start() error {
	slog.Info("starting HTTP server", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}
	return nil
}

// Shutdown initiates graceful termination of the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		slog.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", duration,
			"remote", r.RemoteAddr,
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
