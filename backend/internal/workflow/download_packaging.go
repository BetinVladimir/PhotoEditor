package workflow

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

type DownloadBuildPhaseSource struct{ DB artifactjson.RowQuerier }

func (s DownloadBuildPhaseSource) Load(ctx context.Context, tenant, run uuid.UUID) (string, error) {
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return "", errors.New("invalid download build phase source")
	}
	var phase string
	err := s.DB.QueryRow(ctx, "SELECT photo.download_build_phase($1,$2)", tenant, run).Scan(&phase)
	if err != nil {
		return "", err
	}
	switch phase {
	case "planning", "encoding", "packaging", "archiving":
		return phase, nil
	}
	return "", errors.New("invalid download build phase")
}

type DownloadEncodeOutcome struct {
	DownloadSource
	State string
}
type DownloadEncodeOutcomeSource struct{ DB ActivationRows }

func (s DownloadEncodeOutcomeSource) Load(ctx context.Context, tenant, run uuid.UUID) ([]DownloadSource, bool, error) {
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return nil, false, errors.New("invalid download outcome source")
	}
	rows, err := s.DB.Query(ctx, "SELECT ordinal,asset_version_id,state,artifact_id,sha256,bytes,filename FROM photo.download_encode_outcomes($1,$2)", tenant, run)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := []DownloadSource{}
	ready := true
	for rows.Next() {
		var o DownloadEncodeOutcome
		var artifact *uuid.UUID
		var sha, filename *string
		var size *int64
		if err = rows.Scan(&o.Ordinal, &o.AssetVersionID, &o.State, &artifact, &sha, &size, &filename); err != nil {
			return nil, false, err
		}
		switch o.State {
		case "blocked", "ready", "queued", "running":
			ready = false
		case "succeeded":
			if artifact == nil || sha == nil || size == nil || filename == nil {
				return nil, false, errors.New("incomplete encoded output")
			}
			o.ArtifactID = *artifact
			o.SHA256 = *sha
			o.Bytes = *size
			o.Filename = *filename
		case "failed", "cancelled", "skipped":
			return nil, false, errors.New("download export failed")
		default:
			return nil, false, errors.New("invalid download export state")
		}
		items = append(items, o.DownloadSource)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		return nil, false, errors.New("missing download export outcomes")
	}
	if !ready {
		return nil, false, nil
	}
	for i := range items {
		if items[i].Ordinal != int64(i) {
			return nil, false, errors.New("invalid download export order")
		}
	}
	return items, true, nil
}

type DownloadPackagingRepository struct{ DB artifactjson.RowQuerier }

func (r DownloadPackagingRepository) Begin(ctx context.Context, tenant, run uuid.UUID, plan []PlannedDownload) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil || len(plan) == 0 {
		return false, errors.New("invalid download packaging plan")
	}
	raw, err := canonical.Marshal(plan)
	if err != nil {
		return false, err
	}
	var saved bool
	err = r.DB.QueryRow(ctx, "SELECT photo.begin_download_packaging($1,$2,$3)", tenant, run, raw).Scan(&saved)
	return saved, err
}

type DownloadPackagingCoordinator struct {
	Outcomes  DownloadEncodeOutcomeSource
	Plans     DownloadPackagingRepository
	ByteLimit int64
}

func (c DownloadPackagingCoordinator) Advance(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	items, ready, err := c.Outcomes.Load(ctx, tenant, run)
	if err != nil {
		return false, err
	}
	if !ready {
		return false, nil
	}
	limit := c.ByteLimit
	if limit == 0 {
		limit = DefaultVolumeByteLimit
	}
	plan, err := PlanDownloadVolumes(items, limit)
	if err != nil {
		return false, err
	}
	saved, err := c.Plans.Begin(ctx, tenant, run, plan)
	if err != nil {
		return false, err
	}
	if !saved {
		return false, errors.New("download packaging barrier conflict")
	}
	return true, nil
}

type DownloadBuildRunnerV2 struct {
	Phases    DownloadBuildPhaseSource
	Exports   DownloadExportCoordinator
	Packaging DownloadPackagingCoordinator
	Archives  DownloadArchiveCoordinator
	Finalize  DownloadArchiveFinalizer
}

func (r DownloadBuildRunnerV2) Build(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	phase, err := r.Phases.Load(ctx, tenant, run)
	if err != nil {
		return false, err
	}
	switch phase {
	case "planning":
		return r.Exports.Build(ctx, tenant, run)
	case "encoding":
		return r.Packaging.Advance(ctx, tenant, run)
	case "packaging":
		return r.Archives.Build(ctx, tenant, run)
	case "archiving":
		return r.Finalize.Finish(ctx, tenant, run)
	}
	return false, errors.New("unsupported download phase")
}

type DownloadArchiveFinalizer struct{ DB artifactjson.RowQuerier }

func (f DownloadArchiveFinalizer) Finish(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if f.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, errors.New("invalid archive finalizer")
	}
	var changed bool
	err := f.DB.QueryRow(ctx, "SELECT photo.finalize_download_archives($1,$2)", tenant, run).Scan(&changed)
	return changed, err
}
