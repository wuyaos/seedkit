package server

import "net/http"

// Settings replace request-facing roots and tool services. SSE does not use them
// and must not keep a read lock for its indefinite connection lifetime.
func (s *Server) lockServices(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tasks/events" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/api/settings" {
			s.servicesMu.Lock()
			defer s.servicesMu.Unlock()
		} else {
			s.servicesMu.RLock()
			defer s.servicesMu.RUnlock()
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) lockStatus(w http.ResponseWriter, r *http.Request) {
	s.servicesMu.Lock()
	defer s.servicesMu.Unlock()
	s.handleStatus(w, r)
}
