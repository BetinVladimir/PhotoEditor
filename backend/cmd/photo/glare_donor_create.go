package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type glareDonorProjectConfig struct {
	Tenant         uuid.UUID                              `json:"tenant_id"`
	Project        uuid.UUID                              `json:"project_id"`
	Pin            v2portrait.RestorationQualificationPin `json:"qualification"`
	BaseProfile    json.RawMessage                        `json:"base_profile"`
	ReservedBytes  int64                                  `json:"reserved_bytes"`
	LicenseCleared bool                                   `json:"license_cleared"`
}

type glareDonorDeploymentConfig struct {
	SchemaVersion int                       `json:"schema_version"`
	Projects      []glareDonorProjectConfig `json:"projects"`
}

type glareDonorProjectServices struct {
	projects map[[2]uuid.UUID]*v2draft.GlareDonorSubmissionService
}

func (s glareDonorProjectServices) SubmitProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	intent v2draft.GlareDonorClientIntent) (v2draft.RestorationAdmissionTicket, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.RestorationAdmissionTicket{}, v2input.ErrInvalid
	}
	return service.SubmitProjectKeyed(ctx, who, tenant, project, key, intent)
}

func (s glareDonorProjectServices) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request v2draft.GlareDonorPreparationRequest) (v2draft.GlareDonorClientIntent, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.GlareDonorClientIntent{}, v2input.ErrInvalid
	}
	return service.PrepareProjectKeyed(ctx, who, tenant, project, key, request)
}

func (s glareDonorProjectServices) ListProjectCandidates(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareDonorCandidateQuery) (v2draft.GlareDonorCandidatePage, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.GlareDonorCandidatePage{}, v2input.ErrInvalid
	}
	return service.ListCandidates(ctx, who, tenant, project, query)
}

func (s glareDonorProjectServices) ListProjectSourceCandidates(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareSourceCandidateQuery) (v2draft.GlareSourceCandidatePage, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.GlareSourceCandidatePage{}, v2input.ErrInvalid
	}
	return service.ListSourceCandidates(ctx, who, tenant, project, query)
}

func (s glareDonorProjectServices) ListProjectMaskPairs(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareMaskPairQuery) (v2draft.GlareMaskPairPage, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.GlareMaskPairPage{}, v2input.ErrInvalid
	}
	return service.ListMaskPairs(ctx, who, tenant, project, query)
}

func buildGlareDonorCreate(ctx context.Context, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string) (httpapi.GlareDonorService, func(), error) {
	noop := func() {}
	path := os.Getenv("PHOTO_V28_GLARE_DONOR_CONFIG")
	if path == "" {
		return nil, noop, nil
	}
	if catalog == nil || objects == nil || bucket == "" {
		return nil, noop, errors.New("glare donor dependencies unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 8<<20 {
		return nil, noop, errors.New("glare donor configuration invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, errors.New("glare donor configuration unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil || len(raw) < 2 || len(raw) > 8<<20 {
		return nil, noop, errors.New("glare donor configuration read failed")
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return nil, noop, errors.New("glare donor configuration is not canonical")
	}
	var config glareDonorDeploymentConfig
	if json.Unmarshal(raw, &config) != nil || config.SchemaVersion != 1 ||
		len(config.Projects) < 1 || len(config.Projects) > 1024 {
		return nil, noop, errors.New("glare donor configuration invalid")
	}
	exact, err := canonical.Marshal(config)
	if err != nil || !bytes.Equal(exact, raw) {
		return nil, noop, errors.New("glare donor configuration fields invalid")
	}
	dsn := os.Getenv("V28_GLARE_DONOR_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, noop, errors.New("glare donor scheduler configuration unavailable")
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("glare donor scheduler configuration invalid")
	}
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, noop, errors.New("glare donor scheduler unavailable")
	}
	closeOnError := func(message string) (httpapi.GlareDonorService, func(), error) {
		scheduler.Close()
		return nil, noop, errors.New(message)
	}
	var role string
	var installed bool
	err = scheduler.QueryRow(ctx, `SELECT current_user,
  to_regprocedure('photo.begin_v28_restoration_draft(text,text,text,bigint)') IS NOT NULL
  AND to_regprocedure('photo.read_v28_restoration_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.v28_glare_donor_face_choice(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.read_v28_glare_donor_preparation_proof(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.list_v28_glare_donor_candidates(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.list_v28_glare_source_candidates(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,uuid)') IS NOT NULL
  AND to_regprocedure('photo.list_v28_glare_mask_pairs(uuid,uuid,uuid,uuid,uuid,jsonb,uuid)') IS NOT NULL`).Scan(&role, &installed)
	if err != nil || role != "photo_scheduler" || !installed {
		return closeOnError("glare donor SQL installation unavailable")
	}
	services := glareDonorProjectServices{projects: map[[2]uuid.UUID]*v2draft.GlareDonorSubmissionService{}}
	for _, p := range config.Projects {
		if !p.LicenseCleared || p.Tenant == uuid.Nil || p.Project == uuid.Nil {
			return closeOnError("glare donor project unavailable")
		}
		key := [2]uuid.UUID{p.Tenant, p.Project}
		if services.projects[key] != nil {
			return closeOnError("duplicate glare donor project")
		}
		service, e := v2draft.NewGlareDonorSubmissionService(scheduler, catalog,
			objects, bucket, p.Tenant, p.Project, p.Pin, p.BaseProfile, p.ReservedBytes)
		if e != nil {
			return closeOnError("glare donor project configuration invalid")
		}
		services.projects[key] = service
	}
	return services, scheduler.Close, nil
}
