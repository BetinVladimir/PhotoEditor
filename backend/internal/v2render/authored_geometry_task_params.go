package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// AuthoredGeometryTaskParams names only actual pre-output inputs. A target
// registered-original descriptor is computed/staged by this blend producer;
// it cannot be nominated as a ready ref before that producer has executed.
// ParallelSource is the canonical unstyled occurrence before this transform.
type AuthoredGeometryTaskParams struct {
	SchemaVersion     int                           `json:"schema_version"`
	TenantID          uuid.UUID                     `json:"tenant_id"`
	ParentRunID       uuid.UUID                     `json:"parent_run_id"`
	WorkflowID        uuid.UUID                     `json:"workflow_id"`
	InstructionID     uuid.UUID                     `json:"instruction_id"`
	Ordinal           int                           `json:"ordinal"`
	ProgramSHA256     string                        `json:"program_sha256"`
	PredecessorSHA256 string                        `json:"predecessor_sha256"`
	Recipe            v2input.Ref                   `json:"recipe"`
	BaseReceipt       v2input.Ref                   `json:"base_receipt"`
	Source            v2input.SessionVersionBinding `json:"source"`
	ParallelSource    v2input.Ref                   `json:"parallel_source"`
	AuthoredParams    v2input.Ref                   `json:"authored_params"`
	Native            AuthoredGeometryPhaseWitness  `json:"native"`
	PhaseReceipts     []v2input.Ref                 `json:"phase_receipts"`
}
type CompiledAuthoredGeometryTaskParams struct {
	Params AuthoredGeometryTaskParams
	JSON   []byte
}

func CompileAuthoredGeometryTaskParams(p AuthoredGeometryTaskParams) (CompiledAuthoredGeometryTaskParams, error) {
	fail := func() (CompiledAuthoredGeometryTaskParams, error) {
		return CompiledAuthoredGeometryTaskParams{}, v2input.ErrInvalid
	}
	instruction, err := v2input.TaskID(p.WorkflowID, "render.authored_geometry", p.Ordinal)
	if err != nil || p.SchemaVersion != 1 || p.TenantID == uuid.Nil || p.ParentRunID == uuid.Nil || p.WorkflowID == uuid.Nil || p.InstructionID == uuid.Nil || p.InstructionID != instruction || p.Ordinal < 1 || p.Ordinal > 4998 || !authoredFinalDigest(p.ProgramSHA256) || !authoredFinalDigest(p.PredecessorSHA256) || !authoredBindingValid(p.Source) || len(p.PhaseReceipts) != p.Ordinal-1 {
		return fail()
	}
	n := p.Native.Task
	child := uuid.NewSHA1(p.InstructionID, []byte("AuthoredGeometryRun/v1"))
	if n.RunID == uuid.Nil || n.TaskID == uuid.Nil || n.ResultID == uuid.Nil || n.RunID == p.ParentRunID || n.RunID == child || n.Attempt < 1 || n.Fence < 1 || n.Version != "1" || len(n.Outputs) < 1 || len(n.Outputs) > 128 || !authoredFinalDigest(n.InputDigest) || !authoredFinalDigest(n.ProfileSHA) {
		return fail()
	}
	switch n.Operation {
	case "optical.lens", "optical.perspective", "optical.panorama", "optical.focus_stack", "image.upscale":
	default:
		return fail()
	}
	raw, err := canonical.Bytes([]byte(n.ProfileJCS))
	var profile map[string]json.RawMessage
	h := sha256.Sum256(raw)
	if err != nil || string(raw) != n.ProfileJCS || hex.EncodeToString(h[:]) != n.ProfileSHA || json.Unmarshal(raw, &profile) != nil || len(profile) == 0 {
		return fail()
	}
	seenOutputs := map[uuid.UUID]bool{}
	rootFound := false
	for _, r := range n.Outputs {
		if seenOutputs[r.ArtifactID] {
			return fail()
		}
		seenOutputs[r.ArtifactID] = true
		if r == p.Native.Root {
			rootFound = true
		}
	}
	if !rootFound {
		return fail()
	}
	refs := []v2input.Ref{p.Recipe, p.BaseReceipt, p.Source.Source, p.ParallelSource, p.AuthoredParams, n.Params, p.Native.Root, p.Native.Image, p.Native.Report}
	refs = append(refs, p.PhaseReceipts...)
	refs = append(refs, n.Outputs...)
	seen := map[uuid.UUID]string{}
	for _, r := range refs {
		if !validLayerArtifactRef(r) {
			return fail()
		}
		if old, ok := seen[r.ArtifactID]; ok && old != r.SHA256 {
			return fail()
		}
		seen[r.ArtifactID] = r.SHA256
	}
	if p.Recipe.ArtifactID == p.BaseReceipt.ArtifactID {
		return fail()
	}
	phaseIDs := map[uuid.UUID]bool{}
	for _, r := range p.PhaseReceipts {
		if phaseIDs[r.ArtifactID] || r.ArtifactID == p.Recipe.ArtifactID || r.ArtifactID == p.BaseReceipt.ArtifactID {
			return fail()
		}
		phaseIDs[r.ArtifactID] = true
	}
	roles := map[uuid.UUID]string{}
	bind := func(r v2input.Ref, contract string) bool {
		if old, ok := roles[r.ArtifactID]; ok && old != contract {
			return false
		}
		roles[r.ArtifactID] = contract
		return true
	}
	rootContract := "ImageResult"
	reportContract := "ImageReport"
	if n.Operation == "image.upscale" {
		rootContract = "WorkingImage"
		reportContract = "UpscaleReport"
		if p.Native.Root != p.Native.Image {
			return fail()
		}
	}
	if !bind(p.Recipe, "FrozenRenderRecipe") || !bind(p.BaseReceipt, "AuthoredBaseReceipt") || !bind(p.Source.Source, "WorkingImage") || !bind(p.ParallelSource, "WorkingImage") || !bind(p.AuthoredParams, "Params") || !bind(n.Params, "Params") || !bind(p.Native.Image, "WorkingImage") || !bind(p.Native.Report, reportContract) || !bind(p.Native.Root, rootContract) {
		return fail()
	}
	for _, r := range p.PhaseReceipts {
		if !bind(r, "AuthoredCanonicalPhaseReceipt") {
			return fail()
		}
	}
	p.PhaseReceipts = append([]v2input.Ref{}, p.PhaseReceipts...)
	p.Native.Task.Outputs = append([]v2input.Ref{}, n.Outputs...)
	body, err := canonical.Marshal(p)
	if err != nil {
		return fail()
	}
	return CompiledAuthoredGeometryTaskParams{Params: p, JSON: body}, nil
}
func ValidateAuthoredGeometryTaskParams(p CompiledAuthoredGeometryTaskParams, ref v2input.Ref) error {
	checked, err := CompileAuthoredGeometryTaskParams(p.Params)
	if err != nil || !validLayerArtifactRef(ref) || !bytes.Equal(checked.JSON, p.JSON) {
		return v2input.ErrInvalid
	}
	h := sha256.Sum256(checked.JSON)
	if hex.EncodeToString(h[:]) != ref.SHA256 {
		return v2input.ErrInvalid
	}
	return nil
}

