package main

import (
 "photoeditor/backend/internal/database"
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

type reflectionProjectConfig struct {
	Tenant         uuid.UUID                              `json:"tenant_id"`
	Project        uuid.UUID                              `json:"project_id"`
	Pin            v2portrait.RestorationQualificationPin `json:"qualification"`
	BaseProfile    json.RawMessage                        `json:"base_profile"`
	ReservedBytes  int64                                  `json:"reserved_bytes"`
	LicenseCleared bool                                   `json:"license_cleared"`
}

type reflectionDeploymentConfig struct {
	SchemaVersion int                       `json:"schema_version"`
	Projects      []reflectionProjectConfig `json:"projects"`
}

type reflectionProjectServices struct {
	projects map[[2]uuid.UUID]*v2draft.ReflectionSubmissionService
}

func (s reflectionProjectServices) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request v2draft.ReflectionPreparationRequest) (v2draft.ReflectionClientIntent, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.ReflectionClientIntent{}, v2input.ErrInvalid
	}
	return service.PrepareProjectKeyed(ctx, who, tenant, project, key, request)
}

func (s reflectionProjectServices) SubmitProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	intent v2draft.ReflectionClientIntent) (v2draft.RestorationAdmissionTicket, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.RestorationAdmissionTicket{}, v2input.ErrInvalid
	}
	return service.SubmitProjectKeyed(ctx, who, tenant, project, key, intent)
}

func (s reflectionProjectServices) ListProjectMaskPairs(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID,
	query v2draft.GlareMaskPairQuery) (v2draft.GlareMaskPairPage, error) {
	service := s.projects[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return v2draft.GlareMaskPairPage{}, v2input.ErrInvalid
	}
	return service.ListMaskPairs(ctx, who, tenant, project, query)
}

func buildReflectionCreate(ctx context.Context, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string) (httpapi.ReflectionService, func(), error) {
	noop := func() {}
	path := os.Getenv("PHOTO_V28_REFLECTION_CONFIG")
	if path == "" {
		return nil, noop, nil
	}
	if catalog == nil || objects == nil || bucket == "" {
		return nil, noop, errors.New("reflection dependencies unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 8<<20 {
		return nil, noop, errors.New("reflection configuration invalid")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, noop, errors.New("reflection configuration unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil || len(raw) < 2 || len(raw) > 8<<20 {
		return nil, noop, errors.New("reflection configuration read failed")
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return nil, noop, errors.New("reflection configuration is not canonical")
	}
	var config reflectionDeploymentConfig
	if json.Unmarshal(raw, &config) != nil || config.SchemaVersion != 1 ||
		len(config.Projects) < 1 || len(config.Projects) > 1024 {
		return nil, noop, errors.New("reflection configuration invalid")
	}
	exact, err := canonical.Marshal(config)
	if err != nil || !bytes.Equal(exact, raw) {
		return nil, noop, errors.New("reflection configuration fields invalid")
	}
	dsn := os.Getenv("V28_REFLECTION_SCHEDULER_DATABASE_URL")
	if dsn == "" {
		return nil, noop, errors.New("reflection scheduler configuration unavailable")
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("reflection scheduler configuration invalid")
	}
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(poolConfig)
	scheduler, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, noop, errors.New("reflection scheduler unavailable")
	}
	closeOnError := func(message string) (httpapi.ReflectionService, func(), error) {
		scheduler.Close()
		return nil, noop, errors.New(message)
	}
	var role string
	var installed bool
	err = scheduler.QueryRow(ctx, `SELECT current_user,
  to_regprocedure('photo.begin_v28_reflection_draft(text,text,text,bigint)') IS NOT NULL
  AND to_regprocedure('photo.v28_reflection_params_request_scope(uuid,uuid,jsonb)') IS NOT NULL
  AND to_regprocedure('photo.read_v28_reflection_admission_ticket(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb)') IS NOT NULL
  AND to_regprocedure('photo.v28_reflection_current_authority(uuid,uuid,uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.apply_v28_reflection_version(uuid,uuid,uuid,uuid,text,text,text)') IS NOT NULL
  AND to_regprocedure('photo.v28_manual_pair_origin(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text)') IS NOT NULL`).Scan(&role, &installed)
	if err != nil || role != "photo_scheduler" || !installed {
		return closeOnError("reflection SQL installation unavailable")
	}
	services := reflectionProjectServices{projects: map[[2]uuid.UUID]*v2draft.ReflectionSubmissionService{}}
	for _, p := range config.Projects {
		if !p.LicenseCleared || p.Tenant == uuid.Nil || p.Project == uuid.Nil {
			return closeOnError("reflection project unavailable")
		}
		key := [2]uuid.UUID{p.Tenant, p.Project}
		if services.projects[key] != nil {
			return closeOnError("duplicate reflection project")
		}
		service, e := v2draft.NewReflectionSubmissionService(scheduler, catalog,
			objects, bucket, p.Tenant, p.Project, p.Pin, p.BaseProfile, p.ReservedBytes)
		if e != nil {
			return closeOnError("reflection project configuration invalid")
		}
		services.projects[key] = service
	}
	return services, scheduler.Close, nil
}
