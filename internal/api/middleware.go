package api

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/smford/cam-proxy/internal/config"
)

// SecurityHeadersMiddleware sets standard security headers on all responses,
// and ensures appropriate Cache-Control headers for dynamic image endpoints.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Enforce MIME-type sniffing protection
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Apply dynamic image cache-control headers to snapshot and stream endpoints
		cleanPath := strings.TrimSuffix(r.URL.Path, "/")
		if strings.HasSuffix(cleanPath, "/snapshot") || strings.HasSuffix(cleanPath, "/mjpeg") || strings.HasSuffix(cleanPath, "/stream") {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}

		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware handles Cross-Origin Resource Sharing (CORS) according to configuration.
// It allows browser frontends, Home Assistant Lovelace cards, and browser extensions
// to fetch snapshots and issue PTZ commands.
func CORSMiddleware(cfg config.CORSConfig) func(http.Handler) http.Handler {
	allowedMethods := strings.Join(cfg.AllowedMethods, ", ")
	if allowedMethods == "" {
		allowedMethods = "GET, POST, OPTIONS"
	}
	allowedHeaders := strings.Join(cfg.AllowedHeaders, ", ")
	if allowedHeaders == "" {
		allowedHeaders = "Content-Type, Authorization, X-Requested-With"
	}
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strconv.Itoa(cfg.MaxAge)
	}
	allowAllOrigins := slices.Contains(cfg.AllowedOrigins, "*")

	return func(next http.Handler) http.Handler {
		if !cfg.Enabled {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			originAllowed := false

			if allowAllOrigins {
				if cfg.AllowCredentials && origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				}
				originAllowed = true
			} else if origin != "" && slices.Contains(cfg.AllowedOrigins, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				originAllowed = true
			}

			if originAllowed {
				if cfg.AllowCredentials {
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				if allowedMethods != "" {
					w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
				}
				if allowedHeaders != "" {
					w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
				}
				if maxAge != "" {
					w.Header().Set("Access-Control-Max-Age", maxAge)
				}
			}

			// Handle preflight OPTIONS request
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
