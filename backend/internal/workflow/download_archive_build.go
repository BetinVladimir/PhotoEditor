package workflow

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type DownloadArchiveContextSource struct{ DB ActivationRows }
type downloadArchiveRow struct {
	snapshot       uuid.UUID
	snapshotDigest string
	volume         int
	kind           string
	ordinal        int64
	artifact       uuid.UUID
	sha            string
	bytes          int64
	name           string
	producer       uuid.UUID
}

func (s DownloadArchiveContextSource) Load(ctx context.Context, tenant, run uuid.UUID) (DownloadArchiveSpec, error) {
	if err := ctx.Err(); err != nil { return DownloadArchiveSpec{}, err }
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return DownloadArchiveSpec{}, errors.New("invalid archive context source")
	}
	rows, err := s.DB.Query(ctx, "SELECT snapshot_id,snapshot_digest,volume_no,delivery_kind,ordinal,artifact_id,sha256,bytes,entry_name,producer_task_id FROM photo.download_archive_context($1,$2)", tenant, run)
	if err != nil {
		return DownloadArchiveSpec{}, err
	}
	defer rows.Close()
	spec := DownloadArchiveSpec{RunID: run}
	byVolume := map[int][]DownloadArchiveEntry{}
	seen := map[int64]bool{}
	for rows.Next() {
		if err := ctx.Err(); err != nil { return DownloadArchiveSpec{}, err }
		var r downloadArchiveRow
		if err = rows.Scan(&r.snapshot, &r.snapshotDigest, &r.volume, &r.kind, &r.ordinal, &r.artifact, &r.sha, &r.bytes, &r.name, &r.producer); err != nil {
			return DownloadArchiveSpec{}, err
		}
        if err := ctx.Err(); err != nil { return DownloadArchiveSpec{}, err }
		if spec.SnapshotID == uuid.Nil {
			spec.SnapshotID, spec.SnapshotDigest = r.snapshot, r.snapshotDigest
		} else if spec.SnapshotID != r.snapshot || spec.SnapshotDigest != r.snapshotDigest {
			return DownloadArchiveSpec{}, errors.New("archive snapshot changed")
		}
		if len(seen) >= MaxDownloadItems || seen[r.ordinal] || r.ordinal < 0 || r.ordinal >= MaxDownloadItems || r.volume < 1 || r.volume > MaxDownloadItems ||
            r.snapshot == uuid.Nil || domainDigestInvalid(r.snapshotDigest) || !validExpansionRef(review.ArtifactRef{ID: r.artifact, SHA256: r.sha}) ||
            r.bytes < 1 || !validArchiveEntryName(r.name) || r.producer == uuid.Nil || (r.kind != "zip" && r.kind != "single") {
			return DownloadArchiveSpec{}, errors.New("invalid archive context row")
		}
		seen[r.ordinal] = true
		if r.kind == "zip" {
			byVolume[r.volume] = append(byVolume[r.volume], DownloadArchiveEntry{Ordinal: r.ordinal, Artifact: review.ArtifactRef{ID: r.artifact, SHA256: r.sha}, Bytes: r.bytes, EntryName: r.name, ProducerTask: r.producer})
		}
	}
	if err = rows.Err(); err != nil {
		return DownloadArchiveSpec{}, err
	}
	if err := ctx.Err(); err != nil { return DownloadArchiveSpec{}, err }
	if spec.SnapshotID == uuid.Nil {
		return DownloadArchiveSpec{}, errors.New("empty archive context")
	}
	volumes := make([]int, 0, len(byVolume))
	for number := range byVolume {
		volumes = append(volumes, number)
	}
	sort.Ints(volumes)
	for _, number := range volumes {
        sort.Slice(byVolume[number], func(i, j int) bool { return byVolume[number][i].Ordinal < byVolume[number][j].Ordinal })
		spec.Volumes = append(spec.Volumes, DownloadArchiveVolume{Number: number, Entries: byVolume[number]})
	}
	registry, err := LoadRegistry()
	if err != nil {
		return DownloadArchiveSpec{}, err
	}
	if _, err = CompileDownloadArchives(spec, registry); err != nil {
		return DownloadArchiveSpec{}, err
	}
	return spec, nil
}

type DownloadArchiveTaskRepository struct{ DB artifactjson.RowQuerier }

func (r DownloadArchiveTaskRepository) Materialize(ctx context.Context, tenant, run uuid.UUID, task PreparedTask) (bool, error) {
	if err := ctx.Err(); err != nil { return false, err }
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil || task.Operation != "archive.build" || !validExpansionRef(task.Params) || len(task.Inputs) < 1 {
		return false, errors.New("invalid archive task materialization")
	}
	digest, e := repairInputDigest(task.Operation, task.Version, task.Params, task.Inputs)
	if e != nil || digest != task.InputDigest {
		return false, errors.New("invalid archive task digest")
	}
	if _, e = ParseResourceEstimate(task.ExecutionProfile); e != nil {
		return false, e
	}
	raw, err := canonical.Marshal(task)
	if err != nil {
		return false, err
	}
	var ok bool
	if err := ctx.Err(); err != nil { return false, err }
	err = r.DB.QueryRow(ctx, "SELECT photo.materialize_download_archive_task($1,$2,$3)", tenant, run, raw).Scan(&ok)
	if err != nil { return false, err }
	if err := ctx.Err(); err != nil { return false, err }
	return ok, nil
}
func (r DownloadArchiveTaskRepository) Seal(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	if err := ctx.Err(); err != nil { return false, err }
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return false, errors.New("invalid archive seal")
	}
	var ok bool
	err := r.DB.QueryRow(ctx, "SELECT photo.seal_download_archives($1,$2)", tenant, run).Scan(&ok)
	if err != nil { return false, err }
	if err := ctx.Err(); err != nil { return false, err }
	return ok, nil
}

type DownloadArchiveCoordinator struct {
	Source  DownloadArchiveContextSource
	Builder DownloadArchiveBuilder
	Tasks   DownloadArchiveTaskRepository
}

func (c DownloadArchiveCoordinator) Build(ctx context.Context, tenant, run uuid.UUID) (bool, error) {
	spec, err := c.Source.Load(ctx, tenant, run)
	if err != nil {
		return false, err
	}
	published, err := c.Builder.Publish(ctx, tenant, spec)
	if err != nil {
		return false, err
	}
	for _, entry := range published {
		task, e := PrepareDownloadArchiveTask(entry)
		if e != nil {
			return false, e
		}
		ok, e := c.Tasks.Materialize(ctx, tenant, run, task)
		if e != nil {
			return false, e
		}
		if !ok {
			return false, errors.New("archive task materialization conflict")
		}
	}
	ok, err := c.Tasks.Seal(ctx, tenant, run)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, errors.New("archive seal incomplete")
	}
	return true, nil
}
