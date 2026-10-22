package v2input

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// Content-only ABI projection. SQL and physical equality remain independent.
type AuthoredNativeReplayComparison struct {
	Role     string `json:"role"`
	Original Ref    `json:"original"`
	Replay   Ref    `json:"replay"`
	SHA      string `json:"sha256"`
	Bytes    int64  `json:"bytes"`
}
type AuthoredNativeReplayEvidence struct {
	SchemaVersion int                              `json:"schema_version"`
	Request       Ref                              `json:"request"`
	RequestSHA    string                           `json:"request_sha256"`
	Replay        AuthoredNativeObservation        `json:"replay"`
	Comparisons   []AuthoredNativeReplayComparison `json:"comparisons"`
}

func DecodeAuthoredNativeReplayEvidence(raw []byte) (AuthoredNativeReplayEvidence, error) {
	var e AuthoredNativeReplayEvidence
	if len(raw) < 2 || len(raw) > 4<<20 {
		return e, ErrInvalid
	}
	wire, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(wire, raw) {
		return e, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil {
		return e, ErrInvalid
	}
	if e.SchemaVersion != 1 || !validRef(e.Request) || e.Request.SHA256 != e.RequestSHA || !nativeReplayProducer(e.Replay.Task) || !validRef(e.Replay.Root) || !validRef(e.Replay.Image) || !validRef(e.Replay.Report) || len(e.Comparisons) < 1 || len(e.Comparisons) > 4 {
		return e, ErrInvalid
	}
	if e.Replay.Task.Operation != "image.upscale" || e.Replay.Task.Version != "1" || e.Replay.Root != e.Replay.Image {
		return e, ErrInvalid
	}
	root, report := false, false
	for _, ref := range e.Replay.Task.Outputs {
		root = root || ref == e.Replay.Root
		report = report || ref == e.Replay.Report
	}
	if !root || !report {
		return e, ErrInvalid
	}
	seen := map[string]bool{}
	for _, c := range e.Comparisons {
		if (c.Role != "pixels" && c.Role != "alpha" && c.Role != "inverse_field") || seen[c.Role] || !validRef(c.Original) || !validRef(c.Replay) || c.Original.ArtifactID == c.Replay.ArtifactID || c.Bytes < 1 || c.Bytes > 9007199254740991 || c.Original.SHA256 != c.SHA || c.Replay.SHA256 != c.SHA {
			return e, ErrInvalid
		}
		seen[c.Role] = true
	}
	out, err := canonical.Marshal(e)
	if err != nil || !bytes.Equal(out, raw) {
		return e, ErrInvalid
	}
	return e, nil
}

func AuthoredNativeReplayEvidenceChildren(raw []byte) (AuthoredReplayChildren, error) {
	e, err := DecodeAuthoredNativeReplayEvidence(raw)
	if err != nil {
		return AuthoredReplayChildren{}, err
	}
	result := AuthoredReplayChildren{Typed: []AuthoredReplayChild{{Ref: e.Request, Contract: "AuthoredNativeReplayRequest"}, {Ref: e.Replay.Task.Params, Contract: "Params"}, {Ref: e.Replay.Root, Contract: "WorkingImage"}, {Ref: e.Replay.Image, Contract: "WorkingImage"}, {Ref: e.Replay.Report, Contract: "UpscaleReport"}}, Opaque: append([]Ref(nil), e.Replay.Task.Outputs...)}
	for _, c := range e.Comparisons {
		result.Typed = append(result.Typed, AuthoredReplayChild{Ref: c.Original, Contract: "NpyArray"}, AuthoredReplayChild{Ref: c.Replay, Contract: "NpyArray"})
	}
	refs := map[uuid.UUID]Ref{}
	contracts := map[uuid.UUID]string{}
	for _, c := range result.Typed {
		if prior, ok := refs[c.Ref.ArtifactID]; ok && prior != c.Ref {
			return AuthoredReplayChildren{}, ErrInvalid
		}
		if prior, ok := contracts[c.Ref.ArtifactID]; ok && prior != c.Contract {
			return AuthoredReplayChildren{}, ErrInvalid
		}
		refs[c.Ref.ArtifactID] = c.Ref
		contracts[c.Ref.ArtifactID] = c.Contract
	}
	for _, ref := range result.Opaque {
		if prior, ok := refs[ref.ArtifactID]; ok && prior != ref {
			return AuthoredReplayChildren{}, ErrInvalid
		}
		refs[ref.ArtifactID] = ref
	}
	return result, nil
}
