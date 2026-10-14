package provenance

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type SigningRenderParentReader interface {
	ReadSigningRenderParents(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask) ([]SigningRenderParent, error)
}
type SigningRenderQualificationReader interface {
	ReadSigningRenderQualification(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2draft.FrozenRenderRecipe) (SigningRenderQualification, error)
}

type BoundSigningRenderEvidenceSource struct {
	Qualification SigningRenderQualificationReader
	Adoption      SigningRenderAdoptionReader
	Documents     SigningTaskDocumentReader
	Counter       SigningRenderMaskCounter
	Parents       SigningRenderParentReader
	Reports       SigningReportCatalog
	Objects       SigningObjects
}

// This receipt deliberately grants no SigningAction until model attribution and
// applicable human review are independently checked by the final resolver.
type SigningRenderEvidence struct {
	taskDigest      string
	pixels          SigningRenderPixelEvidence
	report          v2draft.ImageReportDocument
	parents         []SigningRenderParent
	inheritedReview bool
	qualification   SigningRenderQualification
	dependencies    []v2draft.LayerRef
}

func (s BoundSigningRenderEvidenceSource) ReadSigningRenderEvidence(ctx context.Context, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (SigningRenderEvidence, error) {
	var zero SigningRenderEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Parents == nil || s.Reports == nil || s.Objects == nil || s.Qualification == nil {
		return zero, ErrInvalid
	}
	pixels, err := ReadSigningRenderPixelEvidence(ctx, s.Adoption, s.Documents, s.Counter, who, ready, task)
	if err != nil {
		return zero, err
	}
	recipeBytes, err := canonical.Marshal(pixels.recipe)
	if err != nil {
		return zero, err
	}
	dependencies, err := v2draft.FrozenRenderRecipeDependencies(recipeBytes, task.Params)
	if err != nil {
		return zero, ErrInvalid
	}
	qualification, err := s.Qualification.ReadSigningRenderQualification(ctx, who, ready, task, pixels.recipe)
	if err != nil {
		return zero, err
	}
	if !qualification.valid() || qualification.ModelLockSHA != pixels.recipe.Scalars.ModelLockSHA {
		return zero, ErrInvalid
	}
	parents, err := s.Parents.ReadSigningRenderParents(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	if parents == nil || len(parents) > 33 {
		return zero, ErrInvalid
	}
	parents, frozenParents, err := ownSigningRenderParents(parents)
	if err != nil {
		return zero, err
	}
	roots := []v2input.Ref{}
	bodies := map[v2input.Ref][]byte{}
	for _, parent := range parents {
		declared, err := canonicalTaskOutputRefs(parent.Producer.Outputs)
		if err != nil || len(declared) != 1 || declared[0] != parent.Root {
			return zero, ErrInvalid
		}
		raw, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, parent.Producer, parent.Root, 1<<20)
		if err != nil {
			return zero, err
		}
		if !pinned(raw, parent.Root, 1<<20) {
			return zero, ErrInvalid
		}
		owned := append([]byte(nil), raw...)
		result, err := v2draft.ParseImageResult(owned, parent.Root)
		if err != nil {
			return zero, ErrInvalid
		}
		bodies[parent.Root] = owned
		roots = append(roots, result.Report)
	}
	reader := BoundSigningImageReportReader{Catalog: s.Reports, Objects: s.Objects, Who: who, Ready: ready}
	inherited, err := CollectSigningRenderParentReports(ctx, reader, roots, pixels.recipe.Scalars.Source)
	if err != nil {
		return zero, err
	}
	report, err := ReadSigningRenderReport(ctx, s.Documents, who, ready, task, pixels.recipe.Scalars.Source, pixels.result.Report, inherited.Reports)
	if err != nil {
		return zero, err
	}
	if inherited.ReviewRequired && report.Status != "review" {
		return zero, ErrInvalid
	}
	for _, parent := range parents {
		raw, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, parent.Producer, parent.Root, 1<<20)
		if err != nil {
			return zero, err
		}
		if !bytes.Equal(raw, bodies[parent.Root]) {
			return zero, ErrInvalid
		}
	}
	current, err := s.Parents.ReadSigningRenderParents(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	currentRaw, err := canonical.Marshal(current)
	if err != nil || !bytes.Equal(currentRaw, frozenParents) {
		return zero, ErrInvalid
	}
	root, err := s.Adoption.ReadSigningAdoptedRenderRoot(ctx, who, ready, task)
	if err != nil {
		return zero, err
	}
	if root != pixels.root {
		return zero, ErrInvalid
	}
	currentQualification, err := s.Qualification.ReadSigningRenderQualification(ctx, who, ready, task, pixels.recipe)
	if err != nil {
		return zero, err
	}
	if currentQualification != qualification {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	taskDigest, err := canonical.Hash(task)
	if err != nil {
		return zero, err
	}
	return SigningRenderEvidence{taskDigest: taskDigest, pixels: pixels, report: report, parents: parents, inheritedReview: inherited.ReviewRequired, qualification: qualification, dependencies: dependencies}, nil
}

var _ SigningRenderParentReader = SQLSigningRenderParentReader{}

var _ SigningRenderQualificationReader = SQLSigningRenderQualificationReader{}

// Parent catalog adapters may reuse nested JSON buffers. Snapshot before any
// subsequent adapter call, including physical document reads, can mutate them.
func ownSigningRenderParents(parents []SigningRenderParent) ([]SigningRenderParent, []byte, error) {
	if parents == nil || len(parents) > 33 {
		return nil, nil, ErrInvalid
	}
	raw, err := canonical.Marshal(parents)
	if err != nil || len(raw) > 64<<20 {
		return nil, nil, ErrInvalid
	}
	var owned []SigningRenderParent
	if json.Unmarshal(raw, &owned) != nil || owned == nil {
		return nil, nil, ErrInvalid
	}
	return owned, raw, nil
}
