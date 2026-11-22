package server

import (
	"net/http"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// header returns X-Switchboard-<name>.
func (s *Server) header(r *http.Request, name string) string {
	return r.Header.Get(config.HeaderPrefix + name)
}

func (s *Server) aliasUse() wire.AliasUse {
	return wire.AliasUse{OldEnv: config.OldEnvNames()}
}
