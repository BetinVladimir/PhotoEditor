package interchange

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type ImportPersonOriginReceiptSource interface {
	LookupPersonOriginReceipt(context.Context, []byte, v2input.Ref) (objectstore.Object, error)
}

type ImportPersonOriginReceiptWriter interface {
	RecordPersonOriginReceipt(context.Context, []byte, v2input.Ref, []byte) error
}

// Scope must read the staged recipe, Override, original MaskRef and source
// image, then prove the exact authored person_subject edge independently of
// the receipt claim and manifest declaration.
type ImportPersonOriginScope interface {
	QualifyImportedPersonOriginScope(context.Context, []byte, PortablePersonOriginExpectation) error
}

type ImportPersonOriginScopeDiscoverer interface {
	ImportPersonOriginScope
	DiscoverEntry(context.Context, []byte, uuid.UUID, string) ([]PortablePersonOriginExpectation, error)
}

type SQLImportPersonOriginReceiptSource struct {
	DB               *pgxpool.Pool
	Tenant, Workflow uuid.UUID
	Bucket           string
}

func (s SQLImportPersonOriginReceiptSource) LookupPersonOriginReceipt(ctx context.Context, parsed []byte, ref v2input.Ref) (objectstore.Object, error) {
	return (SQLImportOpaqueCatalog{DB: s.DB, Tenant: s.Tenant, Workflow: s.Workflow,
		Parsed: append([]byte(nil), parsed...), Bucket: s.Bucket}).LookupImportDocument(ctx,
		v2input.Entry{Ref: ref, Contract: "Artifact"})
}

type SQLImportPersonOriginReceiptWriter struct {
	DB                       *pgxpool.Pool
	Tenant, Workflow, Target uuid.UUID
}

