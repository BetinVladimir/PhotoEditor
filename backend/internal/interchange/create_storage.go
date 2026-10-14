package interchange

import (
	"bytes"
	"context"
 "crypto/sha256"
 "encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type CreateDocumentStore interface {
	ReserveCreate(context.Context, CreateIdentity, CreateDocument) (artifactjson.Reservation, error)
	CompleteCreate(context.Context, CreateIdentity, CreateDocument, artifactjson.Reservation, objectstore.Object) (v2input.Ref, error)
}
type CreateStoragePublisher struct {
	Tenant  uuid.UUID
	Store   CreateDocumentStore
	Objects artifactjson.Objects
}

type CreateDocument struct {Purpose string;Ref v2input.Ref;Body []byte}
func rawSHA(raw []byte)string{sum:=sha256.Sum256(raw);return hex.EncodeToString(sum[:])}
func validateCreateStorageDocument(doc CreateDocument)error{
 if doc.Purpose!="task_params"&&doc.Purpose!="v30_interchange_snapshot"{return objectstore.ErrInvalid}
 if doc.Ref.ArtifactID==uuid.Nil||len(doc.Body)<2||len(doc.Body)>65536||rawSHA(doc.Body)!=doc.Ref.SHA256{return objectstore.ErrInvalid}
 body,err:=canonical.Bytes(doc.Body);if err!=nil||!bytes.Equal(body,doc.Body)||body[0]!='{'{return objectstore.ErrInvalid};return nil
}

func (p CreateStoragePublisher) PublishCreateDocument(ctx context.Context, identity CreateIdentity, doc CreateDocument) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if p.Tenant == uuid.Nil || identity.Run == uuid.Nil || p.Store == nil || p.Objects == nil {
		return v2input.Ref{}, objectstore.ErrInvalid
	}
	if err := validateCreateStorageDocument(doc); err != nil {
		return v2input.Ref{}, err
	}
	doc.Body = append([]byte(nil), doc.Body...)
	expected := doc.Ref
	reservation, err := p.Store.ReserveCreate(ctx, identity, doc)
	if err != nil {
		return v2input.Ref{}, err
	}
	if reservation.ArtifactID != expected.ArtifactID {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	if reservation.Ready != nil {
		if reservation.Ready.ID != expected.ArtifactID || reservation.Ready.SHA256 != expected.SHA256 {
			return v2input.Ref{}, objectstore.ErrIntegrity
		}
		return expected, ctx.Err()
	}
	if reservation.Token == uuid.Nil {
		return v2input.Ref{}, objectstore.ErrInvalid
	}
	key, err := objectstore.DocumentKey(p.Tenant, identity.Run, expected.ArtifactID)
	if err != nil {
		return v2input.Ref{}, err
	}
	size := int64(len(doc.Body))
	object, err := p.Objects.PutImmutable(ctx, key, bytes.NewReader(doc.Body), size, expected.SHA256, "application/json")
	if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
		if ctx.Err() != nil {
			return v2input.Ref{}, ctx.Err()
		}
		object, err = p.Objects.RecoverImmutable(ctx, key, size, expected.SHA256, "application/json")
	}
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if object.Key != key || object.SHA256 != expected.SHA256 || object.Bytes != size || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	result, err := p.Store.CompleteCreate(ctx, identity, doc, reservation, object)
	if err != nil {
		return v2input.Ref{}, err
	}
	if result != expected {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	return result, nil
}

type SQLCreateDocumentStore struct {
	DB     *pgxpool.Pool
 Tenant uuid.UUID
 Key string
	Intent CreateIntent
	Who    auth.Identity
	Bucket string
}

func(s SQLCreateDocumentStore)valid(identity CreateIdentity,doc CreateDocument)error{
 intent,err:=FreezeCreateIntent(s.Tenant,s.Who,s.Intent.Request,s.Key)
 if err!=nil||s.DB==nil||identity!=intent.Identity||identity!=s.Intent.Identity||intent.RequestSHA!=s.Intent.RequestSHA||len(s.Bucket)<3||len(s.Bucket)>63{return objectstore.ErrInvalid}
 return validateCreateStorageDocument(doc)
}
func (s SQLCreateDocumentStore) ReserveCreate(ctx context.Context, identity CreateIdentity, doc CreateDocument) (artifactjson.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Reservation{}, err
	}
	if err := s.valid(identity, doc); err != nil {
		return artifactjson.Reservation{}, err
	}
	var result artifactjson.Reservation
	err := tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outcome string
		var id, token *uuid.UUID
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_interchange_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tenant, s.Who.UserID, s.Who.SessionID, s.Key, s.Intent.RequestSHA, doc.Ref.ArtifactID, doc.Purpose, string(doc.Body), s.Bucket).Scan(&outcome, &id, &token, &ready); err != nil {
			return err
		}
		switch outcome {
		case "quota":
			return artifactjson.ErrQuota
		case "conflict":
			return artifactjson.ErrConflict
		case "created", "resumed", "replayed":
		default:
			return artifactjson.ErrStale
		}
		if id == nil || *id != doc.Ref.ArtifactID || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
			return objectstore.ErrIntegrity
		}
		result = artifactjson.Reservation{ArtifactID: *id, Token: *token}
		if ready {
			result.Ready = &artifactjson.Ref{ID: *id, SHA256: doc.Ref.SHA256}
		}
		return nil
	})
	return result, err
}
func (s SQLCreateDocumentStore) CompleteCreate(ctx context.Context, identity CreateIdentity, doc CreateDocument, reservation artifactjson.Reservation, object objectstore.Object) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if err := s.valid(identity, doc); err != nil {
		return v2input.Ref{}, err
	}
	key, err := objectstore.DocumentKey(s.Tenant, identity.Run, doc.Ref.ArtifactID)
	if err != nil || reservation.ArtifactID != doc.Ref.ArtifactID || reservation.Token == uuid.Nil || object.Key != key || object.SHA256 != doc.Ref.SHA256 || object.Bytes != int64(len(doc.Body)) || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	err = tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v30_interchange_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, s.Tenant, s.Who.UserID, s.Who.SessionID, s.Key, s.Intent.RequestSHA, doc.Ref.ArtifactID, doc.Purpose, string(doc.Body), s.Bucket, reservation.Token, object.VersionID).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
	if err != nil {
		return v2input.Ref{}, err
	}
	return doc.Ref, nil
}
