package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ImmutableStore interface {
	PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error)
	RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error)
}

type immutableVersionReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

type Publisher struct {
	DB      *pgxpool.Pool
	Objects ImmutableStore
	Bucket  string
}

type reservation struct {
	ArtifactID uuid.UUID
	Token      uuid.UUID
	Ready      bool
}

func validPublishedDraftObject(object objectstore.Object, key, digest string, size int64) bool {
	return object.Key == key && object.VersionID != "" && object.VersionID != "null" &&
		object.SHA256 == digest && object.Bytes == size && object.ContentType == "application/json"
}

func verifyExactDraftObjectStream(ctx context.Context, stream io.ReadCloser, object objectstore.Object) error {
	if stream == nil || object.Bytes < 2 || object.Bytes > 1048576 {
		if stream != nil {
			_ = stream.Close()
		}
		return objectstore.ErrIntegrity
	}
	if err := ctx.Err(); err != nil {
		_ = stream.Close()
		return err
	}
	hash := sha256.New()
	count, readErr := io.Copy(hash, io.LimitReader(stream, object.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if count != object.Bytes || hex.EncodeToString(hash.Sum(nil)) != object.SHA256 {
		return objectstore.ErrIntegrity
	}
	return nil
}

func (p Publisher) readyDraftObject(ctx context.Context, who auth.Identity, tenantID, documentID uuid.UUID, ref v2input.Ref, digest string) (objectstore.Object, error) {
	var object objectstore.Object
	err := tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		object.SHA256 = digest
		return tx.QueryRow(ctx, `SELECT object_key,object_version,bytes,media_type
		 FROM photo.v21_ready_draft_object($1,$2,$3,$4,$5,$6,$7)`,
			tenantID, documentID, who.UserID, who.SessionID, ref.ArtifactID, digest, p.Bucket).
			Scan(&object.Key, &object.VersionID, &object.Bytes, &object.ContentType)
	})
	if err != nil {
		return objectstore.Object{}, err
	}
	key, err := objectstore.V2DraftKey(tenantID, ref.ArtifactID)
	if err != nil || !validPublishedDraftObject(object, key, digest, object.Bytes) || object.Bytes < 2 || object.Bytes > 1048576 {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	return object, nil
}

func (p Publisher) verifyReadyDraftReplay(ctx context.Context, who auth.Identity, tenantID, documentID uuid.UUID, ref v2input.Ref, expectedBytes int64) error {
	reader, ok := p.Objects.(immutableVersionReader)
	if !ok {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	object, err := p.readyDraftObject(ctx, who, tenantID, documentID, ref, ref.SHA256)
	if err != nil {
		return err
	}
	if object.Bytes != expectedBytes {
		return objectstore.ErrIntegrity
	}
	stream, err := reader.Open(ctx, object)
	if err != nil {
		return err
	}
	if err := verifyExactDraftObjectStream(ctx, stream, object); err != nil {
		return err
	}
	current, err := p.readyDraftObject(ctx, who, tenantID, documentID, ref, ref.SHA256)
	if err != nil {
		return err
	}
	if current != object {
		return objectstore.ErrIntegrity
	}
	return nil
}

// Publish makes a recipe or base checkpoint immutable before its document
// pointer can be changed. documentID is stable across retries of one save.
func (p Publisher) Publish(ctx context.Context, who auth.Identity, tenantID,
	assetVersionID, documentID uuid.UUID, raw []byte) (v2input.Ref, error) {
	return p.publish(ctx, who, tenantID, assetVersionID, documentID, raw, false, false)
}

// PublishShotList publishes a validated session-scoped ShotList using the same
// immutable object protocol and cleanup lifecycle as recipe publication.
func (p Publisher) PublishShotList(ctx context.Context, who auth.Identity, tenantID,
	sessionID, documentID uuid.UUID, shots v2input.CaptureShotList) (v2input.Ref, error) {
	body, err := v2input.CompileCaptureShotList(shots)
	if err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return p.publish(ctx, who, tenantID, sessionID, documentID, body, true, false)
}

func (p Publisher) publish(ctx context.Context, who auth.Identity, tenantID,
	assetVersionID, documentID uuid.UUID, raw []byte, shotList, semanticIntent bool) (v2input.Ref, error) {
	reserveSQL := `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_draft_upload($1,$2,$3,$4,$5,$6,$7,$8)`
	completeSQL := `SELECT photo.complete_v2_draft_upload($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	if semanticIntent {
		reserveSQL = `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v27_semantic_intent_upload($1,$2,$3,$4,$5,$6,$7,$8)`
		completeSQL = `SELECT photo.complete_v27_semantic_intent_upload($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	}
	if shotList {
		reserveSQL = `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v24_shot_upload($1,$2,$3,$4,$5,$6,$7,$8)`
		completeSQL = `SELECT photo.complete_v24_shot_upload($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	}
	if p.DB == nil || p.Objects == nil || p.Bucket == "" {
		return v2input.Ref{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if tenantID == uuid.Nil || assetVersionID == uuid.Nil || documentID == uuid.Nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	body, err := canonical.Bytes(raw)
	if err != nil || len(body) < 2 || len(body) > 1048576 {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var r reservation
	var outcome string
	err = tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			return tx.QueryRow(ctx, reserveSQL,
				tenantID, assetVersionID, documentID, who.UserID, who.SessionID,
				digest, len(body), p.Bucket).Scan(&outcome, &r.ArtifactID, &r.Token, &r.Ready)
		})
	if err != nil {
		return v2input.Ref{}, err
	}
	switch outcome {
	case "quota":
		return v2input.Ref{}, domain.NewError("QUOTA_EXCEEDED", 409)
	case "conflict", "stale":
		return v2input.Ref{}, domain.NewError("REVISION_CONFLICT", 409)
	case "forbidden":
		return v2input.Ref{}, domain.NewError("FORBIDDEN", 403)
	case "created", "resumed", "replayed":
	default:
		return v2input.Ref{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if r.ArtifactID == uuid.Nil || r.Token == uuid.Nil {
		return v2input.Ref{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	ref := v2input.Ref{ArtifactID: r.ArtifactID, SHA256: digest}
	if r.Ready {
		if err := p.verifyReadyDraftReplay(ctx, who, tenantID, documentID, ref, int64(len(body))); err != nil {
			return v2input.Ref{}, err
		}
		var current reservation
		var replayOutcome string
		err := tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor,
			func(tx pgx.Tx, _ domain.Role) error {
				return tx.QueryRow(ctx, reserveSQL, tenantID, assetVersionID, documentID,
					who.UserID, who.SessionID, digest, len(body), p.Bucket).
					Scan(&replayOutcome, &current.ArtifactID, &current.Token, &current.Ready)
			})
		if err != nil {
			return v2input.Ref{}, err
		}
		if replayOutcome != "replayed" || !current.Ready || current.ArtifactID != r.ArtifactID || current.Token != r.Token {
			return v2input.Ref{}, domain.NewError("REVISION_CONFLICT", 409)
		}
		return ref, nil
	}
	key, err := objectstore.V2DraftKey(tenantID, r.ArtifactID)
	if err != nil {
		return v2input.Ref{}, err
	}
	obj, err := p.Objects.PutImmutable(ctx, key, bytes.NewReader(body), int64(len(body)),
		digest, "application/json")
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		obj, err = p.Objects.RecoverImmutable(ctx, key, int64(len(body)), digest, "application/json")
	}
	if err != nil {
		return v2input.Ref{}, err
	}
	if !validPublishedDraftObject(obj, key, digest, int64(len(body))) {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	var completed bool
	err = tenant.WithTenant(ctx, p.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			return tx.QueryRow(ctx, completeSQL,
				tenantID, assetVersionID, documentID, who.UserID, who.SessionID,
				r.ArtifactID, r.Token, digest, len(body), p.Bucket, obj.VersionID).Scan(&completed)
		})
	if err != nil {
		return v2input.Ref{}, err
	}
	if !completed {
		return v2input.Ref{}, domain.NewError("REVISION_CONFLICT", 409)
	}
	return ref, nil
}

// PublishSemanticIntent publishes strict user intent using the accepted-version
// reservation path. Catalog/mask/snapshot scope remains a caller obligation.
func (p Publisher) PublishSemanticIntent(ctx context.Context, who auth.Identity, tenantID, assetVersionID, documentID uuid.UUID, raw []byte) (v2input.Ref, error) {
	intent, err := v2input.DecodeSemanticEditIntent(raw)
	if err != nil || intent.Reference.AssetVersionID != assetVersionID {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return p.publish(ctx, who, tenantID, assetVersionID, documentID, raw, false, true)
}

// PublishSemanticTransferParams publishes server-compiled transfer parameters
// against the exact accepted target version, including an older pinned version.
// Qualification and both source closures must be verified by the preparer;
// publication alone does not authorize task admission.
func (p Publisher) PublishSemanticTransferParams(ctx context.Context, who auth.Identity, tenantID, assetVersionID, documentID uuid.UUID, raw []byte) (v2input.Ref, error) {
	params, err := v2input.DecodeSemanticTransferParams(raw)
	if err != nil || params.Target.AssetVersionID != assetVersionID {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return p.publish(ctx, who, tenantID, assetVersionID, documentID, raw, false, true)
}
