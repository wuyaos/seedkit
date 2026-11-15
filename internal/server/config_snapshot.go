package server

import (
	"github.com/wuyaos/seedkit/internal/config"
	"maps"
)

// Jobs capture settings once so saving new credentials cannot mutate an in-flight upload.
func (s *Server) configSnapshot() config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	c := *s.cfg
	c.Paths.Roots = maps.Clone(c.Paths.Roots)
	c.Upload.Extra = maps.Clone(c.Upload.Extra)
	return c
}
