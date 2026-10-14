package v2draft

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type RestorationAdoptionVerifier struct {
	Material  RestorationAdoptionMaterialLoader
	Documents RestorationResultDocumentCatalog
	Arrays    RestorationArrayCatalog
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
}

type RestorationAdoptionMaterialLoader interface {
	Load(context.Context, auth.Identity, uuid.UUID, string, []byte) (v2portrait.RestorationDocuments, error)
}

// Verify replays live manual-origin input authority, reads the canonical
// worker result, and checks all physical pixels. It grants no review or Apply
// right; an atomic SQL adoption must consume the verified evidence afterward.
func (v RestorationAdoptionVerifier) Verify(ctx context.Context, who auth.Identity,
	scope RestorationArrayScope, operation string, paramsRaw []byte,
) (RestorationReviewCandidate, int64, error) {
	candidate, count, _, err := v.VerifyWithEvidence(ctx, who, scope, operation, paramsRaw)
	return candidate, count, err
}

func (v RestorationAdoptionVerifier) VerifyWithEvidence(ctx context.Context, who auth.Identity,
	scope RestorationArrayScope, operation string, paramsRaw []byte,
) (RestorationReviewCandidate, int64, RestorationVerificationEvidence, error) {
	var zero RestorationReviewCandidate
	if err := ctx.Err(); err != nil {
		return zero, 0, RestorationVerificationEvidence{}, err
	}
	if scope.Tenant == uuid.Nil || scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		v.Material == nil || v.Documents == nil || v.Arrays == nil || v.Objects == nil ||
		len(v.Bucket) < 3 || len(v.Bucket) > 63 {
		return zero, 0, RestorationVerificationEvidence{}, v2input.ErrInvalid
	}
	recorder := newRestorationLocatorRecorder(v.Documents, v.Arrays)
	material, err := v.Material.Load(ctx, who, scope.Tenant, operation, paramsRaw)
	if err != nil {
		return zero, 0, RestorationVerificationEvidence{}, err
	}
	docs, err := LoadRestorationResultDocuments(ctx, scope, recorder, v.Objects, paramsRaw)
	if err != nil {
		return zero, 0, RestorationVerificationEvidence{}, err
	}
	if !bytes.Equal(material.Source, docs.Source) {
		return zero, 0, RestorationVerificationEvidence{}, v2input.ErrExpired
	}
	root := v2input.Ref{ArtifactID: scope.Root, SHA256: scope.RootSHA}
	candidate, count, err := VerifyRestorationPublishedResult(ctx, scope, recorder,
		v.Objects, operation, paramsRaw, material.Target, material.Protected, root, docs)
	if err != nil {
		return zero, 0, RestorationVerificationEvidence{}, err
	}
	if err := ctx.Err(); err != nil {
		return zero, 0, RestorationVerificationEvidence{}, err
	}
	evidence, err := recorder.evidence(scope, v.Bucket, operation, candidate, count)
	if err != nil {
		return zero, 0, RestorationVerificationEvidence{}, err
	}
	return candidate, count, evidence, nil
}
