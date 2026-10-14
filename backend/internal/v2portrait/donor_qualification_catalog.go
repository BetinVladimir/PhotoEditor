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
)

type SQLDonorQualificationCatalog struct {
	DB     DonorConfirmationTransactions
	Bucket string
}

func (s SQLDonorQualificationCatalog) ResolveDonorQualification(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, pin DonorQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
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
	defer tx.Rollback(context.WithoutCancel(ctx))
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
	if err := tx.QueryRow(ctx, "SELECT photo.v28_donor_qualification_sources($1,$2,$3,$4,$5::jsonb)", tenant, project, who.UserID, who.SessionID, string(body)).Scan(&raw); err != nil {
		return "", zero, zero, err
	}
	if len(raw) < 2 || len(raw) > 32768 {
		return "", zero, zero, v2input.ErrExpired
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return "", zero, zero, v2input.ErrInvalid
	}
	var locations []DonorQualificationObject
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&locations) != nil || decoder.Decode(new(any)) != io.EOF || len(locations) != 2 {
		return "", zero, zero, v2input.ErrInvalid
	}
	var result [2]objectstore.Object
	for i, location := range locations {
		ref, label, max := pin.Calibration, "Calibration", int64(1<<20)
		if i == 1 {
			ref, label, max = pin.Report, "Report", 8<<20
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
