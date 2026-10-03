package api

import "net/http"

// ContentTypeMiddleware is kept as a router-level hook; CORS now lives in
// the credential-aware wrapper that cmd/server's buildHTTPHandler adds.
func ContentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
