package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type restorationSelectedArrayCatalog interface {
	lookupArray(context.Context, auth.Identity, RestorationDecisionIdentity, []byte, string, review.ArtifactRef) (objectstore.Object, error)
}

type SQLRestorationSelectedArrays struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s SQLRestorationSelectedArrays) lookupArray(ctx context.Context, who auth.Identity,
	id RestorationDecisionIdentity, image []byte, channel string,
	ref review.ArtifactRef) (objectstore.Object, error) {
	var zero objectstore.Object
	if s.DB == nil || s.Bucket == "" || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		id.Tenant == uuid.Nil || id.Project == uuid.Nil || id.Review == uuid.Nil ||
		id.Task == uuid.Nil || id.Run == uuid.Nil || !validRestorationSelectedDigest(id.DecisionHash) ||
		(id.Route != "base" && id.Route != "candidate") ||
		(channel != "pixels" && channel != "alpha") || ref.ID == uuid.Nil ||
		!validRestorationSelectedDigest(ref.SHA256) || len(image) < 2 || len(image) > 1048576 {
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
	err = tx.QueryRow(bounded, `SELECT photo.read_v28_restoration_selected_array($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		id.Tenant, id.Project, who.UserID, who.SessionID, id.Review, id.Task, id.Run,
		id.DecisionHash, id.Route, string(image), channel, ref.ID, ref.SHA256, s.Bucket).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	object, err := decodeRestorationSelectedArray(raw, ref, s.Bucket)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return object, nil
}

func decodeRestorationSelectedArray(raw []byte, ref review.ArtifactRef, bucket string) (objectstore.Object, error) {
	var zero objectstore.Object
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var value restorationSelectedLocator
	if json.Unmarshal(normal, &value) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(exact, normal) || value.ArtifactID != ref.ID ||
		value.SHA256 != ref.SHA256 || value.Bucket != bucket || value.Key == "" ||
		len(value.Key) > 1024 || value.Version == "" || value.Version == "null" ||
		len(value.Version) > 1024 || value.Bytes < 14 || value.Bytes > 2400065536 ||
		(value.MediaType != "application/x-npy" && value.MediaType != "application/octet-stream") {
		return zero, v2input.ErrInvalid
	}
	return objectstore.Object{Key: value.Key, VersionID: value.Version, SHA256: value.SHA256,
		Bytes: value.Bytes, ContentType: value.MediaType}, nil
}

type restorationSelectedArrayWitness struct {
	Channel string
	Ref     review.ArtifactRef
	Object  objectstore.Object
}

// VerifiedRestorationSelectedArrays owns exact physical array witnesses but is
// not itself a child Apply admission or quota reservation.
type VerifiedRestorationSelectedArrays struct {
	selection VerifiedRestorationSelection
	arrays    []restorationSelectedArrayWitness
}

func (v VerifiedRestorationSelectedArrays) Selection() VerifiedRestorationSelection {
	return v.selection
}

func VerifyRestorationSelectedArrays(ctx context.Context, catalog restorationSelectedArrayCatalog,
	objects v2input.SessionAnalysisObjectReader, sourceCatalog restorationSelectedCatalog,
	who auth.Identity, selected VerifiedRestorationSelection) (VerifiedRestorationSelectedArrays, error) {
	var zero VerifiedRestorationSelectedArrays
	if catalog == nil || objects == nil || sourceCatalog == nil || selected.source.Source == nil || selected.raw == "" {
		return zero, v2input.ErrInvalid
	}
	id := RestorationDecisionIdentity{Tenant: selected.source.Tenant, Project: selected.source.Project,
		Review: selected.source.Review, Task: selected.source.Task, Run: selected.source.Run,
		DecisionHash: selected.source.DecisionHash, Route: selected.source.Route}
	image, err := review.ParseWorkingImage([]byte(selected.raw))
	if err != nil || image.DType != "float32" {
		return zero, v2input.ErrInvalid
	}
	entries := []struct {
		channel string
		ref     review.ArtifactRef
	}{{"pixels", image.Pixels}}
	if image.Alpha != nil {
		entries = append(entries, struct {
			channel string
			ref     review.ArtifactRef
		}{"alpha", *image.Alpha})
	}
	witnesses := make([]restorationSelectedArrayWitness, 0, len(entries))
	for _, entry := range entries {
		obj, err := catalog.lookupArray(ctx, who, id, []byte(selected.raw), entry.channel, entry.ref)
		if err != nil {
			return zero, err
		}
		channels := int64(3)
		if entry.channel == "alpha" {
			channels = 1
		}
		if obj.SHA256 != entry.ref.SHA256 || obj.Key == "" || obj.VersionID == "" ||
			obj.VersionID == "null" || obj.Bytes < 14 ||
			obj.Bytes > image.Canvas.Width*image.Canvas.Height*channels*4+65536 ||
			(obj.ContentType != "application/x-npy" && obj.ContentType != "application/octet-stream") {
			return zero, v2input.ErrInvalid
		}
		reader, err := objects.Open(ctx, obj)
		if err != nil {
			if reader != nil {
				_ = reader.Close()
			}
			return zero, err
		}
		if reader == nil {
			return zero, v2input.ErrInvalid
		}
		if channels == 1 {
			err = masknpy.VerifyAlphaNPY(ctx, reader, obj.Bytes, obj.SHA256, image.Canvas.Width, image.Canvas.Height)
		}
		if channels == 3 {
			err = masknpy.VerifyFloat32ArrayNPY(ctx, reader, obj.Bytes, obj.SHA256, image.Canvas.Width, image.Canvas.Height, 3, false)
		}
		closeErr := reader.Close()
		if err != nil {
			return zero, err
		}
		if closeErr != nil {
			return zero, closeErr
		}
		current, err := catalog.lookupArray(ctx, who, id, []byte(selected.raw), entry.channel, entry.ref)
		if err != nil {
			return zero, err
		}
		if current != obj {
			return zero, v2input.ErrExpired
		}
		witnesses = append(witnesses, restorationSelectedArrayWitness{Channel: entry.channel, Ref: entry.ref, Object: obj})
	}
	current, err := sourceCatalog.lookup(ctx, who, id)
	if err != nil {
		return zero, err
	}
	a, err := canonical.Marshal(selected.source)
	if err != nil {
		return zero, err
	}
	b, err := canonical.Marshal(current)
	if err != nil || !bytes.Equal(a, b) {
		return zero, v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return VerifiedRestorationSelectedArrays{selection: selected, arrays: witnesses}, nil
}
