package middleware

import (
	"log"
	"net/http"

	"infra-diagram/internal/api/render"
)

// Recovery catches panics in handlers and returns 500 instead of crashing.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC in %s %s: %v", r.Method, r.URL.Path, rec)
				render.Error(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
