package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type NegativeTransactions interface {
	Begin(context.Context) (pgx.Tx, error)
}
type NegativeQualificationObject struct {
	Contract   string    `json:"contract"`
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"object_key"`
	Version    string    `json:"object_version"`
	Bytes      int64     `json:"bytes"`
}
type SQLNegativeQualificationCatalog struct {
	DB     NegativeTransactions
	Bucket string
}

func (s SQLNegativeQualificationCatalog) ResolveNegativeQualification(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, pin NegativeQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return "", zero, zero, err
	}
	if s.DB == nil || s.Bucket == "" || len(s.Bucket) > 255 || tenant == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return "", zero, zero, v2input.ErrInvalid
	}
	body, err := pin.Compile()
	if err != nil {
		return "", zero, zero, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", zero, zero, err
	}
	if tx == nil {
		return "", zero, zero, v2input.ErrInvalid
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return "", zero, zero, err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return "", zero, zero, err
	}
	if role != "photo_scheduler" {
		return "", zero, zero, v2input.ErrInvalid
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.v29_negative_qualification_sources($1,$2,$3,$4,$5::jsonb)", tenant, project, who.UserID, who.SessionID, string(body)).Scan(&raw); err != nil {
		return "", zero, zero, err
	}
	if len(raw) < 2 || len(raw) > 32768 {
		return "", zero, zero, v2input.ErrExpired
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return "", zero, zero, v2input.ErrInvalid
	}
	var locations []NegativeQualificationObject
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&locations) != nil || decoder.Decode(new(any)) != io.EOF || len(locations) != 2 {
		return "", zero, zero, v2input.ErrInvalid
	}
	var result [2]objectstore.Object
	for i, location := range locations {
		ref, label, max := pin.Calibration, "NegativeCalibration", int64(65536)
		if i == 1 {
			ref, label, max = pin.Report, "QualificationReport", 8<<20
		}
		if location.Contract != label || location.ArtifactID != ref.ArtifactID || location.SHA256 != ref.SHA256 || location.Bucket != s.Bucket || location.Key == "" || len(location.Key) > 1024 || location.Version == "" || location.Version == "null" || len(location.Version) > 1024 || location.Bytes < 2 || location.Bytes > max {
			return "", zero, zero, v2input.ErrInvalid
		}
		result[i] = objectstore.Object{Key: location.Key, VersionID: location.Version, SHA256: location.SHA256, Bytes: location.Bytes, ContentType: "application/json"}
	}
	if err := ctx.Err(); err != nil {
		return "", zero, zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", zero, zero, err
	}
	return s.Bucket, result[1], result[0], nil
}
