package provenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// StorageRun is the immutable first reservation's owning run. A tenant policy
// can be reused by later exports; their runs must not change its object key.
type SigningPolicyReservation struct {
	Reservation  artifactjson.Reservation
	StorageRun   uuid.UUID
	ReadyVersion string
}

// Implementations must enforce tenant/editor/session/current export access,
// installed content identity, quota and fenced immutable completion. A ready
// replay must prove the physical versioned artifact, not merely metadata.
// Cleaned reservations cannot be reopened while old purge may still execute.
type SigningPolicyStore interface {
	ReserveSigningPolicy(context.Context, auth.Identity, ReadySigningExport, v2input.Ref, []byte) (SigningPolicyReservation, error)
	CompleteSigningPolicy(context.Context, auth.Identity, ReadySigningExport, v2input.Ref, []byte, SigningPolicyReservation, objectstore.Object) (v2input.Ref, error)
}

type ImmutableSigningPolicyPublisher struct {
	Catalog  SigningExportCatalog
	Registry *SigningPolicyRegistry
	Store    SigningPolicyStore
	Objects  artifactjson.Objects
	Clock    func() time.Time
}

type signingPolicyVersionReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

func verifySigningPolicyVersion(ctx context.Context, reader signingPolicyVersionReader, expected objectstore.Object) error {
	if reader == nil {
		return ErrInvalid
	}
	stream, err := reader.Open(ctx, expected)
	if err != nil {
		return err
	}
	if stream == nil {
		return objectstore.ErrIntegrity
	}
	digest := sha256.New()
	n, readErr := io.Copy(io.Discard, io.TeeReader(io.LimitReader(stream, expected.Bytes+1), digest))
	closeErr := stream.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != expected.Bytes || hex.EncodeToString(digest.Sum(nil)) != expected.SHA256 {
		return objectstore.ErrIntegrity
	}
	return nil
}

func (p ImmutableSigningPolicyPublisher) PublishSigningPolicy(ctx context.Context, who auth.Identity, ready ReadySigningExport, params SignParams) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Catalog == nil || p.Registry == nil || p.Store == nil || p.Objects == nil || p.Clock == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || ready.Run == uuid.Nil || params.Export != ready.Ref {
		return zero, ErrInvalid
	}
	if _, ok := p.Objects.(signingPolicyVersionReader); !ok {
		return zero, ErrInvalid
	}
	expected, body, err := p.Registry.PolicyDocument(ctx, ready.Tenant, params, ready.Object.ContentType, p.Clock())
	if err != nil {
		return zero, err
	}
	check := func() error {
		current, err := p.Catalog.LookupSigningExport(ctx, who, ready.Tenant, ready.ExportID)
		if err != nil {
			return err
		}
		if current != ready {
			return ErrInvalid
		}
		reference, raw, err := p.Registry.PolicyDocument(ctx, ready.Tenant, params, ready.Object.ContentType, p.Clock())
		if err != nil {
			return err
		}
		if reference != expected || !bytes.Equal(raw, body) {
			return ErrInvalid
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return zero, err
	}
	saved, err := p.Store.ReserveSigningPolicy(ctx, who, ready, expected, append([]byte(nil), body...))
	if err != nil {
		return zero, err
	}
	if err := check(); err != nil {
		return zero, err
	}
	reservation := saved.Reservation
	if saved.StorageRun == uuid.Nil || reservation.ArtifactID != expected.ArtifactID || reservation.Token == uuid.Nil {
		return zero, objectstore.ErrIntegrity
	}
	key, err := objectstore.DocumentKey(ready.Tenant, saved.StorageRun, expected.ArtifactID)
	if err != nil {
		return zero, err
	}
	if reservation.Ready != nil {
		if reservation.Ready.ID != expected.ArtifactID || reservation.Ready.SHA256 != expected.SHA256 {
			return zero, objectstore.ErrIntegrity
		}
		if saved.ReadyVersion == "" || saved.ReadyVersion == "null" || len(saved.ReadyVersion) > 1024 {
			return zero, objectstore.ErrIntegrity
		}
		reader, ok := p.Objects.(signingPolicyVersionReader)
		if !ok {
			return zero, ErrInvalid
		}
		if err := verifySigningPolicyVersion(ctx, reader, objectstore.Object{
			Key: key, VersionID: saved.ReadyVersion, SHA256: expected.SHA256,
			Bytes: int64(len(body)), ContentType: "application/json",
		}); err != nil {
			return zero, err
		}
		if err := check(); err != nil {
			return zero, err
		}
		return expected, nil
	}
	object, err := p.Objects.PutImmutable(ctx, key, bytes.NewReader(body), int64(len(body)), expected.SHA256, "application/json")
	if cancelled := ctx.Err(); cancelled != nil {
		return zero, cancelled
	}
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		object, err = p.Objects.RecoverImmutable(ctx, key, int64(len(body)), expected.SHA256, "application/json")
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
	receipt, err := p.Store.CompleteSigningPolicy(ctx, who, ready, expected, append([]byte(nil), body...), saved, object)
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
