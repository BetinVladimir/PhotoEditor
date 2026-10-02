package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type DownloadExportBatch struct {
	JobID, SnapshotID uuid.UUID
	SnapshotDigest    string
	Spec              DownloadExportSpec
	Sizes             map[uuid.UUID]int64
}

type DownloadExportLoader struct {
	DB                    ActivationRows
	Objects               ArtifactOpener
	Bucket, WorkICCSHA256 string
	TargetICCs            map[string]string
}

type downloadPresetEnvelope struct {
	ID       uuid.UUID      `json:"id"`
	Revision int64          `json:"revision"`
	Hash     string         `json:"hash"`
	Config   DownloadPreset `json:"config"`
}

type pendingDownloadMaster struct {
	ordinal int64
	version uuid.UUID
	object  inspectedObject
}

// Load closes both SQL result sets before object IO. The first query binds the
// immutable selection/master; the second attests nested refs to the exact
// canonical asset.decode result that produced each master.
func (l DownloadExportLoader) Load(ctx context.Context, tenant, run uuid.UUID) (DownloadExportBatch, error) {
	invalid := errors.New("invalid download export context")
	fail := func() (DownloadExportBatch, error) { return DownloadExportBatch{}, invalid }
	if l.DB == nil || l.Objects == nil || tenant == uuid.Nil || run == uuid.Nil || l.Bucket == "" {
		return fail()
	}
	if _, err := domain.ParseDigest(l.WorkICCSHA256); err != nil || len(l.TargetICCs) == 0 {
		return fail()
	}
	rows, err := l.DB.Query(ctx, "SELECT job_id,snapshot_id,snapshot_digest,item_count,preset,ordinal,asset_version_id,master_id,master_sha,master_bucket,master_key,master_version,master_bytes,master_media_type FROM photo.download_export_context($1,$2)", tenant, run)
	if err != nil {
		return DownloadExportBatch{}, err
	}
	var out DownloadExportBatch
	out.Spec.RunID, out.Sizes = run, map[uuid.UUID]int64{}
	masters := []pendingDownloadMaster{}
	var expected int64
	for rows.Next() {
		var job, snapshot uuid.UUID
		var digest string
		var count int64
		var presetRaw []byte
		var ordinal int64
		var version uuid.UUID
		var object inspectedObject
		if err = rows.Scan(&job, &snapshot, &digest, &count, &presetRaw, &ordinal, &version,
			&object.ID, &object.SHA, &object.Bucket, &object.Key, &object.Version, &object.Bytes, &object.MediaType); err != nil {
			rows.Close()
			return DownloadExportBatch{}, err
		}
		if job == uuid.Nil || snapshot == uuid.Nil || version == uuid.Nil || count < 1 || count > MaxDownloadItems ||
			ordinal != int64(len(masters)) || len(masters) >= int(count) || !validCatalogObject(object, l.Bucket, "application/json", 1048576) {
			rows.Close()
			return fail()
		}
		if len(masters) == 0 {
			out.JobID, out.SnapshotID, out.SnapshotDigest, expected = job, snapshot, digest, count
			if _, err = domain.ParseDigest(digest); err != nil {
				rows.Close()
				return fail()
			}
			normalized, e := canonical.Bytes(presetRaw)
			var envelope downloadPresetEnvelope
			decoder := json.NewDecoder(bytes.NewReader(normalized))
			decoder.DisallowUnknownFields()
			if e != nil || decoder.Decode(&envelope) != nil || envelope.ID == uuid.Nil || envelope.Revision < 1 ||
				envelope.Revision > 9007199254740991 || domainDigestInvalid(envelope.Hash) || !validDownloadPreset(envelope.Config) {
				rows.Close()
				return fail()
			}
			configHash, hashErr := canonical.Hash(envelope.Config)
			targetSHA, targetExists := l.TargetICCs[envelope.Config.ICCID]
			if hashErr != nil || configHash != envelope.Hash || !targetExists || domainDigestInvalid(targetSHA) {
				rows.Close()
				return fail()
			}
			out.Spec.TargetICCSHA256 = targetSHA
			out.Spec.Preset = envelope.Config
		} else if out.JobID != job || out.SnapshotID != snapshot || out.SnapshotDigest != digest || expected != count {
			rows.Close()
			return fail()
		}
		masters = append(masters, pendingDownloadMaster{ordinal, version, object})
		out.Sizes[object.ID] = object.Bytes
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return DownloadExportBatch{}, err
	}
	rows.Close()
	if expected == 0 || int64(len(masters)) != expected {
		return fail()
	}
	inputRows, err := l.DB.Query(ctx, "SELECT ordinal,role,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.download_export_inputs($1,$2)", tenant, run)
	if err != nil {
		return DownloadExportBatch{}, err
	}
	nested := map[int64]map[string]inspectedObject{}
	for inputRows.Next() {
		var ordinal int64
		var role string
		var object inspectedObject
		if err = inputRows.Scan(&ordinal, &role, &object.ID, &object.SHA, &object.Bytes, &object.Bucket, &object.Key, &object.Version, &object.MediaType); err != nil {
			inputRows.Close()
			return DownloadExportBatch{}, err
		}
		if ordinal < 0 || ordinal >= expected || (role != "working_rgb" && role != "alpha") ||
			!validCatalogObject(object, l.Bucket, "application/octet-stream", 2401048576) {
			inputRows.Close()
			return fail()
		}
		if nested[ordinal] == nil {
			nested[ordinal] = map[string]inspectedObject{}
		}
		if _, exists := nested[ordinal][role]; exists {
			inputRows.Close()
			return fail()
		}
		nested[ordinal][role] = object
		out.Sizes[object.ID] = object.Bytes
	}
	if err = inputRows.Err(); err != nil {
		inputRows.Close()
		return DownloadExportBatch{}, err
	}
	inputRows.Close()
	for _, master := range masters {
		roles := nested[master.ordinal]
		pixels, ok := roles["working_rgb"]
		if !ok {
			return fail()
		}
		binding := DecodeOutputBinding{Manifest: objectRef(master.object), Pixels: objectRef(pixels)}
		if alpha, exists := roles["alpha"]; exists {
			ref := objectRef(alpha)
			binding.Alpha = &ref
		}
		reader, e := l.Objects.Open(ctx, objectstore.Object{Key: master.object.Key, VersionID: master.object.Version,
			SHA256: master.object.SHA, Bytes: master.object.Bytes, ContentType: master.object.MediaType})
		if e != nil {
			return DownloadExportBatch{}, e
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, master.object.Bytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return DownloadExportBatch{}, readErr
		}
		if closeErr != nil {
			return DownloadExportBatch{}, closeErr
		}
		hash := sha256.Sum256(raw)
		if int64(len(raw)) != master.object.Bytes || hex.EncodeToString(hash[:]) != master.object.SHA {
			return fail()
		}
		image, e := review.ParseWorkingImage(raw)
		if e != nil || image.Pixels != binding.Pixels || (image.Alpha == nil) != (binding.Alpha == nil) ||
			image.Alpha != nil && *image.Alpha != *binding.Alpha || image.Color.ProfileID != "work_rec2020_gamma24_v1" ||
			image.DType != "float32" || image.Color.ICCSHA256 != l.WorkICCSHA256 {
			return fail()
		}
		out.Spec.Items = append(out.Spec.Items, DownloadExportItem{Ordinal: master.ordinal, Manifest: binding.Manifest, Image: image})
	}
	registry, err := LoadRegistry()
	if err != nil {
		return DownloadExportBatch{}, err
	}
	if _, err = CompileDownloadExports(out.Spec, registry); err != nil {
		return fail()
	}
	return out, nil
}

func domainDigestInvalid(value string) bool {
	_, err := domain.ParseDigest(value)
	return err != nil
}
