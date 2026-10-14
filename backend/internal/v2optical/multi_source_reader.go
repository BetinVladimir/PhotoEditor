package v2optical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

type SQLMultiSourceReader struct {
	DB      PerspectiveTransactions
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}

func (s SQLMultiSourceReader) lookup(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, base v2input.SessionVersionBinding) (perspectiveSourceLocator, error) {
	var zero perspectiveSourceLocator
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
	defer tx.Rollback(context.Background())
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
	if err = tx.QueryRow(ctx, "SELECT photo.v29_multi_source_document($1,$2,$3,$4,$5,$6::jsonb)", tenant, project, snapshot, who.UserID, who.SessionID, string(body)).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 32768 {
		return zero, v2input.ErrExpired
	}
	var locator perspectiveSourceLocator
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&locator) != nil || decoder.Decode(new(any)) != io.EOF || locator.ArtifactID != base.Source.ArtifactID || locator.SHA256 != base.Source.SHA256 || locator.Bucket != s.Bucket || locator.Key == "" || len(locator.Key) > 1024 || locator.Version == "" || locator.Version == "null" || len(locator.Version) > 1024 || locator.Media != "application/json" || locator.Bytes < 2 || locator.Bytes > PerspectiveWorkingImageMaxBytes {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return locator, nil
}

// Load reads every original WorkingImage in caller order. The shared source
// binding gate proves accepted-snapshot authority; it does not run perspective.
func (s SQLMultiSourceReader) Load(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, operation string, paramsRaw []byte) ([][]byte, error) {
	if s.Objects == nil {
		return nil, v2input.ErrInvalid
	}
	paramsRaw = append([]byte(nil), paramsRaw...)
	var frames []v2input.SessionVersionBinding
	switch operation {
	case "optical.panorama":
		p, err := ParsePanoramaParams(paramsRaw)
		if err != nil {
			return nil, err
		}
		frames = p.Frames
	case "optical.focus_stack":
		p, err := ParseFocusStackParams(paramsRaw)
		if err != nil {
			return nil, err
		}
		frames = p.Frames
	default:
		return nil, v2input.ErrInvalid
	}
	originals := make([][]byte, len(frames))
	locators := make([]perspectiveSourceLocator, len(frames))
	for i, frame := range frames {
		locator, err := s.lookup(ctx, who, tenant, project, snapshot, frame)
		if err != nil {
			return nil, err
		}
		locators[i] = locator
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		normal, err := canonical.Bytes(raw)
		sum := sha256.Sum256(raw)
		if int64(len(raw)) != locator.Bytes || err != nil || !bytes.Equal(normal, raw) || hex.EncodeToString(sum[:]) != frame.Source.SHA256 {
			return nil, v2input.ErrInvalid
		}
		originals[i] = raw
		current, err := s.lookup(ctx, who, tenant, project, snapshot, frame)
		if err != nil {
			return nil, err
		}
		if current != locator {
			return nil, v2input.ErrExpired
		}
	}
	if _, err := VerifyMultiWorkingImages(operation, paramsRaw, originals); err != nil {
		return nil, err
	}
	for i, frame := range frames {
		current, err := s.lookup(ctx, who, tenant, project, snapshot, frame)
		if err != nil {
			return nil, err
		}
		if current != locators[i] {
			return nil, v2input.ErrExpired
		}
	}
	return originals, ctx.Err()
}
