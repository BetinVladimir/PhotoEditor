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

var errDownloadEncodeFailed = errors.New("download export failed")

type DownloadEncodeOutcome struct {
	DownloadSource
	State string
}
type DownloadEncodeOutcomeSource struct{ DB ActivationRows }

func (s DownloadEncodeOutcomeSource) Load(ctx context.Context, tenant, run uuid.UUID) ([]DownloadSource, bool, error) {
	if err:=ctx.Err();err!=nil{return nil,false,err}
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
 failed := false
	for rows.Next() {
		if err:=ctx.Err();err!=nil{return nil,false,err}
		var o DownloadEncodeOutcome
		var artifact *uuid.UUID
		var sha, filename *string
		var size *int64
		if err = rows.Scan(&o.Ordinal, &o.AssetVersionID, &o.State, &artifact, &sha, &size, &filename); err != nil {
			return nil, false, err
		}
		// Validate the frozen inventory even while other exports are still running.
		// A missing or duplicated ordinal must not masquerade as a pending barrier.
		if o.Ordinal != int64(len(items)) || o.AssetVersionID == uuid.Nil {
			return nil, false, errors.New("invalid download export order")
		}
		switch o.State {
		case "blocked", "ready", "queued", "running":
			ready = false
		case "succeeded":
			if artifact == nil || sha == nil || size == nil || filename == nil {
                failed = true
                break
			}
			o.ArtifactID = *artifact
			o.SHA256 = *sha
			o.Bytes = *size
			o.Filename = *filename
		case "failed", "cancelled", "skipped":
			failed = true
		default:
			return nil, false, errors.New("invalid download export state")
		}
		items = append(items, o.DownloadSource)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	if err:=ctx.Err();err!=nil{return nil,false,err}
	if len(items) == 0 {
		return nil, false, errors.New("missing download export outcomes")
	}
	if !ready {
		return nil, false, nil
	}
 if failed { return nil, false, errDownloadEncodeFailed }
	return items, true, nil
}

type DownloadPackagingRepository struct{ DB artifactjson.RowQuerier }

func (r DownloadPackagingRepository) Begin(ctx context.Context, tenant, run uuid.UUID, plan []PlannedDownload) (bool, error) {
	if err:=ctx.Err();err!=nil{return false,err}
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

// FailEncoded is independently gated in SQL; transient IO and malformed output
// errors never convert an otherwise recoverable download into a terminal failure.
func (r DownloadPackagingRepository) FailEncoded(ctx context.Context, tenant, run uuid.UUID) (bool,error) {
 if err:=ctx.Err();err!=nil{return false,err}
 if r.DB==nil || tenant==uuid.Nil || run==uuid.Nil{return false,errors.New("invalid download failure repository")}
 var changed bool
 err:=r.DB.QueryRow(ctx,"SELECT photo.fail_download_encoding($1,$2)",tenant,run).Scan(&changed)
 if err!=nil{return false,err}
 if err:=ctx.Err();err!=nil{return false,err}
 return changed,nil
}

type DownloadPackagingCoordinator struct {
	Outcomes  DownloadEncodeOutcomeSource
	Plans     DownloadPackagingRepository
	ByteLimit int64
 FileLimit int
}

func (c DownloadPackagingCoordinator) Advance(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	items, ready, err := c.Outcomes.Load(ctx, tenant, run)
 if errors.Is(err,errDownloadEncodeFailed) {
  changed,saveErr:=c.Plans.FailEncoded(ctx,tenant,run)
  if saveErr!=nil{return false,saveErr}
  if !changed{return false,errors.New("download failure barrier conflict")}
  return true,nil
 }
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
	fileLimit:=c.FileLimit
 if fileLimit==0{fileLimit=MaxDownloadItems}
 plan, err := PlanDownloadVolumesWithLimits(items, limit,fileLimit)
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
	if err:=ctx.Err();err!=nil{return false,err}
	if f.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, errors.New("invalid archive finalizer")
	}
	var changed bool
	err := f.DB.QueryRow(ctx, "SELECT photo.finalize_download_archives($1,$2)", tenant, run).Scan(&changed)
	if err!=nil{return false,err}
	if err:=ctx.Err();err!=nil{return false,err}
	return changed,nil
}
