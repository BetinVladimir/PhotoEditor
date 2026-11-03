package httpapi

import (
	"net/http"

	videoapi "photoeditor/backend/internal/video/api"
)

// videoViewer authorizes the portal share session cookie for a video delivery permission.
func (s *Server) videoViewer(r *http.Request, permission string) (videoapi.Viewer, error) {
	token, err := s.portalToken(r)
	if err != nil {
		return videoapi.Viewer{}, err
	}
	who, err := s.Portal.Authorize(r.Context(), token, "", permission, false)
	if err != nil {
		return videoapi.Viewer{}, err
	}
	return videoapi.Viewer{Tenant: who.TenantID, Share: who.ShareID, Collection: who.CollectionID}, nil
}
