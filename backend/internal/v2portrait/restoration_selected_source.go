package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type RestorationDecisionIdentity struct {
	Tenant, Project, Review, Task, Run uuid.UUID
	DecisionHash, Route                string
}

type restorationSelectedLocator struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"object_key"`
	Version    string    `json:"object_version"`
	Bytes      int64     `json:"bytes"`
	MediaType  string    `json:"media_type"`
}

type restorationSelectedSource struct {
	SchemaVersion int                         `json:"schema_version"`
	Tenant        uuid.UUID                   `json:"tenant_id"`
	Project       uuid.UUID                   `json:"project_id"`
	Review        uuid.UUID                   `json:"review_id"`
	Task          uuid.UUID                   `json:"origin_task_id"`
	Run           uuid.UUID                   `json:"origin_run_id"`
	DecisionHash  string                      `json:"decision_hash"`
	Route         string                      `json:"route"`
	Source        *restorationSelectedLocator `json:"source"`
}

func decodeRestorationSelectedSource(raw []byte, id RestorationDecisionIdentity, bucket string) (restorationSelectedSource, error) {
	var zero restorationSelectedSource
	if len(raw) < 2 || len(raw) > 32768 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var out restorationSelectedSource
	if json.Unmarshal(normal, &out) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(out)
	if err != nil || !bytes.Equal(exact, normal) || out.SchemaVersion != 1 ||
		out.Tenant != id.Tenant || out.Project != id.Project || out.Review != id.Review ||
		out.Task != id.Task || out.Run != id.Run || out.DecisionHash != id.DecisionHash ||
		out.Route != id.Route {
		return zero, v2input.ErrInvalid
	}
	if out.Route == "skip" {
		if out.Source != nil {
			return zero, v2input.ErrInvalid
		}
		return out, nil
	}
	if out.Route != "candidate" && out.Route != "base" {
		return zero, v2input.ErrInvalid
	}
	s := out.Source
	if s == nil || s.ArtifactID == uuid.Nil || !validRestorationSelectedDigest(s.SHA256) ||
		s.Bucket != bucket || s.Key == "" || len(s.Key) > 1024 || s.Version == "" ||
		s.Version == "null" || len(s.Version) > 1024 || s.Bytes < 2 || s.Bytes > 1048576 ||
		s.MediaType != "application/json" {
		return zero, v2input.ErrInvalid
	}
	return out, nil
}

func validRestorationSelectedDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, b := range []byte(value) {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

type restorationSelectedCatalog interface {
	lookup(context.Context, auth.Identity, RestorationDecisionIdentity) (restorationSelectedSource, error)
}

type SQLRestorationSelectedCatalog struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s SQLRestorationSelectedCatalog) lookup(ctx context.Context, who auth.Identity, id RestorationDecisionIdentity) (restorationSelectedSource, error) {
	var zero restorationSelectedSource
	if s.DB == nil || s.Bucket == "" || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		id.Tenant == uuid.Nil || id.Project == uuid.Nil || id.Review == uuid.Nil ||
		id.Task == uuid.Nil || id.Run == uuid.Nil || !validRestorationSelectedDigest(id.DecisionHash) ||
		(id.Route != "candidate" && id.Route != "base" && id.Route != "skip") {
		return zero, v2input.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", id.Tenant.String()); err != nil {
		return zero, err
	}
	var raw []byte
	err = tx.QueryRow(bounded, `SELECT photo.read_v28_restoration_selected_source($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id.Tenant, id.Project, who.UserID, who.SessionID, id.Review, id.Task, id.Run, id.DecisionHash, id.Route).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	out, err := decodeRestorationSelectedSource(raw, id, s.Bucket)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return out, nil
}

// VerifiedRestorationSelection owns the selected canonical WorkingImage bytes.
// It is metadata proof only; Apply admission must also verify array closure.
type VerifiedRestorationSelection struct {
	source restorationSelectedSource
	raw    string
}

func (v VerifiedRestorationSelection) Route() string             { return v.source.Route }
func (v VerifiedRestorationSelection) WorkingImageBytes() []byte { return []byte(v.raw) }
func (v VerifiedRestorationSelection) Image() *review.ArtifactRef {
	if v.source.Source == nil {
		return nil
	}
	return &review.ArtifactRef{ID: v.source.Source.ArtifactID, SHA256: v.source.Source.SHA256}
}

func LoadRestorationSelectedSource(ctx context.Context, db *pgxpool.Pool,
	objects v2input.SessionAnalysisObjectReader, bucket string, who auth.Identity,
	id RestorationDecisionIdentity) (VerifiedRestorationSelection, error) {
	return loadRestorationSelectedSource(ctx, SQLRestorationSelectedCatalog{DB: db, Bucket: bucket}, objects, who, id)
}

func loadRestorationSelectedSource(ctx context.Context, catalog restorationSelectedCatalog,
	objects v2input.SessionAnalysisObjectReader, who auth.Identity,
	id RestorationDecisionIdentity) (VerifiedRestorationSelection, error) {
	var zero VerifiedRestorationSelection
	if catalog == nil || objects == nil {
		return zero, v2input.ErrInvalid
	}
	before, err := catalog.lookup(ctx, who, id)
	if err != nil {
		return zero, err
	}
	var raw []byte
	if before.Source != nil {
		locator := *before.Source
		reader, readErr := objects.Open(ctx, objectstore.Object{Key: locator.Key, VersionID: locator.Version,
			SHA256: locator.SHA256, Bytes: locator.Bytes, ContentType: locator.MediaType})
		if readErr != nil {
			if reader != nil {
				_ = reader.Close()
			}
			return zero, readErr
		}
		if reader == nil {
			return zero, v2input.ErrInvalid
		}
		raw, err = io.ReadAll(io.LimitReader(reader, locator.Bytes+1))
		closeErr := reader.Close()
		if err != nil {
			return zero, err
		}
		if closeErr != nil {
			return zero, closeErr
		}
		if err = ctx.Err(); err != nil {
			return zero, err
		}
		sum := sha256.Sum256(raw)
		if int64(len(raw)) != locator.Bytes || hex.EncodeToString(sum[:]) != locator.SHA256 {
			return zero, v2input.ErrInvalid
		}
		image, parseErr := review.ParseWorkingImage(raw)
		if parseErr != nil || image.DType != "float32" {
			return zero, v2input.ErrInvalid
		}
		exact, marshalErr := canonical.Marshal(image)
		if marshalErr != nil || !bytes.Equal(exact, raw) {
			return zero, v2input.ErrInvalid
		}
	}
	after, err := catalog.lookup(ctx, who, id)
	if err != nil {
		return zero, err
	}
	a, err := canonical.Marshal(before)
	if err != nil {
		return zero, err
	}
	b, err := canonical.Marshal(after)
	if err != nil || !bytes.Equal(a, b) {
		return zero, v2input.ErrExpired
	}
	if before.Source != nil {
		owned := *before.Source
		before.Source = &owned
	}
	return VerifiedRestorationSelection{source: before, raw: string(raw)}, nil
}
