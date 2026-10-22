package api

import (
	"net/http"

	"github.com/beedywool/vps-admin/internal/proxy"
)

// proxySites lists every domain the machine's reverse proxy serves, with the upstream it
// forwards to. A managed snippet whose app is gone comes back without its id, so the panel
// shows it as left over instead of linking to a page that no longer exists.
func (s *Server) proxySites(w http.ResponseWriter, r *http.Request) {
	rep := proxy.Read(s.Cfg.CaddySitesDir, s.Cfg.NginxSitesDir)
	if s.Store != nil {
		if apps, err := s.Store.Apps(r.Context()); err == nil {
			known := make(map[string]bool, len(apps))
			for _, a := range apps {
				known[a.ID] = true
			}
			for i := range rep.Sites {
				if rep.Sites[i].Managed && !known[rep.Sites[i].ID] {
					rep.Sites[i].ID = ""
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, rep)
}
