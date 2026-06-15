package server

import (
	"net/http"
	"time"
)

// withLogging is a minimal access logger. It avoids the overhead of reading
// request bodies or computing stats on the hot path; detailed per-request
// recording happens in the proxy handlers via the store.
func (s *Server) withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		h.ServeHTTP(rw, r)
		_ = start
		// Intentionally silent; the panel surfaces real data from the store.
		_ = rw.status
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying writer so streaming (http.Flusher) works
// through this wrapper. Without it, w.(http.Flusher) assertions fail and kill
// every streamed response.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController and the standard library detect the
// underlying writer for interfaces like Flusher / Hijacker.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
