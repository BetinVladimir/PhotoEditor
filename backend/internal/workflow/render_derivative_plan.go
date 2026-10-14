package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

type RenderDerivativeDocument struct {
	ID, Run   uuid.UUID
	Variant   string
	Params    json.RawMessage
	ParamsSHA string
	Inputs    []review.ArtifactRef
	Profile   json.RawMessage
}

// Compilation binds bytes; caller must independently authorize the complete adopted closure.
func BuildRenderDerivativeDocuments(intent v2render.RenderDerivativeIntent, resultRaw, imageRaw []byte) ([]RenderDerivativeDocument, error) {
	if intent.Run == uuid.Nil || intent.Root.ArtifactID == uuid.Nil || len(resultRaw) < 1 || len(resultRaw) > 1048576 || len(imageRaw) < 1 || len(imageRaw) > 1048576 {
		return nil, v2input.ErrInvalid
	}
	for _, raw := range [][]byte{resultRaw, imageRaw} {
		body, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(body, raw) {
			return nil, v2input.ErrInvalid
		}
	}
	if renderDerivativeBytesSHA(resultRaw) != intent.Root.SHA256 {
		return nil, v2input.ErrInvalid
	}
	root, err := v2draft.ParseImageResult(resultRaw, intent.Root)
	if err != nil {
		return nil, err
	}
	if renderDerivativeBytesSHA(imageRaw) != root.WorkingImage.SHA256 {
		return nil, v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil || image.DType != "float32" {
		return nil, v2input.ErrInvalid
	}
	imageRef := review.ArtifactRef{ID: root.WorkingImage.ArtifactID, SHA256: root.WorkingImage.SHA256}
	inputs := workingInputs(imageRef, image)
	seen := map[uuid.UUID]bool{intent.Root.ArtifactID: true}
	for _, ref := range inputs {
		if seen[ref.ID] {
			return nil, v2input.ErrInvalid
		}
		seen[ref.ID] = true
	}
	result := make([]RenderDerivativeDocument, 0, 2)
	for _, variant := range []string{"preview1600", "preview2560"} {
		profileHash, err := media.ProfileHash(variant, intent.SRGBSHA)
		if err != nil {
			return nil, err
		}
		params, err := canonical.Marshal(map[string]any{"image": imageRef, "variant": variant, "profile_hash": profileHash, "source_kind": "working", "suffix": nil, "matte_rgb": nil})
		if err != nil {
			return nil, err
		}
		profile, err := mediaPresentationProfile(MediaMaster{Artifact: imageRef, Working: &image}, variant)
		if err != nil {
			return nil, err
		}
		result = append(result, RenderDerivativeDocument{ID: uuid.NewSHA1(intent.Run, []byte("render.derivative:"+variant)), Run: intent.Run, Variant: variant, Params: params, ParamsSHA: renderDerivativeBytesSHA(params), Inputs: append([]review.ArtifactRef(nil), inputs...), Profile: profile})
	}
	return result, nil
}

func renderDerivativeBytesSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// PrepareRenderDerivativeTask binds the uploaded parameters without inventing source authority.
// SQL registration/materialization must recheck the adopted closure and quota before activation.
func PrepareRenderDerivativeTask(document RenderDerivativeDocument, params review.ArtifactRef) (PreparedTask, error) {
	return prepareNamedRenderDerivativeTask(document, params, "render.derivative:"+document.Variant)
}

func prepareNamedRenderDerivativeTask(document RenderDerivativeDocument, params review.ArtifactRef, nodeKey string) (PreparedTask, error) {
	if document.Run == uuid.Nil || document.ID != uuid.NewSHA1(document.Run, []byte(nodeKey)) || params.ID == uuid.Nil || params.SHA256 != document.ParamsSHA || renderDerivativeBytesSHA(document.Params) != document.ParamsSHA {
		return PreparedTask{}, v2input.ErrInvalid
	}
	if document.Variant != "preview1600" && document.Variant != "preview2560" {
		return PreparedTask{}, v2input.ErrInvalid
	}
	var body struct {
		Image       review.ArtifactRef `json:"image"`
		Variant     string             `json:"variant"`
		ProfileHash string             `json:"profile_hash"`
		SourceKind  string             `json:"source_kind"`
		Suffix      any                `json:"suffix"`
		Matte       any                `json:"matte_rgb"`
	}
	if json.Unmarshal(document.Params, &body) != nil {
		return PreparedTask{}, v2input.ErrInvalid
	}
	encoded, err := canonical.Marshal(body)
	if err != nil || !bytes.Equal(encoded, document.Params) || body.Variant != document.Variant || body.SourceKind != "working" || body.Suffix != nil || body.Matte != nil || len(document.Inputs) < 2 || len(document.Inputs) > 3 || document.Inputs[0] != body.Image {
		return PreparedTask{}, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(body.ProfileHash); err != nil {
		return PreparedTask{}, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{params.ID: true}
	for _, ref := range document.Inputs {
		if ref.ID == uuid.Nil || seen[ref.ID] {
			return PreparedTask{}, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return PreparedTask{}, v2input.ErrInvalid
		}
		seen[ref.ID] = true
	}
	profile, err := canonical.Bytes(document.Profile)
	if err != nil || !bytes.Equal(profile, document.Profile) {
		return PreparedTask{}, v2input.ErrInvalid
	}
	var specification struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &specification) != nil || specification.Backend != "cpu" || specification.Precision != "float32" || specification.Estimate.VRAMBytes != 0 {
		return PreparedTask{}, v2input.ErrInvalid
	}
	exactProfile, err := canonical.Marshal(specification)
	if err != nil || !bytes.Equal(exactProfile, profile) {
		return PreparedTask{}, v2input.ErrInvalid
	}
	if _, err := ParseResourceEstimate(profile); err != nil {
		return PreparedTask{}, err
	}
	digest, err := repairInputDigest("media.preview", "1", params, document.Inputs)
	if err != nil {
		return PreparedTask{}, err
	}
	return PreparedTask{ID: document.ID, NodeKey: nodeKey, Operation: "media.preview", Version: "1", ResourceClass: "media", TimeoutSeconds: 300, Params: params, Inputs: append([]review.ArtifactRef(nil), document.Inputs...), InputDigest: digest, ExecutionProfile: append(json.RawMessage(nil), profile...)}, nil
}
