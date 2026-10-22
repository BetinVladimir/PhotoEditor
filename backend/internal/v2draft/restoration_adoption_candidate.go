package v2draft

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

// SQLRestorationCandidateAdopter supplies separate API/editor and scheduler
// pools. Catalog reads replay live editor scope; result reads and receipt use
// the scheduler role. Neither pool is granted the other's authority.
type SQLRestorationCandidateAdopter struct {
	CatalogDB   *pgxpool.Pool
	SchedulerDB *pgxpool.Pool
	Objects     v2input.SessionAnalysisObjectReader
	Bucket      string
}

func (a SQLRestorationCandidateAdopter) AdoptCandidate(ctx context.Context,
	job RestorationAdoptionCandidate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.CatalogDB == nil || a.SchedulerDB == nil || a.Objects == nil ||
		len(a.Bucket) < 3 || len(a.Bucket) > 63 ||
		job.Scope.Tenant == uuid.Nil || job.Scope.Root == uuid.Nil ||
		job.Project == uuid.Nil || job.Actor.UserID == uuid.Nil ||
		job.Actor.SessionID == uuid.Nil || len(job.Params) < 2 || len(job.Params) > 65536 {
		return v2input.ErrInvalid
	}
	var params struct {
		Base struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"base"`
		Donor *struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"donor"`
	}
	if json.Unmarshal(job.Params, &params) != nil || params.Base.AssetVersionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	base := SQLArtifactCatalog{DB: a.CatalogDB, Bucket: a.Bucket,
		ProjectID: job.Project, AssetVersionID: params.Base.AssetVersionID}
	var donor ArtifactCatalog
	if params.Donor != nil {
		if params.Donor.AssetVersionID == uuid.Nil {
			return v2input.ErrInvalid
		}
		donor = SQLArtifactCatalog{DB: a.CatalogDB, Bucket: a.Bucket,
			ProjectID: job.Project, AssetVersionID: params.Donor.AssetVersionID}
	}
	material := RestorationCandidateMaterialLoader{Candidate: job,
		DB: a.SchedulerDB, BaseCatalog: base, DonorCatalog: donor, Objects: a.Objects}
	verifier := RestorationAdoptionVerifier{Material: material,
		Documents: SQLRestorationResultDocumentCatalog{DB: a.SchedulerDB, Bucket: a.Bucket},
		Arrays:    SQLRestorationResultArrayCatalog{DB: a.SchedulerDB, Bucket: a.Bucket},
		Objects:   a.Objects, Bucket: a.Bucket}
	var currentVerifier RestorationEvidenceVerifier = verifier
	if job.Operation == "portrait.glare" {
		currentVerifier = currentGlareAdoptionVerifier{Inner: verifier,
			Authority: sqlGlareAdoptionAuthority{DB: a.SchedulerDB}}
	}
	service := RestorationAdoptionService{Verifier: currentVerifier,
		Store: SQLRestorationAdoptionStore{DB: a.SchedulerDB}}
	_, _, err := service.Adopt(ctx, job.Actor, job.Scope, job.Operation, job.Params)
	return err
}
