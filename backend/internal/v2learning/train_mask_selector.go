package v2learning

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type TrainingMaskRootSource interface {
	List(context.Context, TrainingStart, TrainingCandidate) ([]v2input.Ref, error)
}

type SQLTrainingMaskRoots struct{ DB *pgxpool.Pool } // photo_scheduler

func (s SQLTrainingMaskRoots) List(ctx context.Context, start TrainingStart,
	candidate TrainingCandidate) ([]v2input.Ref, error) {
	if s.DB == nil || start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.ActorID == uuid.Nil || start.AuthSessionID == uuid.Nil ||
		candidate.EventID == uuid.Nil {
		return nil, ErrInvalidTrainingDraft
	}
	rows, err := s.DB.Query(ctx, `SELECT artifact_id,sha256 FROM
	 photo.v23_training_mask_roots($1,$2,$3,$4,$5)`, start.TenantID,
		start.ProjectID, candidate.EventID, start.ActorID, start.AuthSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := make([]v2input.Ref, 0)
	for rows.Next() {
		var ref v2input.Ref
		if err := rows.Scan(&ref.ArtifactID, &ref.SHA256); err != nil {
			return nil, err
		}
		if !validManifestRef(ref) || len(refs) >= 1024 {
			return nil, ErrInvalidTrainingDraft
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}

type TrainingMaskRootReader interface {
	Read(context.Context, auth.Identity, uuid.UUID, v2input.Ref) ([]byte, error)
}

type ScopedTrainingMaskRootReader struct {
	Catalog v2draft.ArtifactCatalog
	Objects LearningEvaluateObjectReader
}

func (s ScopedTrainingMaskRootReader) Read(ctx context.Context,
	who auth.Identity, tenantID uuid.UUID, ref v2input.Ref) ([]byte, error) {
	if s.Catalog == nil || s.Objects == nil || tenantID == uuid.Nil ||
		!validManifestRef(ref) {
		return nil, ErrInvalidTrainingDraft
	}
	object, err := s.Catalog.Lookup(ctx, who, tenantID, v2draft.LayerRef{
		Path: "TrainingRenderCase.candidate", Contract: "Artifact", Ref: ref})
	if err != nil {
		return nil, err
	}
	return ReadFrozenRootDocument(ctx, s.Objects, object, ref)
}

type TrainingMaskLoader interface {
	Load(context.Context, auth.Identity, TrainingStart, TrainingCandidate,
		v2input.Ref, TrainingImage, bool) (TrainingMask, error)
}

type TrainingMaskSelector struct {
	Roots  TrainingMaskRootSource
	Reader TrainingMaskRootReader
	Masks  TrainingMaskLoader
}

// SelectPinned uses the explicit immutable recipe binding. It still proves
// that the root belongs to this correction event's registered closure before
// reading the document and canonical task-produced pixels.
func (s TrainingMaskSelector) SelectPinned(ctx context.Context,
	who auth.Identity, start TrainingStart, candidate TrainingCandidate,
	image TrainingImage, pinned v2input.Ref) (v2input.Ref, TrainingMask, error) {
	if s.Roots == nil || s.Reader == nil || s.Masks == nil ||
		image.PixelsCount < 1 || !validManifestRef(pinned) {
		return v2input.Ref{}, TrainingMask{}, ErrInvalidTrainingDraft
	}
	refs, err := s.Roots.List(ctx, start, candidate)
	if err != nil {
		return v2input.Ref{}, TrainingMask{}, err
	}
	found := false
	for _, ref := range refs {
		if ref == pinned {
			if found {
				return v2input.Ref{}, TrainingMask{}, ErrInvalidTrainingDraft
			}
			found = true
		}
	}
	if !found {
		return v2input.Ref{}, TrainingMask{}, ErrInvalidTrainingExample
	}
	body, err := s.Reader.Read(ctx, who, start.TenantID, pinned)
	if err != nil {
		return v2input.Ref{}, TrainingMask{}, err
	}
	parsed, err := review.ParseMaskRef(body)
	if err != nil || !parsed.Protected ||
		parsed.Canvas != image.Document.Canvas ||
		!bytes.Equal(parsed.TransformChain, image.Document.TransformChain) {
		return v2input.Ref{}, TrainingMask{}, ErrInvalidTrainingExample
	}
	mask, err := s.Masks.Load(ctx, who, start, candidate, pinned, image, true)
	if err != nil {
		return v2input.Ref{}, TrainingMask{}, err
	}
	return pinned, mask, nil
}

// SelectProtected admits one uniquely bound protected MaskRef. A missing or
// ambiguous protection excludes the event; no synthetic mask is substituted.
func (s TrainingMaskSelector) SelectProtected(ctx context.Context,
	who auth.Identity, start TrainingStart, candidate TrainingCandidate,
	image TrainingImage) (v2input.Ref, TrainingMask, error) {
	if s.Roots == nil || s.Reader == nil || s.Masks == nil ||
		image.PixelsCount < 1 {
		return v2input.Ref{}, TrainingMask{}, ErrInvalidTrainingDraft
	}
	refs, err := s.Roots.List(ctx, start, candidate)
	if err != nil {
		return v2input.Ref{}, TrainingMask{}, err
	}
	var selected v2input.Ref
	var mask TrainingMask
	seen := make(map[v2input.Ref]bool, len(refs))
	for _, ref := range refs {
		if !validManifestRef(ref) || seen[ref] {
			return v2input.Ref{}, TrainingMask{}, ErrInvalidTrainingDraft
		}
		seen[ref] = true
		body, err := s.Reader.Read(ctx, who, start.TenantID, ref)
		if err != nil {
			return v2input.Ref{}, TrainingMask{}, err
		}
		parsed, err := review.ParseMaskRef(body)
		if err != nil {
			continue
		}
		if !parsed.Protected || parsed.Canvas != image.Document.Canvas ||
			!bytes.Equal(parsed.TransformChain, image.Document.TransformChain) {
			continue
		}
		if selected.ArtifactID != uuid.Nil {
			return v2input.Ref{}, TrainingMask{}, ErrInvalidTrainingExample
		}
		mask, err = s.Masks.Load(ctx, who, start, candidate, ref, image, true)
		if err != nil {
			return v2input.Ref{}, TrainingMask{}, err
		}
		selected = ref
	}
	if selected.ArtifactID == uuid.Nil {
		return v2input.Ref{}, TrainingMask{}, ErrInvalidTrainingExample
	}
	return selected, mask, nil
}

var _ TrainingMaskRootSource = SQLTrainingMaskRoots{}
var _ TrainingMaskRootReader = ScopedTrainingMaskRootReader{}
var _ TrainingMaskLoader = TrainingMaskSource{}