func (w SQLImportPersonOriginReceiptWriter) RecordPersonOriginReceipt(ctx context.Context, parsed []byte, ref v2input.Ref, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w.DB == nil || w.Tenant == uuid.Nil || w.Workflow == uuid.Nil || w.Target == uuid.Nil ||
		!validImportRef(ref) || rawSHA(raw) != ref.SHA256 || len(raw) < 2 || len(raw) > 16384 ||
		len(parsed) < 2 || len(parsed) > 8<<20 {
		return v2input.ErrInvalid
	}
	return database.TenantTransaction(ctx, w.DB, w.Tenant, func(tx pgx.Tx) error {
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.record_v30_import_person_origin($1,$2,$3,$4,$5,$6)`,
			w.Tenant, w.Workflow, w.Target, string(parsed), ref.ArtifactID, string(raw)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
}

type ImportPersonOriginRecordService struct {
	Parsed  ImportReviewParsedSource
	Source  ImportPersonOriginReceiptSource
	Objects ImportDocumentObjects
	Writer  ImportPersonOriginReceiptWriter
	Scope   ImportPersonOriginScope
	Trusted map[PortablePersonOriginKey]ed25519.PublicKey
}

// RecordEntry is the scheduler entry boundary. Its scopes come from physical
// staged recipes through DiscoverEntry and are rechecked again by Record.
func (s ImportPersonOriginRecordService) RecordEntry(ctx context.Context, entryID uuid.UUID,
	modelLockSHA string) ([]VerifiedImportPersonOrigin, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	discoverer, ok := s.Scope.(ImportPersonOriginScopeDiscoverer)
	if !ok || entryID == uuid.Nil || s.Parsed == nil || s.Objects == nil ||
		!portablePersonOriginSHA.MatchString(modelLockSHA) {
		return nil, v2input.ErrInvalid
	}
	parsed, err := ReadImportReviewParsed(ctx, s.Parsed, s.Objects)
	if err != nil {
		return nil, err
	}
	var document importParsedDocument
	if err := decodeImportDocument(parsed, &document); err != nil {
		return nil, err
	}
	declared := 0
	for _, origin := range document.Manifest.PersonOrigins {
		if origin.EntryID == entryID {
			declared++
		}
	}
	if declared == 0 {
		return nil, v2input.ErrInvalid
	}
	scopes, err := discoverer.DiscoverEntry(ctx, parsed, entryID, modelLockSHA)
	if err != nil {
		return nil, err
	}
	if len(scopes) != declared || len(scopes) > 5000 {
		return nil, v2input.ErrInvalid
	}
	seen := map[[3]uuid.UUID]bool{}
	for _, scope := range scopes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := [3]uuid.UUID{scope.EntryID, scope.Override.ArtifactID, scope.MaskRef.ArtifactID}
		if scope.EntryID != entryID || scope.ModelLockSHA != modelLockSHA || seen[key] {
			return nil, v2input.ErrInvalid
		}
		seen[key] = true
		if _, err := selectedPersonOriginReceiptRef(parsed, scope); err != nil {
			return nil, err
		}
	}
	results := make([]VerifiedImportPersonOrigin, 0, len(scopes))
	for _, scope := range scopes {
		verified, err := s.Record(ctx, scope)
		if err != nil {
			return nil, err
		}
		results = append(results, verified)
	}
	return results, ctx.Err()
}

func selectedPersonOriginReceiptRef(parsed []byte, expected PortablePersonOriginExpectation) (v2input.Ref, error) {
	var document importParsedDocument
	if err := decodeImportDocument(parsed, &document); err != nil {
		return v2input.Ref{}, err
	}
	var ref v2input.Ref
	matches := 0
	for _, origin := range document.Manifest.PersonOrigins {
		if origin.EntryID != expected.EntryID || origin.Recipe != expected.Recipe ||
			origin.Override != expected.Override || origin.MaskRef != expected.MaskRef || origin.MaskPixels != expected.MaskPixels {
			continue
		}
		for _, file := range document.Files {
			if file.Path == origin.Path && file.Contract == "PersonOriginReceipt" && file.Content.SHA256 == origin.SHA {
				ref = file.Content
				matches++
			}
		}
	}
	if matches != 1 || !validImportRef(ref) {
		return v2input.Ref{}, errPortablePersonOrigin
	}
	return ref, nil
}

func (s ImportPersonOriginRecordService) Record(ctx context.Context, expected PortablePersonOriginExpectation) (VerifiedImportPersonOrigin, error) {
	var zero VerifiedImportPersonOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Parsed == nil || s.Source == nil || s.Objects == nil || s.Writer == nil || s.Scope == nil || len(s.Trusted) == 0 {
		return zero, v2input.ErrInvalid
	}
	parsed, err := ReadImportReviewParsed(ctx, s.Parsed, s.Objects)
	if err != nil {
		return zero, err
	}
	if err := s.Scope.QualifyImportedPersonOriginScope(ctx, append([]byte(nil), parsed...), expected); err != nil {
		return zero, err
	}
	ref, err := selectedPersonOriginReceiptRef(parsed, expected)
	if err != nil {
		return zero, err
	}
	object, err := s.Source.LookupPersonOriginReceipt(ctx, parsed, ref)
	if err != nil {
		return zero, err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != ref.SHA256 ||
		object.ContentType != "application/octet-stream" || object.Bytes < 2 || object.Bytes > 16384 {
		return zero, v2input.ErrInvalid
	}
	stream, err := s.Objects.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return zero, err
	}
	if stream == nil {
		return zero, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx: ctx, reader: stream}, object.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return zero, readErr
	}
	if closeErr != nil {
		return zero, closeErr
	}
	if int64(len(raw)) != object.Bytes || rawSHA(raw) != ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	verified, err := VerifyImportedPersonOriginFile(ctx, parsed, raw, expected, s.Trusted)
	if err != nil || verified.ReceiptFile != ref {
		return zero, errPortablePersonOrigin
	}
	currentParsed, err := ReadImportReviewParsed(ctx, s.Parsed, s.Objects)
	if err != nil || !bytes.Equal(currentParsed, parsed) {
		return zero, v2input.ErrInvalid
	}
	currentObject, err := s.Source.LookupPersonOriginReceipt(ctx, parsed, ref)
	if err != nil || currentObject != object {
		return zero, v2input.ErrInvalid
	}
	if err := s.Scope.QualifyImportedPersonOriginScope(ctx, append([]byte(nil), parsed...), expected); err != nil {
		return zero, err
	}
	if err := s.Writer.RecordPersonOriginReceipt(ctx, append([]byte(nil), parsed...), ref, append([]byte(nil), raw...)); err != nil {
		return zero, err
	}
	return verified, ctx.Err()
}
