package v2learning

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type TrainingEditingDocuments struct {
	BaseBody, ApprovedBody             []byte
	BaseDocumentID, ApprovedDocumentID uuid.UUID
	Binding                            v2input.SessionVersionBinding
	BaseStyle                          v2input.Ref
}

// SQLTrainingDocumentSource reads the two EditingRecipeV3 documents named by
// an authorized candidate inventory. ResolveRecipe and its dependency checks
// must run later; these documents are not FrozenRecipe artifacts.
type SQLTrainingDocumentSource struct {
	DB        *pgxpool.Pool
	Objects   LearningEvaluateObjectReader
	Bucket    string
	ProjectID uuid.UUID
}

func (s SQLTrainingDocumentSource) Load(ctx context.Context,
	start TrainingStart, candidate TrainingCandidate) (
	TrainingEditingDocuments, error) {
	var empty TrainingEditingDocuments
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		s.ProjectID == uuid.Nil ||
		start.TenantID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil || start.ProjectID != s.ProjectID ||
		candidate.EventID == uuid.Nil ||
		candidate.BaseDocumentID == uuid.Nil ||
		candidate.ApprovedDocumentID == uuid.Nil ||
		candidate.BaseDocumentID == candidate.ApprovedDocumentID ||
		candidate.AssetVersionID == uuid.Nil ||
		!candidate.ConsentLive || !candidate.SourceReady ||
		!candidate.BaseReady || !candidate.ApprovedReady ||
		candidate.ApprovalID == nil || *candidate.ApprovalID == uuid.Nil {
		return empty, ErrInvalidTrainingDraft
	}
	load := func(side string, documentID, id uuid.UUID, sha string) ([]byte, uuid.UUID, int64, error) {
		ref := v2input.Ref{ArtifactID: id, SHA256: sha}
		if !validManifestRef(ref) {
			return nil, uuid.Nil, 0, ErrInvalidTrainingDraft
		}
		var object objectstore.Object
		var foundDocumentID, foundArtifactID uuid.UUID
		var foundSHA string
		var bucket string
		var streamID uuid.UUID
		var versionNo int64
		var approvalID *uuid.UUID
		err := s.DB.QueryRow(ctx, `SELECT document_id,artifact_id,sha256,
		 bytes,bucket,object_key,object_version,media_type,stream_id,
		 version_no,approval_id FROM photo.v23_training_document_source($1,$2,$3,$4,$5,$6)`,
			start.TenantID, s.ProjectID, candidate.EventID, side,
			start.ActorID, start.AuthSessionID).Scan(&foundDocumentID,
			&foundArtifactID, &foundSHA, &object.Bytes, &bucket,
			&object.Key, &object.VersionID, &object.ContentType,
			&streamID, &versionNo, &approvalID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, uuid.Nil, 0, ErrTrainingDraftConflict
		}
		if err != nil {
			return nil, uuid.Nil, 0, err
		}
		if foundDocumentID != documentID || foundArtifactID != id ||
			foundSHA != sha || bucket != s.Bucket {
			return nil, uuid.Nil, 0, ErrInvalidTrainingDraft
		}
		if side == "approved" && (approvalID == nil ||
			*approvalID != *candidate.ApprovalID) {
			return nil, uuid.Nil, 0, ErrTrainingDraftConflict
		}
		object.SHA256 = sha
		body, err := ReadFrozenRootDocument(ctx, s.Objects, object, ref)
		return body, streamID, versionNo, err
	}
	base, baseStream, baseVersion, err := load("base", candidate.BaseDocumentID,
		candidate.BaseArtifactID, candidate.BaseSHA)
	if err != nil {
		return empty, err
	}
	approved, approvedStream, approvedVersion, err := load("approved", candidate.ApprovedDocumentID,
		candidate.ApprovedArtifactID,
		candidate.ApprovedSHA)
	if err != nil {
		return empty, err
	}
	if baseStream != approvedStream || baseVersion >= approvedVersion {
		return empty, ErrTrainingDraftConflict
	}
	binding, style, err := ValidateTrainingEditingPair(base, approved,
		candidate.AssetVersionID)
	if err != nil {
		return empty, err
	}
	return TrainingEditingDocuments{BaseBody: base,
		BaseDocumentID:     candidate.BaseDocumentID,
		ApprovedDocumentID: candidate.ApprovedDocumentID,
		ApprovedBody:       approved, Binding: binding, BaseStyle: style}, nil
}

// ValidateTrainingEditingPair rejects mixed sources before either recipe is
// resolved to its pinned scalar FrozenRecipe. LayerStack eligibility is checked
// by the resolver after reading the referenced layer contract.
func ValidateTrainingEditingPair(base, approved []byte,
	assetVersionID uuid.UUID) (v2input.SessionVersionBinding,
	v2input.Ref, error) {
	var empty v2input.SessionVersionBinding
	parse := func(body []byte) (v2input.SessionVersionBinding,
		v2input.Ref, *v2input.Ref, error) {
		var descriptor struct {
			Source        v2input.SessionVersionBinding `json:"source"`
			BaseStyle     v2input.Ref                   `json:"base_style"`
			ProtectedMask *v2input.Ref                  `json:"protected_mask"`
		}
		if json.Unmarshal(body, &descriptor) != nil ||
			v2input.ValidateEditingRecipeV3(body, descriptor.Source) != nil ||
			descriptor.Source.AssetVersionID != assetVersionID ||
			!validManifestRef(descriptor.BaseStyle) {
			return empty, v2input.Ref{}, nil, ErrInvalidTrainingDraft
		}
		return descriptor.Source, descriptor.BaseStyle, descriptor.ProtectedMask, nil
	}
	baseBinding, baseStyle, baseMask, err := parse(base)
	if err != nil {
		return empty, v2input.Ref{}, err
	}
	approvedBinding, approvedStyle, approvedMask, err := parse(approved)
	if err != nil || baseBinding != approvedBinding ||
		baseStyle != approvedStyle ||
		(baseMask == nil) != (approvedMask == nil) ||
		(baseMask != nil && *baseMask != *approvedMask) {
		return empty, v2input.Ref{}, ErrInvalidTrainingDraft
	}
	return baseBinding, baseStyle, nil
}
