package provenance

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// Store must use dedicated lineage authority, stable content reservation/quota,
// current actor/session/export ACL, expected export pins and fenced completion.
// The ordinary task_params or recipe upload scope is not lineage authority.
type SigningLineageStore interface {
	ReserveSigningLineage(context.Context, auth.Identity, ReadySigningExport, v2input.Ref, []byte) (artifactjson.Reservation, error)
	ReadySigningLineageVersion(context.Context, auth.Identity, ReadySigningExport, v2input.Ref, []byte) (string, error)
	CompleteSigningLineage(context.Context, auth.Identity, ReadySigningExport, v2input.Ref, []byte, artifactjson.Reservation, objectstore.Object) (v2input.Ref, error)
}
type ImmutableSigningLineagePublisher struct {
	Catalog SigningExportCatalog
	Store   SigningLineageStore
	Objects artifactjson.Objects
}

func (s ImmutableSigningLineagePublisher) PublishSigningLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, expected v2input.Ref, raw []byte) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Catalog == nil || s.Store == nil || s.Objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || ready.Run == uuid.Nil || !ref(ready.Ref) || !ref(expected) {
		return zero, ErrInvalid
	}
	if _, ok := s.Objects.(signingPolicyVersionReader); !ok {
		return zero, ErrInvalid
	}
	// Reject oversized caller buffers before allocating an owned copy.
	if len(raw) == 0 || len(raw) > 8<<20 {
		return zero, ErrInvalid
	}
	body := append([]byte(nil), raw...)
	if _, err := DecodeSigningLineage(body, expected); err != nil {
		return zero, err
	}
	if expected.ArtifactID != uuid.NewSHA1(ready.ExportID, []byte("signing:lineage:"+expected.SHA256)) {
		return zero, ErrInvalid
	}
	check := func() error {
		current, err := s.Catalog.LookupSigningExport(ctx, who, ready.Tenant, ready.ExportID)
		if err != nil {
			return err
		}
		if current != ready {
			return ErrInvalid
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return zero, err
	}
	reservation, err := s.Store.ReserveSigningLineage(ctx, who, ready, expected, append([]byte(nil), body...))
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if reservation.ArtifactID != expected.ArtifactID || reservation.Token == uuid.Nil {
		return zero, objectstore.ErrIntegrity
	}
	if reservation.Ready != nil {
		if reservation.Ready.ID != expected.ArtifactID || reservation.Ready.SHA256 != expected.SHA256 {
			return zero, objectstore.ErrIntegrity
		}
		version, err := s.Store.ReadySigningLineageVersion(ctx, who, ready, expected, append([]byte(nil), body...))
		if err != nil {
			return zero, err
		}
		if version == "" || version == "null" || len(version) > 1024 {
			return zero, objectstore.ErrIntegrity
		}
		key, err := objectstore.DocumentKey(ready.Tenant, ready.Run, expected.ArtifactID)
		if err != nil {
			return zero, err
		}
		reader, ok := s.Objects.(signingPolicyVersionReader)
		if !ok {
			return zero, ErrInvalid
		}
		if err := verifySigningPolicyVersion(ctx, reader, objectstore.Object{
			Key: key, VersionID: version, SHA256: expected.SHA256,
			Bytes: int64(len(body)), ContentType: "application/json",
		}); err != nil {
			return zero, err
		}
		if err := check(); err != nil {
			return zero, err
		}
		return expected, nil
	}
	key, err := objectstore.DocumentKey(ready.Tenant, ready.Run, expected.ArtifactID)
	if err != nil {
		return zero, err
	}
	object, err := s.Objects.PutImmutable(ctx, key, bytes.NewReader(body), int64(len(body)), expected.SHA256, "application/json")
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		object, err = s.Objects.RecoverImmutable(ctx, key, int64(len(body)), expected.SHA256, "application/json")
	}
	if err != nil {
		return zero, err
	}
	if object.Key != key || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || object.SHA256 != expected.SHA256 || object.Bytes != int64(len(body)) || object.ContentType != "application/json" {
		return zero, objectstore.ErrIntegrity
	}
	if err := check(); err != nil {
		return zero, err
	}
	receipt, err := s.Store.CompleteSigningLineage(ctx, who, ready, expected, append([]byte(nil), body...), reservation, object)
	if err != nil {
		return zero, err
	}
	if receipt != expected {
		return zero, objectstore.ErrIntegrity
	}
	if err := check(); err != nil {
		return zero, err
	}
	return receipt, nil
}

var _ SigningLineagePublisher = ImmutableSigningLineagePublisher{}