// AuthoredGeometryTaskInputRoots are only typed direct roots. Actual scoped
// heterogeneous closure resolution and SQL authority remain mandatory.
func AuthoredGeometryTaskInputRoots(p CompiledAuthoredGeometryTaskParams, ref v2input.Ref) ([]v2input.Entry, error) {
	if ValidateAuthoredGeometryTaskParams(p, ref) != nil {
		return nil, v2input.ErrInvalid
	}
	x := p.Params
	rootContract := "ImageResult"
	reportContract := "ImageReport"
	if x.Native.Task.Operation == "image.upscale" {
		rootContract = "WorkingImage"
		reportContract = "UpscaleReport"
	}
	roots := []v2input.Entry{{Ref: ref, Contract: "Params"}, {Ref: x.Recipe, Contract: "FrozenRenderRecipe"}, {Ref: x.BaseReceipt, Contract: "AuthoredBaseReceipt"}, {Ref: x.Source.Source, Contract: "WorkingImage"}, {Ref: x.ParallelSource, Contract: "WorkingImage"}, {Ref: x.AuthoredParams, Contract: "Params"}, {Ref: x.Native.Task.Params, Contract: "Params"}, {Ref: x.Native.Root, Contract: rootContract}, {Ref: x.Native.Image, Contract: "WorkingImage"}, {Ref: x.Native.Report, Contract: reportContract}}
	for _, r := range x.PhaseReceipts {
		roots = append(roots, v2input.Entry{Ref: r, Contract: "AuthoredCanonicalPhaseReceipt"})
	}
	seen := map[uuid.UUID]v2input.Entry{}
	result := []v2input.Entry{}
	for i, r := range roots {
		if i > 0 && r.Ref.ArtifactID == ref.ArtifactID {
			return nil, v2input.ErrInvalid
		}
		if old, ok := seen[r.Ref.ArtifactID]; ok {
			if old != r {
				return nil, v2input.ErrInvalid
			}
			continue
		}
		seen[r.Ref.ArtifactID] = r
		result = append(result, r)
	}
	return result, nil
}
