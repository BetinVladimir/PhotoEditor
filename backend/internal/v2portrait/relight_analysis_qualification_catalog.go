package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type SQLRelightAnalysisQualificationCatalog struct {
	DB     DonorConfirmationTransactions
	Bucket string
}

func (s SQLRelightAnalysisQualificationCatalog) ResolveRelightAnalysisQualification(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, operation string, runtime RelightAnalysisRuntime) (string, objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return "", zero, err
	}
	if s.DB == nil || s.Bucket == "" || len(s.Bucket) > 255 || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return "", zero, v2input.ErrInvalid
	}
	if operation != "depth.predict" && operation != "mask.person" || !validRelightTransportRef(runtime.Qualification) {
		return "", zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(runtime)
	if err != nil {
		return "", zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", zero, err
	}
	if tx == nil {
		return "", zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return "", zero, err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return "", zero, err
	}
	if role != "photo_scheduler" {
		return "", zero, v2input.ErrInvalid
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.v28_relight_analysis_qualification_source($1,$2,$3,$4,$5,$6::jsonb)", tenant, project, who.UserID, who.SessionID, operation, string(body)).Scan(&raw); err != nil {
		return "", zero, err
	}
	if len(raw) < 2 || len(raw) > 32768 {
		return "", zero, v2input.ErrExpired
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return "", zero, v2input.ErrInvalid
	}
	var location DonorQualificationObject
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&location) != nil || decoder.Decode(new(any)) != io.EOF {
		return "", zero, v2input.ErrInvalid
	}
	ref := runtime.Qualification
	if location.Contract != "Report" || location.ArtifactID != ref.ArtifactID || location.SHA256 != ref.SHA256 || location.Bucket != s.Bucket || location.Key == "" || len(location.Key) > 1024 || location.Version == "" || location.Version == "null" || len(location.Version) > 1024 || location.Bytes < 2 || location.Bytes > 1048576 {
		return "", zero, v2input.ErrInvalid
	}
	result := objectstore.Object{Key: location.Key, VersionID: location.Version, SHA256: location.SHA256, Bytes: location.Bytes, ContentType: "application/json"}
	if err := ctx.Err(); err != nil {
		return "", zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", zero, err
	}
	return s.Bucket, result, nil
}
