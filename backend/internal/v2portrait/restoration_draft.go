package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RestorationDraft is a frozen descriptor for the common V2 draft-shell and
// input-closure publisher. SQL locks and source/consent proof remain mandatory
// before publication and graph seal.
type RestorationDraft struct {
	Task    v2input.DraftTask
	Entries []v2input.Entry
}

func restorationDirectInputs(operation string, paramsRaw []byte) ([]v2input.Entry, int, error) {
	var p struct {
		Base struct {
			Source v2input.Ref `json:"source"`
		} `json:"base"`
		Donor *struct {
			Source v2input.Ref `json:"source"`
		} `json:"donor"`
		Mode         string       `json:"mode"`
		GlassesMask  v2input.Ref  `json:"glasses_mask"`
		Mask         v2input.Ref  `json:"mask"`
		Protected    v2input.Ref  `json:"protected"`
		Confirmation v2input.Ref  `json:"confirmation"`
		SourceFaces  v2input.Ref  `json:"source_faces"`
		DonorFaces   *v2input.Ref `json:"donor_faces"`
		Calibration  v2input.Ref  `json:"calibration"`
	}
	if json.Unmarshal(paramsRaw, &p) != nil {
		return nil, 0, v2input.ErrInvalid
	}
	entries := []v2input.Entry{{Ref: p.Base.Source, Contract: "WorkingImage"}}
	minimumArrays := 3
	if operation == "image.reflection" {
		entries = append(entries,
			v2input.Entry{Ref: p.Mask, Contract: "MaskRef"},
			v2input.Entry{Ref: p.Protected, Contract: "MaskRef"},
			v2input.Entry{Ref: p.Calibration, Contract: "Calibration"})
		if p.Donor != nil || p.DonorFaces != nil || p.Mode != "" {
			return nil, 0, v2input.ErrInvalid
		}
	} else if operation == "portrait.glare" {
		entries = append(entries,
			v2input.Entry{Ref: p.GlassesMask, Contract: "MaskRef"},
			v2input.Entry{Ref: p.Protected, Contract: "MaskRef"},
			v2input.Entry{Ref: p.Calibration, Contract: "Calibration"},
			v2input.Entry{Ref: p.Confirmation, Contract: "DonorConfirmation"},
			v2input.Entry{Ref: p.SourceFaces, Contract: "FaceObservations"})
		if p.Mode == "donor" && p.Donor != nil && p.DonorFaces != nil {
			entries = append(entries,
				v2input.Entry{Ref: p.Donor.Source, Contract: "WorkingImage"},
				v2input.Entry{Ref: *p.DonorFaces, Contract: "FaceObservations"})
			minimumArrays = 4
		} else if p.Mode != "generative" || p.Donor != nil || p.DonorFaces != nil {
			return nil, 0, v2input.ErrInvalid
		}
	} else {
		return nil, 0, v2input.ErrInvalid
	}
	return entries, minimumArrays, nil
}

// CompileRestorationDraftTask binds the exact Params, qualification pin and
// sorted typed inventory to a deterministic GPU-class task identity.
func CompileRestorationDraftTask(ctx context.Context, workflow uuid.UUID, ordinal int,
	operation string, paramsRaw, baseProfile []byte, pin RestorationQualificationPin,
	paramsRef v2input.Ref, inputs []v2input.Entry) (RestorationDraft, error) {
	var zero RestorationDraft
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if workflow == uuid.Nil || paramsRef.ArtifactID == uuid.Nil ||
		len(inputs) < 6 || len(inputs) > 127 {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(paramsRef.SHA256); err != nil {
		return zero, v2input.ErrInvalid
	}
	sum := sha256.Sum256(paramsRaw)
	if paramsRef.SHA256 != hex.EncodeToString(sum[:]) {
		return zero, v2input.ErrInvalid
	}
	profile, err := CompilePinnedRestorationProfile(operation, paramsRaw, baseProfile, pin)
	if err != nil {
		return zero, err
	}
	direct, minimumArrays, err := restorationDirectInputs(operation, paramsRaw)
	if err != nil {
		return zero, err
	}
	expected := map[uuid.UUID]v2input.Entry{}
	for _, entry := range append(direct,
		v2input.Entry{Ref: paramsRef, Contract: "Params"},
		v2input.Entry{Ref: pin.Report, Contract: "Report"}) {
		if entry.Ref.ArtifactID == uuid.Nil || expected[entry.Ref.ArtifactID].Ref.ArtifactID != uuid.Nil {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(entry.Ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		expected[entry.Ref.ArtifactID] = entry
	}
	seen := map[uuid.UUID]bool{}
	arrayCount := 0
	for i, entry := range inputs {
		if entry.Ref.ArtifactID == uuid.Nil || seen[entry.Ref.ArtifactID] ||
			i > 0 && inputs[i-1].Ref.ArtifactID.String() >= entry.Ref.ArtifactID.String() {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(entry.Ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
		if want, found := expected[entry.Ref.ArtifactID]; found {
			if entry != want {
				return zero, v2input.ErrInvalid
			}
		} else if entry.Contract == "NpyArray" {
			arrayCount++
		} else {
			return zero, v2input.ErrInvalid
		}
	}
	if arrayCount < minimumArrays || len(seen) < len(expected) {
		return zero, v2input.ErrInvalid
	}
	for id := range expected {
		if !seen[id] {
			return zero, v2input.ErrInvalid
		}
	}
	taskID, err := v2input.TaskID(workflow, operation, ordinal)
	if err != nil {
		return zero, err
	}
	key, err := v2input.NodeKey(operation, ordinal)
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{taskID, operation, "1", inputs})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return RestorationDraft{Task: v2input.DraftTask{
		ID: taskID, NodeKey: key, Operation: operation, OperationVersion: "1",
		ResourceClass: "gpu", ParamsArtifactID: paramsRef.ArtifactID,
		ParamsSHA: paramsRef.SHA256, InputDigest: digest,
		ExecutionProfile: append(json.RawMessage(nil), profile...),
	}, Entries: append([]v2input.Entry(nil), inputs...)}, nil
}
