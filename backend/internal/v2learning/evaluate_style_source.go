package v2learning

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SQLLearningEvaluateStyleSource accepts only immutable exported StyleV2
// versions belonging to the frozen learning workflow's project.
type SQLLearningEvaluateStyleSource struct {
	DB      *pgxpool.Pool
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func (s SQLLearningEvaluateStyleSource) Load(ctx context.Context,
	lease LearningEvaluateLease, ref v2input.Ref) ([]byte, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		!validLearningEvaluateLease(lease) || !validManifestRef(ref) {
		return nil, ErrInvalidTrainingDraft
	}
	var found v2input.Ref
	var object objectstore.Object
	var bucket string
	err := s.DB.QueryRow(ctx, `SELECT artifact_id,sha256,bytes,bucket,
	 object_key,object_version,media_type
	 FROM photo.v23_evaluate_base_style_source($1,$2,$3,$4,$5,$6)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		ref.ArtifactID, ref.SHA256).Scan(&found.ArtifactID, &found.SHA256,
		&object.Bytes, &bucket, &object.Key, &object.VersionID,
		&object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTrainingDraftConflict
	}
	if err != nil {
		return nil, err
	}
	if found != ref || bucket != s.Bucket {
		return nil, ErrInvalidTrainingDraft
	}
	object.SHA256 = found.SHA256
	body, err := ReadFrozenRootDocument(ctx, s.Objects, object, ref)
	if err != nil {
		return nil, err
	}
	var style struct {
		SchemaVersion   string `json:"schema_version"`
		RendererVersion string `json:"renderer_version"`
	}
	if json.Unmarshal(body, &style) != nil || style.SchemaVersion != "2.0.0" ||
		style.RendererVersion != "photo_renderer_v2" {
		return nil, ErrInvalidTrainingDraft
	}
	return body, nil
}
