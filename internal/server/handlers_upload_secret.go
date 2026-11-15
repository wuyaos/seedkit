package server

import (
	"errors"
	"net/http"
	"strings"
)

// handleUploadSecret reveals one configured upload credential after explicit
// authenticated user action. The settings snapshot never uses this endpoint.
func (s *Server) handleUploadSecret(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var in struct {
		Host string `json:"host"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cfg := s.configSnapshot()
	var secret string
	switch strings.ToLower(strings.TrimSpace(in.Host)) {
	case "picgo":
		secret = cfg.Upload.Picgo.APIKey
	case "imgbb":
		secret = cfg.Upload.Imgbb.APIKey
	case "freeimage":
		secret = cfg.Upload.Freeimage.APIKey
	case "agsv":
		secret = cfg.Upload.AGSV.Token
	case "pterclub":
		secret = cfg.Upload.Pterclub.Cookie
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown upload host"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": secret})
}
