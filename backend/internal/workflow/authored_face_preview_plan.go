package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// CompileAuthoredFacePreviewDocument uses the installed media.preview compute
// contract, not an original-asset derivative. Admission must independently lock
// the persisted candidate and current source before publishing/staging this plan.
func CompileAuthoredFacePreviewDocument(candidateSHA string, candidate v2render.AuthoredFaceSelectionCandidate, imageRaw []byte, srgb, policy string) (RenderDerivativeDocument, error) {
	var zero RenderDerivativeDocument
	body, err := canonical.Marshal(candidate)
	if err != nil || renderDerivativeBytesSHA(body) != candidateSHA || candidate.Tenant == uuid.Nil || candidate.Parent == uuid.Nil || candidate.Instruction == uuid.Nil || candidate.Ordinal < 1 || candidate.Ordinal > 4998 {
		return zero, v2input.ErrInvalid
	}
	imageBody, err := canonical.Bytes(imageRaw)
	if err != nil || !bytes.Equal(imageBody, imageRaw) || renderDerivativeBytesSHA(imageRaw) != candidate.Source.Source.SHA256 {
		return zero, v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil || image.DType != "float32" {
		return zero, v2input.ErrInvalid
	}
	canvasHash, err := canonical.Hash(image.Canvas)
	if err != nil || canvasHash != candidate.Source.CanvasHash || candidate.Source.Source.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	source := review.ArtifactRef{ID: candidate.Source.Source.ArtifactID, SHA256: candidate.Source.Source.SHA256}
	profileHash, err := media.ProfileHashForPolicy("preview1600", srgb, policy)
	if err != nil {
		return zero, err
	}
	params, err := canonical.Marshal(map[string]any{"image": source, "variant": "preview1600", "profile_hash": profileHash, "source_kind": "working", "suffix": nil, "matte_rgb": nil})
	if err != nil {
		return zero, err
	}
	profile, err := mediaPresentationProfile(MediaMaster{Artifact: source, Working: &image}, "preview1600")
	if err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(candidate.Instruction, []byte("AuthoredFacePreview/v1:"+candidateSHA))
	node := "authored.face:preview1600"
	return RenderDerivativeDocument{ID: uuid.NewSHA1(run, []byte(node)), Run: run, Variant: "preview1600", Params: json.RawMessage(params), ParamsSHA: renderDerivativeBytesSHA(params), Inputs: workingInputs(source, image), Profile: profile}, nil
}
func PrepareAuthoredFacePreviewTask(document RenderDerivativeDocument, params review.ArtifactRef) (PreparedTask, error) {
	return prepareNamedRenderDerivativeTask(document, params, "authored.face:preview1600")
}
