package semanticproposal_test

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/semanticproposal"
)

func init() {
	semanticproposal.CreateHTTPHandler = func(db *pgxpool.Pool, a *auth.Service, s semanticproposal.CreateHTTPProposer) http.Handler {
		return (&httpapi.Server{DB: db, Auth: a, SemanticPropose: s, Config: config.Config{Environment: "development", AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 65536}}).Routes()
	}
}
