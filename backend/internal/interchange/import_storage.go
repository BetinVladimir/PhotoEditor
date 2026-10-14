package interchange

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/canonical"
	
	"photoeditor/backend/internal/objectstore"
	
	"photoeditor/backend/internal/v2input"
)

type ImportDocumentStore interface {
	ReserveImport(context.Context, ImportDocumentIdentity, ImportDocument) (artifactjson.Reservation, error)
	CompleteImport(context.Context, ImportDocumentIdentity, ImportDocument, artifactjson.Reservation, objectstore.Object) (v2input.Ref, error)
}
type ImportStoragePublisher struct {
	Tenant  uuid.UUID
	Store   ImportDocumentStore
	Objects artifactjson.Objects
}

type ImportDocumentIdentity struct {Run,Workflow uuid.UUID}
type ImportDocument struct {Purpose string;Ref v2input.Ref;Body []byte}
func validateImportStorageDocument(identity ImportDocumentIdentity,doc ImportDocument)error{
 if identity.Run==uuid.Nil||identity.Workflow==uuid.Nil{return objectstore.ErrInvalid}
 task:=uuid.NewSHA1(identity.Workflow,[]byte("v2.interchange.import:0"))
 name:="interchange:plan:1";limit:=8<<20
 if doc.Purpose=="task_params"{name="interchange:params:1";limit=65536}else if doc.Purpose=="v30_interchange_import_evidence"{name="interchange:evidence:1"}else if doc.Purpose!="v30_interchange_import_plan"{return objectstore.ErrInvalid}
 if doc.Ref.ArtifactID!=uuid.NewSHA1(task,[]byte(name))||len(doc.Body)<2||len(doc.Body)>limit||rawSHA(doc.Body)!=doc.Ref.SHA256{return objectstore.ErrInvalid}
 body,err:=canonical.Bytes(doc.Body);if err!=nil||!bytes.Equal(body,doc.Body)||body[0]!='{'{return objectstore.ErrInvalid};return nil
}

func (p ImportStoragePublisher) PublishImportDocument(ctx context.Context, identity ImportDocumentIdentity, doc ImportDocument) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if p.Tenant == uuid.Nil || identity.Run == uuid.Nil || p.Store == nil || p.Objects == nil {
		return v2input.Ref{}, objectstore.ErrInvalid
	}
	if err := validateImportStorageDocument(identity,doc); err != nil {
		return v2input.Ref{}, err
	}
	doc.Body = append([]byte(nil), doc.Body...)
	expected := doc.Ref
	reservation, err := p.Store.ReserveImport(ctx, identity, doc)
	if err != nil {
		return v2input.Ref{}, err
	}
	if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
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
	result, err := p.Store.CompleteImport(ctx, identity, doc, reservation, object)
	if err != nil {
		return v2input.Ref{}, err
	}
	if result != expected {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	return result, nil
}

type SQLImportDocumentStore struct {
 DB *pgxpool.Pool
 Tenant uuid.UUID
 Identity ImportDocumentIdentity
 Parsed []byte
 Bucket string
}
func(s SQLImportDocumentStore)valid(identity ImportDocumentIdentity,doc ImportDocument)error{
 if s.DB==nil||s.Tenant==uuid.Nil||identity!=s.Identity||len(s.Bucket)<3||len(s.Bucket)>63||len(s.Parsed)<2||len(s.Parsed)>8<<20{return objectstore.ErrInvalid}
 normalized,err:=canonical.Bytes(s.Parsed);if err!=nil||!bytes.Equal(normalized,s.Parsed){return objectstore.ErrInvalid}
 return validateImportStorageDocument(identity,doc)
}
func(s SQLImportDocumentStore)checkRun(ctx context.Context,tx pgx.Tx,identity ImportDocumentIdentity)error{
 var run uuid.UUID
 if err:=tx.QueryRow(ctx,`SELECT run_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2 AND workflow_type='interchange'`,s.Tenant,identity.Workflow).Scan(&run);err!=nil{return err}
 if run!=identity.Run{return objectstore.ErrIntegrity};return nil
}
func (s SQLImportDocumentStore) ReserveImport(ctx context.Context, identity ImportDocumentIdentity, doc ImportDocument) (artifactjson.Reservation, error) {
	if err := ctx.Err(); err != nil {
		return artifactjson.Reservation{}, err
	}
	if err := s.valid(identity, doc); err != nil {
		return artifactjson.Reservation{}, err
	}
	var result artifactjson.Reservation
	err := database.TenantTransaction(ctx, s.DB, s.Tenant, func(tx pgx.Tx) error {
        if err:=s.checkRun(ctx,tx,identity);err!=nil{return err}
		var outcome string
		var id, token *uuid.UUID
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_interchange_import_document($1,$2,$3,$4,$5,$6,$7)`, s.Tenant, identity.Workflow, string(s.Parsed), doc.Ref.ArtifactID, doc.Purpose, string(doc.Body), s.Bucket).Scan(&outcome, &id, &token, &ready); err != nil {
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
func (s SQLImportDocumentStore) CompleteImport(ctx context.Context, identity ImportDocumentIdentity, doc ImportDocument, reservation artifactjson.Reservation, object objectstore.Object) (v2input.Ref, error) {
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
	err = database.TenantTransaction(ctx, s.DB, s.Tenant, func(tx pgx.Tx) error {
        if err:=s.checkRun(ctx,tx,identity);err!=nil{return err}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v30_interchange_import_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tenant, identity.Workflow, string(s.Parsed), doc.Ref.ArtifactID, doc.Purpose, string(doc.Body), s.Bucket, reservation.Token, object.VersionID).Scan(&accepted); err != nil {
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
