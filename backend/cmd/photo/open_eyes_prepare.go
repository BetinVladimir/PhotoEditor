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
	"photoeditor/backend/internal/v2qualification"
)

type openEyesProjectConfig struct {
	Tenant         uuid.UUID                        `json:"tenant_id"`
	Project        uuid.UUID                        `json:"project_id"`
	Pin            v2portrait.DonorQualificationPin `json:"qualification"`
	Locks          v2qualification.DeploymentLocks  `json:"deployment_locks"`
	PatchWidth     int64                            `json:"patch_width"`
	PatchHeight    int64                            `json:"patch_height"`
	Capacity       v2portrait.DonorResourceEstimate `json:"capacity"`
	ReservedBytes  int64                            `json:"reserved_bytes"`
	LicenseCleared bool                             `json:"license_cleared"`
}

type openEyesDeploymentConfig struct {
	SchemaVersion int                     `json:"schema_version"`
	Projects      []openEyesProjectConfig `json:"projects"`
}

type openEyesProjectPreparers struct {
	projects map[[2]uuid.UUID]*v2draft.OpenEyesSubmissionService
}

func (s openEyesProjectPreparers) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	selection v2draft.OpenEyesClientSelection) (v2draft.OpenEyesClientPreparation, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.OpenEyesClientPreparation{}, v2input.ErrInvalid
	}
	return service.PrepareProjectKeyed(ctx, who, tenant, project, key, selection)
}

func (s openEyesProjectPreparers) SubmitProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	intent v2draft.OpenEyesClientIntent) (v2draft.DonorQueuedTicket, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.DonorQueuedTicket{}, v2input.ErrInvalid
	}
	return service.SubmitProjectKeyed(ctx, who, tenant, project, key, intent)
}

func (s openEyesProjectPreparers) ListProjectSourceCandidates(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareSourceCandidateQuery) (v2draft.GlareSourceCandidatePage, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.GlareSourceCandidatePage{}, v2input.ErrInvalid
	}
	return service.ListProjectSourceCandidates(ctx, who, tenant, project, query)
}

func (s openEyesProjectPreparers) ListProjectCandidates(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareDonorCandidateQuery) (v2draft.GlareDonorCandidatePage, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.GlareDonorCandidatePage{}, v2input.ErrInvalid
	}
	return service.ListProjectCandidates(ctx, who, tenant, project, query)
}

func (s openEyesProjectPreparers) ListProjectMaskPairs(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareMaskPairQuery) (v2draft.GlareMaskPairPage, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.GlareMaskPairPage{}, v2input.ErrInvalid
	}
	return service.ListProjectMaskPairs(ctx, who, tenant, project, query)
}

func buildOpenEyesPrepare(ctx context.Context, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string) (httpapi.OpenEyesService, func(), error) {
	noop := func() {}
	path := os.Getenv("PHOTO_V28_OPEN_EYES_CONFIG")
	if path == "" {
		return nil, noop, nil
	}
	if catalog == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, noop, errors.New("open eyes dependencies unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 8<<20 {
		return nil, noop, errors.New("open eyes configuration invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, errors.New("open eyes configuration unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil || len(raw) < 2 || len(raw) > 8<<20 {
		return nil, noop, errors.New("open eyes configuration read failed")
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return nil, noop, errors.New("open eyes configuration is not canonical")
	}
	var config openEyesDeploymentConfig
	if json.Unmarshal(raw, &config) != nil || config.SchemaVersion != 1 ||
		len(config.Projects) < 1 || len(config.Projects) > 1024 {
		return nil, noop, errors.New("open eyes configuration invalid")
	}
	exact, err := canonical.Marshal(config)
	if err != nil || !bytes.Equal(exact, raw) {
		return nil, noop, errors.New("open eyes configuration fields invalid")
	}
	dsn := os.Getenv("V28_OPEN_EYES_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, noop, errors.New("open eyes scheduler configuration unavailable")
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("open eyes scheduler configuration invalid")
	}
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, noop, errors.New("open eyes scheduler unavailable")
	}
	closeOnError := func(message string) (httpapi.OpenEyesService, func(), error) {
		scheduler.Close()
		return nil, noop, errors.New(message)
	}
	var role string
	var installed bool
	err = scheduler.QueryRow(ctx, `SELECT current_user,
  to_regprocedure('photo.read_v28_open_eyes_preparation_proof(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.v28_manual_pair_origin(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text)') IS NOT NULL
  AND to_regprocedure('photo.lock_v28_donor_qualification(uuid,uuid,uuid,uuid,jsonb)') IS NOT NULL
  AND to_regprocedure('photo.list_v28_glare_source_candidates(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,uuid)') IS NOT NULL
  AND to_regprocedure('photo.list_v28_glare_donor_candidates(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.list_v28_glare_mask_pairs(uuid,uuid,uuid,uuid,uuid,jsonb,uuid)') IS NOT NULL
  AND to_regprocedure('photo.begin_v28_open_eyes_draft(text,text,text,bigint)') IS NOT NULL
  AND to_regprocedure('photo.v28_open_eyes_queued_request_matches(uuid,uuid,uuid,uuid,text)') IS NOT NULL
  AND to_regprocedure('photo.reserve_v28_donor_document(uuid,uuid,uuid,jsonb,text,bigint,text)') IS NOT NULL
  AND to_regprocedure('photo.complete_v28_donor_document(uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,text)') IS NOT NULL
  AND to_regprocedure('photo.save_v28_donor_params_intent(uuid,uuid,jsonb)') IS NOT NULL
  AND to_regprocedure('photo.read_v28_donor_params_intent(uuid,uuid,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.stage_v28_donor_task(uuid,uuid,uuid,uuid,text,text)') IS NOT NULL
  AND to_regprocedure('photo.seal_v28_donor_graph(uuid,uuid,uuid,uuid,jsonb)') IS NOT NULL
  AND to_regprocedure('photo.read_v28_donor_queued_receipt(uuid,uuid,uuid,uuid)') IS NOT NULL`).Scan(&role, &installed)
	if err != nil || role != "photo_scheduler" || !installed {
		return closeOnError("open eyes SQL installation unavailable")
	}
	services := openEyesProjectPreparers{projects: map[[2]uuid.UUID]*v2draft.OpenEyesSubmissionService{}}
	for _, p := range config.Projects {
		if !p.LicenseCleared || p.Tenant == uuid.Nil || p.Project == uuid.Nil {
			return closeOnError("open eyes project unavailable")
		}
		key := [2]uuid.UUID{p.Tenant, p.Project}
		if services.projects[key] != nil {
			return closeOnError("duplicate open eyes project")
		}
		service, e := v2draft.NewOpenEyesSubmissionService(scheduler, catalog,
			objects, bucket, p.Tenant, p.Project, p.Pin, p.Locks,
			p.PatchWidth, p.PatchHeight, p.Capacity, p.ReservedBytes)
		if e != nil {
			return closeOnError("open eyes project configuration invalid")
		}
		services.projects[key] = service
	}
	return services, scheduler.Close, nil
}
