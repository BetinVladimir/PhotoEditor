package v2optical

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

type negativeSourceLocator struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"object_key"`
	Version    string    `json:"object_version"`
	Bytes      int64     `json:"bytes"`
	Media      string    `json:"media_type"`
}
type SQLNegativeSourceReader struct {
	DB      NegativeTransactions
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}

func (s SQLNegativeSourceReader) lookup(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, base v2input.SessionVersionBinding) (negativeSourceLocator, error) {
	var zero negativeSourceLocator
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Bucket == "" || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || base.AssetVersionID == uuid.Nil || !validPerspectiveRef(base.Source) {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(base)
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.v29_negative_source_document($1,$2,$3,$4,$5,$6::jsonb)", tenant, project, snapshot, who.UserID, who.SessionID, string(body)).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 32768 {
		return zero, v2input.ErrExpired
	}
	var locator negativeSourceLocator
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&locator) != nil || decoder.Decode(new(any)) != io.EOF || locator.ArtifactID != base.Source.ArtifactID || locator.SHA256 != base.Source.SHA256 || locator.Bucket != s.Bucket || locator.Key == "" || len(locator.Key) > 1024 || locator.Version == "" || locator.Version == "null" || len(locator.Version) > 1024 || locator.Media != "application/json" || locator.Bytes < 2 || locator.Bytes > 65536 {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return locator, nil
}

// Load reads an authorized immutable WorkingImage outside SQL, verifies its
// exact content and canvas binding, then repeats source authority/locator checks.
func (s SQLNegativeSourceReader) Load(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, params NegativeParams) ([]byte, error) {
	base := params.Base
	if s.Objects == nil {
		return nil, v2input.ErrInvalid
	}
	locator, err := s.lookup(ctx, who, tenant, project, snapshot, base)
	if err != nil {
		return nil, err
	}
	reader, err := s.Objects.Open(ctx, objectstore.Object{Key: locator.Key, VersionID: locator.Version, SHA256: locator.SHA256, Bytes: locator.Bytes, ContentType: locator.Media})
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, locator.Bytes+1))
	closeErr := reader.Close()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) != locator.Bytes {
		return nil, v2input.ErrInvalid
	}
	if _, err = VerifyNegativeWorkingImage(params, raw); err != nil {
		return nil, err
	}
	current, err := s.lookup(ctx, who, tenant, project, snapshot, base)
	if err != nil {
		return nil, err
	}
	if current != locator {
		return nil, v2input.ErrExpired
	}
	return raw, nil
}
