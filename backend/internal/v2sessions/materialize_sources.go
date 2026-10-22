package v2sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type sceneObject struct {
	ID     uuid.UUID
	Bucket string
	Object objectstore.Object
}

func (m PlanMaterializer) readSceneObject(ctx context.Context, source sceneObject) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.Objects == nil || m.Bucket == "" {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if source.ID == uuid.Nil || source.Bucket != m.Bucket ||
		source.Object.Key == "" || source.Object.VersionID == "" || source.Object.VersionID == "null" ||
		source.Object.ContentType != "application/json" ||
		source.Object.Bytes < 2 || source.Object.Bytes > 8<<20 ||
		!digestPattern.MatchString(source.Object.SHA256) {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	reader, err := m.Objects.Open(ctx, source.Object)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, 8<<20+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(raw)) != source.Object.Bytes ||
		!exactCanonicalJSON(raw, source.Object.SHA256, 8<<20) {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	return raw, nil
}

func (m PlanMaterializer) frozenBindings(ctx context.Context, tenantID, runID,
	snapshotID uuid.UUID, snapshotDigest string, itemCount int) (
	map[int]v2input.SessionVersionBinding, error) {
	var source sceneObject
	err := m.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,
		object_version,media_type FROM photo.v22_scene_input_snapshot_source($1,$2)`,
		tenantID, runID).Scan(&source.ID, &source.Object.SHA256,
		&source.Object.Bytes, &source.Bucket, &source.Object.Key,
		&source.Object.VersionID, &source.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	if err != nil {
		return nil, err
	}
	raw, err := m.readSceneObject(ctx, source)
	if err != nil {
		return nil, err
	}
	var snapshot v2input.SessionInputSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || decoder.Decode(new(any)) != io.EOF ||
		snapshot.SnapshotID != snapshotID || snapshot.SnapshotDigest != snapshotDigest ||
		len(snapshot.Items) != itemCount {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	compiled, body, err := v2input.CompileSessionInputSnapshot(runID, snapshot.Items)
	if err != nil || compiled.SnapshotID != snapshotID ||
		compiled.SnapshotDigest != snapshotDigest || !bytes.Equal(body, raw) {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	bindings := make(map[int]v2input.SessionVersionBinding, itemCount)
	for _, item := range snapshot.Items {
		bindings[item.Ordinal] = item.Binding
	}
	rows, err := m.DB.Query(ctx, `SELECT ordinal,asset_version_id,artifact_id,sha256
		FROM photo.v22_scene_snapshot_members($1,$2)`, tenantID, runID)
	if err != nil {
		return nil, err
	}
	count := 0
	for rows.Next() {
		var ordinal int64
		var versionID, artifactID uuid.UUID
		var sha string
		if err = rows.Scan(&ordinal, &versionID, &artifactID, &sha); err != nil {
			break
		}
		binding, exists := bindings[int(ordinal)]
		if ordinal < 0 || ordinal >= int64(itemCount) || !exists ||
			binding.AssetVersionID != versionID ||
			binding.Source.ArtifactID != artifactID || binding.Source.SHA256 != sha {
			err = domain.NewError("ANALYSIS_CONFLICT", 409)
			break
		}
		count++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if count != itemCount {
		return nil, domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	return bindings, nil
}

func (m PlanMaterializer) verifyCorrections(ctx context.Context, tenantID, runID uuid.UUID,
	plan scenePlan, bindings map[int]v2input.SessionVersionBinding) error {
	rows, err := m.DB.Query(ctx, `SELECT artifact_id,sha256,bytes,bucket,object_key,
		object_version,media_type FROM photo.v22_scene_correction_sources($1,$2)`,
		tenantID, runID)
	if err != nil {
		return err
	}
	sources := make(map[uuid.UUID]sceneObject, len(plan.Groups))
	for rows.Next() {
		var source sceneObject
		if err = rows.Scan(&source.ID, &source.Object.SHA256,
			&source.Object.Bytes, &source.Bucket, &source.Object.Key,
			&source.Object.VersionID, &source.Object.ContentType); err != nil {
			break
		}
		if _, exists := sources[source.ID]; exists {
			err = domain.NewError("ANALYSIS_CONFLICT", 409)
			break
		}
		sources[source.ID] = source
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if len(sources) != len(plan.Groups) {
		return domain.NewError("ANALYSIS_CONFLICT", 409)
	}
	seen := make(map[uuid.UUID]bool, len(plan.Groups))
	for _, group := range plan.Groups {
		source, exists := sources[group.Corrections.ArtifactID]
		if !exists || source.Object.SHA256 != group.Corrections.SHA256 ||
			seen[source.ID] {
			return domain.NewError("ANALYSIS_CONFLICT", 409)
		}
		seen[source.ID] = true
		raw, err := m.readSceneObject(ctx, source)
		if err != nil {
			return err
		}
		if err = validateCorrectionPlan(raw, group.Corrections,
			plan.SnapshotDigest, group, bindings); err != nil {
			return err
		}
	}
	return nil
}
