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

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type localPortraitPrepareProjectConfig struct {
	Tenant           uuid.UUID `json:"tenant_id"`
	Project          uuid.UUID `json:"project_id"`
	Enabled          bool      `json:"enabled"`
	RuntimeSHA       string    `json:"runtime_sha"`
	PolicySHA        string    `json:"policy_sha"`
	MaxReservedBytes int64     `json:"max_reserved_bytes"`
}
type localPortraitPrepareDeploymentConfig struct {
	SchemaVersion int                                 `json:"schema_version"`
	Projects      []localPortraitPrepareProjectConfig `json:"projects"`
}
type localPortraitProjectPreparers struct {
	projects map[[2]uuid.UUID]v2draft.LocalPortraitSubmissionService
}

func (s localPortraitProjectPreparers) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request v2draft.LocalPortraitClientSelection) (v2draft.LocalPortraitClientSelection, error) {
	service, ok := s.projects[[2]uuid.UUID{tenant, project}]
	if !ok {
		return v2draft.LocalPortraitClientSelection{}, v2input.ErrInvalid
	}
	return service.PrepareProjectKeyed(ctx, who, tenant, project, key, request)
}

func (s localPortraitProjectPreparers) SubmitProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request v2draft.LocalPortraitClientSelection) (v2draft.LocalPortraitTicket, error) {
	service, ok := s.projects[[2]uuid.UUID{tenant, project}]
	if !ok {
		return v2draft.LocalPortraitTicket{}, v2input.ErrInvalid
	}
	return service.SubmitProjectKeyed(ctx, who, tenant, project, key, request)
}

func (s localPortraitProjectPreparers) ListProjectMaskPairs(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareMaskPairQuery) (v2draft.GlareMaskPairPage, error) {
	service, ok := s.projects[[2]uuid.UUID{tenant, project}]
	if !ok {
		return v2draft.GlareMaskPairPage{}, v2input.ErrInvalid
	}
	return service.ListMaskPairs(ctx, who, tenant, project, query)
}

func buildLocalPortraitPrepare(ctx context.Context, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string) (httpapi.LocalPortraitService, func(), error) {
	noop := func() {}
	path := os.Getenv("PHOTO_V28_LOCAL_PORTRAIT_CONFIG")
	if path == "" {
		return nil, noop, nil
	}
	if catalog == nil || objects == nil || bucket == "" {
		return nil, noop, errors.New("local portrait dependencies unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 8<<20 {
		return nil, noop, errors.New("local portrait configuration invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, errors.New("local portrait configuration unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil || len(raw) < 2 || len(raw) > 8<<20 {
		return nil, noop, errors.New("local portrait configuration read failed")
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return nil, noop, errors.New("local portrait configuration is not canonical")
	}
	var config localPortraitPrepareDeploymentConfig
	if json.Unmarshal(raw, &config) != nil || config.SchemaVersion != 1 || len(config.Projects) < 1 || len(config.Projects) > 1024 {
		return nil, noop, errors.New("local portrait configuration invalid")
	}
	exact, err := canonical.Marshal(config)
	if err != nil || !bytes.Equal(exact, raw) {
		return nil, noop, errors.New("local portrait configuration fields invalid")
	}
	dsn := os.Getenv("V28_LOCAL_PORTRAIT_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, noop, errors.New("local portrait scheduler configuration unavailable")
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("local portrait scheduler configuration invalid")
	}
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, noop, errors.New("local portrait scheduler unavailable")
	}
	closeOnError := func(message string) (httpapi.LocalPortraitService, func(), error) {
		scheduler.Close()
		return nil, noop, errors.New(message)
	}
	var role string
	var installed bool
	err = scheduler.QueryRow(ctx, `SELECT current_user,
  to_regprocedure('photo.v28_manual_pair_origin(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text)') IS NOT NULL
  AND to_regprocedure('photo.v28_local_portrait_source_scope(uuid,uuid,uuid,uuid,uuid,jsonb)') IS NOT NULL
  AND to_regprocedure('photo.v28_local_owner_request_scope(jsonb)') IS NOT NULL
  AND to_regprocedure('photo.begin_v28_local_portrait_draft(text,text,bigint)') IS NOT NULL
  AND to_regprocedure('photo.read_v28_local_recovery_owner(uuid,uuid,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.v28_local_runtime_qualification_ready(uuid,uuid,text,text)') IS NOT NULL
  AND to_regprocedure('photo.v32_local_portrait_queue(jsonb)') IS NOT NULL`).Scan(&role, &installed)
	if err != nil || role != "photo_scheduler" || !installed {
		return closeOnError("local portrait SQL installation unavailable")
	}
	publisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: bucket}, Objects: objects}
	services := localPortraitProjectPreparers{projects: map[[2]uuid.UUID]v2draft.LocalPortraitSubmissionService{}}
	for _, p := range config.Projects {
		if !p.Enabled || p.Tenant == uuid.Nil || p.Project == uuid.Nil {
			return closeOnError("local portrait project unavailable")
		}
		key := [2]uuid.UUID{p.Tenant, p.Project}
		if _, exists := services.projects[key]; exists {
			return closeOnError("duplicate local portrait project")
		}
		service := v2draft.LocalPortraitSubmissionService{Scheduler: scheduler, Catalog: catalog,
			Objects: objects, Documents: publisher, Bucket: bucket, Tenant: p.Tenant,
			Project: p.Project, RuntimeSHA: p.RuntimeSHA, PolicySHA: p.PolicySHA,
			MaxReservedBytes: p.MaxReservedBytes}
		if !service.Valid() {
			return closeOnError("local portrait project configuration invalid")
		}
		services.projects[key] = service
	}
	return services, scheduler.Close, nil
}
