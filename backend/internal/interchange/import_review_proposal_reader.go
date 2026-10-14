package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ImportReviewProposalSource interface {
	ResolveImportReviewProposal(context.Context) (v2input.Ref, v2input.Ref, objectstore.Object, error)
}

type SQLImportReviewProposalSource struct {
	DB               *pgxpool.Pool
	Tenant, Workflow uuid.UUID
	Who              auth.Identity
	Entry, Target    uuid.UUID
	Kind             string
	Proposal         uuid.UUID
	Parsed           []byte
	Bucket           string
}

func (s SQLImportReviewProposalSource) ResolveImportReviewProposal(ctx context.Context) (v2input.Ref, v2input.Ref, objectstore.Object, error) {
	var proposal, base v2input.Ref
	var object objectstore.Object
	if err := ctx.Err(); err != nil {
		return proposal, base, object, err
	}
	if s.DB == nil || s.Tenant == uuid.Nil || s.Workflow == uuid.Nil || s.Entry == uuid.Nil || s.Target == uuid.Nil || s.Proposal == uuid.Nil ||
		s.Who.UserID == uuid.Nil || s.Who.SessionID == uuid.Nil || (s.Kind != "defects" && s.Kind != "dust") ||
		len(s.Parsed) < 2 || len(s.Parsed) > 8<<20 || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return proposal, base, object, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT proposal_sha,bucket,object_key,object_version,bytes,media_type,base_id,base_sha
 FROM photo.v30_import_review_proposal_object($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			s.Tenant, s.Who.UserID, s.Who.SessionID, s.Workflow, s.Entry, s.Target, s.Kind, s.Proposal, string(s.Parsed)).Scan(
			&proposal.SHA256, &bucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType,
			&base.ArtifactID, &base.SHA256)
		if err != nil {
			return err
		}
		proposal.ArtifactID = s.Proposal
		object.SHA256 = proposal.SHA256
		if bucket != s.Bucket || !validImportRef(proposal) || !validImportRef(base) || object.Key == "" ||
			object.VersionID == "" || object.VersionID == "null" || object.Bytes < 2 || object.Bytes > 1<<20 ||
			object.ContentType != "application/json" {
			return objectstore.ErrIntegrity
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return v2input.Ref{}, v2input.Ref{}, objectstore.Object{}, artifactjson.ErrStale
	}
	if err != nil {
		return v2input.Ref{}, v2input.Ref{}, objectstore.Object{}, err
	}
	return proposal, base, object, ctx.Err()
}

// The second SQL read fences proposal replacement, revocation of the source
// scope, and object-version changes while bytes were read outside the TX.
func ReadImportReviewProposal(ctx context.Context, source ImportReviewProposalSource, objects ImportDocumentObjects, kind string) ([]byte, v2input.Ref, v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, v2input.Ref{}, v2input.Ref{}, err
	}
	if source == nil || objects == nil || (kind != "defects" && kind != "dust") {
		return nil, v2input.Ref{}, v2input.Ref{}, v2input.ErrInvalid
	}
	proposal, base, object, err := source.ResolveImportReviewProposal(ctx)
	if err != nil {
		return nil, v2input.Ref{}, v2input.Ref{}, err
	}
	if !validImportRef(proposal) || !validImportRef(base) || object.SHA256 != proposal.SHA256 || object.Bytes < 2 || object.Bytes > 1<<20 {
		return nil, v2input.Ref{}, v2input.Ref{}, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, v2input.Ref{}, v2input.Ref{}, err
	}
	if reader == nil {
		return nil, v2input.Ref{}, v2input.Ref{}, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx, reader}, object.Bytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, v2input.Ref{}, v2input.Ref{}, readErr
	}
	if closeErr != nil {
		return nil, v2input.Ref{}, v2input.Ref{}, closeErr
	}
	if int64(len(raw)) != object.Bytes || rawSHA(raw) != proposal.SHA256 {
		return nil, v2input.Ref{}, v2input.Ref{}, objectstore.ErrIntegrity
	}
	if kind == "defects" {
		if _, err := review.ParseDefectProposalStructure(ctx, raw, review.ArtifactRef{ID: proposal.ArtifactID, SHA256: proposal.SHA256}); err != nil {
			return nil, v2input.Ref{}, v2input.Ref{}, err
		}
		var document struct {
			Image v2input.Ref `json:"image"`
		}
		if json.Unmarshal(raw, &document) != nil || document.Image != base {
			return nil, v2input.Ref{}, v2input.Ref{}, v2input.ErrInvalid
		}
	} else {
		if _, err := review.ReadDustProposal(ctx, bytes.NewReader(raw), review.ArtifactRef{ID: proposal.ArtifactID, SHA256: proposal.SHA256}, review.ArtifactRef{ID: base.ArtifactID, SHA256: base.SHA256}); err != nil {
			return nil, v2input.Ref{}, v2input.Ref{}, err
		}
	}
	currentProposal, currentBase, currentObject, err := source.ResolveImportReviewProposal(ctx)
	if err != nil {
		return nil, v2input.Ref{}, v2input.Ref{}, err
	}
	if currentProposal != proposal || currentBase != base || currentObject != object {
		return nil, v2input.Ref{}, v2input.Ref{}, artifactjson.ErrStale
	}
	return raw, proposal, base, ctx.Err()
}
