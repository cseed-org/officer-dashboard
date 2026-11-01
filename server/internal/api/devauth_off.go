//go:build !dev

package api

import "net/http"

// DevBuild reports whether this binary was built with `-tags dev`. The release build has
// no /auth/dev route at all, whatever DEV_AUTH says.
const DevBuild = false

func (s *Server) registerDev(_ *http.ServeMux, _ func(string, handlerFunc)) {}
