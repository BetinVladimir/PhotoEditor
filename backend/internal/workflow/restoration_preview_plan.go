package workflow

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// RestorationPreviewPlan compiles both sides of a pending review. The SQL
// admission transaction must independently replay source, object and quota
// authority before any compiled task may be activated.
type RestorationPreviewPlan struct {
	source RestorationPreviewSource
	run    uuid.UUID
	srgb   string
	policy string
	docs   [4]RestorationPreviewDocument
}

type RestorationPreviewDocument struct {
	Side string
	RenderDerivativeDocument
}

func (p RestorationPreviewPlan) Run() uuid.UUID                   { return p.run }
func (p RestorationPreviewPlan) Source() RestorationPreviewSource { return p.source }
func (p RestorationPreviewPlan) SRGBSHA() string                  { return p.srgb }

func (p RestorationPreviewPlan) Documents() []RestorationPreviewDocument {
	out := make([]RestorationPreviewDocument, len(p.docs))
	for i, value := range p.docs {
		out[i] = value
		out[i].Params = append(json.RawMessage(nil), value.Params...)
		out[i].Profile = append(json.RawMessage(nil), value.Profile...)
		out[i].Inputs = append([]review.ArtifactRef(nil), value.Inputs...)
	}
	return out
}

func restorationPreviewNodeKey(side, variant string) string {
	return "restoration." + side + ":" + variant
}

func restorationPreviewTaskID(run uuid.UUID, side, variant string) uuid.UUID {
	return uuid.NewSHA1(run, []byte(restorationPreviewNodeKey(side, variant)))
}

func PrepareRestorationPreviewTask(document RestorationPreviewDocument, params review.ArtifactRef) (PreparedTask, error) {
	if document.Side != "before" && document.Side != "after" {
		return PreparedTask{}, v2input.ErrInvalid
	}
	return prepareNamedRenderDerivativeTask(document.RenderDerivativeDocument, params,
		restorationPreviewNodeKey(document.Side, document.Variant))
}

// Caller supplies canonical, independently authorized WorkingImage bodies.
// This compiler proves document identity and metadata continuity, not pixels.
func CompileRestorationPreviewPlan(source RestorationPreviewSource, srgb string,
	baseRaw, candidateRaw []byte, policies ...string) (RestorationPreviewPlan, error) {
	var zero RestorationPreviewPlan
	policy,policyErr:=media.ResolveRendererPolicy(policies...);if policyErr!=nil{return zero,policyErr}
	if validateRestorationPreviewSource(source) != nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(srgb); err != nil {
		return zero, v2input.ErrInvalid
	}
	refs := [2]v2input.Ref{source.Base.Source, source.Candidate}
	bodies := [2][]byte{baseRaw, candidateRaw}
	var images [2]review.WorkingImage
	for i, raw := range bodies {
		if len(raw) < 1 || len(raw) > 1048576 || renderDerivativeBytesSHA(raw) != refs[i].SHA256 {
			return zero, v2input.ErrInvalid
		}
		body, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(body, raw) {
			return zero, v2input.ErrInvalid
		}
		images[i], err = review.ParseWorkingImage(raw)
		if err != nil || images[i].DType != "float32" {
			return zero, v2input.ErrInvalid
		}
	}
	base, candidate := images[0], images[1]
	if base.Pixels == candidate.Pixels || base.Canvas != candidate.Canvas ||
		base.Color != candidate.Color || base.DType != candidate.DType ||
		!bytes.Equal(base.TransformChain, candidate.TransformChain) ||
		(base.Alpha == nil) != (candidate.Alpha == nil) ||
		(base.Alpha != nil && *base.Alpha != *candidate.Alpha) {
		return zero, v2input.ErrInvalid
	}
	run := uuid.NewSHA1(source.Run, []byte("V28.RestorationPreview:"+
		source.Producer.String()+":"+source.ResultID.String()+":"+
		source.Result.SHA256+":"+source.Review.String()+":"+srgb))
	plan := RestorationPreviewPlan{source: source, run: run, srgb: srgb,policy:policy}
	for i, side := range []string{"before", "after"} {
		imageRef := review.ArtifactRef{ID: refs[i].ArtifactID, SHA256: refs[i].SHA256}
		inputs := workingInputs(imageRef, images[i])
		seen := make(map[uuid.UUID]bool, len(inputs))
		for _, ref := range inputs {
			if ref.ID == uuid.Nil || seen[ref.ID] {
				return zero, v2input.ErrInvalid
			}
			seen[ref.ID] = true
		}
		for j, variant := range []string{"preview1600", "preview2560"} {
			profileHash, err := media.ProfileHashForPolicy(variant, srgb,policy)
			if err != nil {
				return zero, err
			}
			params, err := canonical.Marshal(map[string]any{"image": imageRef,
				"variant": variant, "profile_hash": profileHash,
				"source_kind": "working", "suffix": nil, "matte_rgb": nil})
			if err != nil {
				return zero, err
			}
			profile, err := mediaPresentationProfile(MediaMaster{Artifact: imageRef,
				Working: &images[i]}, variant)
			if err != nil {
				return zero, err
			}
			plan.docs[i*2+j] = RestorationPreviewDocument{Side: side,
				RenderDerivativeDocument: RenderDerivativeDocument{
					ID: restorationPreviewTaskID(run, side, variant), Run: run,
					Variant: variant, Params: params, ParamsSHA: renderDerivativeBytesSHA(params),
					Inputs: append([]review.ArtifactRef(nil), inputs...), Profile: profile}}
		}
	}
	return plan, nil
}
