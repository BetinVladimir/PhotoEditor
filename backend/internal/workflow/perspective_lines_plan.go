package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"sort"
)

// Compilation binds bytes and resources, not SQL authority. The scheduler must
// freeze an authorized accepted-version snapshot before publishing this plan.
type PerspectiveLinesScope struct {
	Tenant, Project, Run uuid.UUID
	Base                 v2input.SessionVersionBinding
}
type perspectiveLinesParams struct {
	SchemaVersion int                           `json:"schema_version"`
	Base          v2input.SessionVersionBinding `json:"base"`
}
type PerspectiveLinesPlan struct {
	scope                  PerspectiveLinesScope
	task                   uuid.UUID
	workingICC             string
	image, params, profile json.RawMessage
	inputs                 []review.ArtifactRef
}

func (p PerspectiveLinesPlan) Scope() PerspectiveLinesScope { return p.scope }
func (p PerspectiveLinesPlan) Task() uuid.UUID              { return p.task }
func (p PerspectiveLinesPlan) Params() json.RawMessage      { return bytes.Clone(p.params) }
func (p PerspectiveLinesPlan) Profile() json.RawMessage     { return bytes.Clone(p.profile) }
func (p PerspectiveLinesPlan) Inputs() []review.ArtifactRef {
	return append([]review.ArtifactRef(nil), p.inputs...)
}

func CompilePerspectiveLinesPlan(scope PerspectiveLinesScope, raw []byte, workingICC string) (PerspectiveLinesPlan, error) {
	var zero PerspectiveLinesPlan
	if scope.Tenant == uuid.Nil || scope.Project == uuid.Nil || scope.Run == uuid.Nil || scope.Base.AssetVersionID == uuid.Nil || scope.Base.Source.ArtifactID == uuid.Nil || domainDigestInvalid(workingICC) || len(raw) < 2 || len(raw) > v2optical.PerspectiveWorkingImageMaxBytes || renderDerivativeBytesSHA(raw) != scope.Base.Source.SHA256 {
		return zero, v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		return zero, err
	}
	exact, err := canonical.Marshal(image)
	if err != nil || !bytes.Equal(exact, raw) || image.DType != "float32" || image.Color.ICCSHA256 != workingICC || image.Canvas.Width < 2 || image.Canvas.Height < 2 || image.Canvas.Width > 200000000/image.Canvas.Height {
		return zero, v2input.ErrInvalid
	}
	canvasHash, err := canonical.Hash(image.Canvas)
	if err != nil || canvasHash != scope.Base.CanvasHash {
		return zero, v2input.ErrInvalid
	}
	inputs := []review.ArtifactRef{{ID: scope.Base.Source.ArtifactID, SHA256: scope.Base.Source.SHA256}, image.Pixels}
	perPixel := int64(12)
	if image.Alpha != nil {
		inputs = append(inputs, *image.Alpha)
		perPixel = 16
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range inputs {
		if ref.ID == uuid.Nil || seen[ref.ID] || domainDigestInvalid(ref.SHA256) {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ID] = true
	}
	// Historical dense fields remain declared provenance, even though this
	// detector reads only the current RGB/alpha canvas.
	var chain struct {
		Steps []struct {
			InverseField *review.ArtifactRef `json:"inverse_field"`
		} `json:"steps"`
	}
	if json.Unmarshal(image.TransformChain, &chain) != nil {
		return zero, v2input.ErrInvalid
	}
	primaryCount := len(inputs)
	for _, step := range chain.Steps {
		if step.InverseField == nil {
			continue
		}
		ref := *step.InverseField
		if seen[ref.ID] {
			found := false
			for _, old := range inputs[primaryCount:] {
				if old == ref {
					found = true
				}
			}
			if !found {
				return zero, v2input.ErrInvalid
			}
			continue
		}
		inputs = append(inputs, ref)
		seen[ref.ID] = true
	}
	params, err := canonical.Marshal(perspectiveLinesParams{SchemaVersion: 1, Base: scope.Base})
	if err != nil {
		return zero, err
	}
	pixels := int64(image.Canvas.Width) * int64(image.Canvas.Height)
	profile, err := canonical.Marshal(map[string]any{"backend": "cpu", "precision": "float32", "renderer_version": "v2.2", "resource_estimate": ResourceEstimate{RAMBytes: pixels*perPixel + 268435456, ScratchBytes: pixels*perPixel + 33554432, OutputBytes: 1048576, VRAMBytes: 0, PixelCount: pixels}})
	if err != nil {
		return zero, err
	}
	if _, err = ParseResourceEstimate(profile); err != nil {
		return zero, err
	}
	identity, err := canonical.Marshal(map[string]any{"tenant": scope.Tenant, "project": scope.Project, "params": json.RawMessage(params), "working_icc_sha": workingICC})
	if err != nil {
		return zero, err
	}
	task := uuid.NewSHA1(scope.Run, append([]byte("V29.PerspectiveLines:"), identity...))
	return PerspectiveLinesPlan{scope: scope, task: task, workingICC: workingICC, image: bytes.Clone(raw), params: params, profile: profile, inputs: inputs}, nil
}

func PreparePerspectiveLinesTask(plan PerspectiveLinesPlan, params review.ArtifactRef) (PreparedTask, error) {
	exact, err := CompilePerspectiveLinesPlan(plan.scope, plan.image, plan.workingICC)
	if err != nil {
		return PreparedTask{}, err
	}
	if plan.task != exact.task || !bytes.Equal(plan.params, exact.params) || !bytes.Equal(plan.profile, exact.profile) || len(plan.inputs) != len(exact.inputs) || params.ID == uuid.Nil || params.SHA256 != renderDerivativeBytesSHA(exact.params) {
		return PreparedTask{}, v2input.ErrInvalid
	}
	for i, ref := range exact.inputs {
		if plan.inputs[i] != ref || ref.ID == params.ID {
			return PreparedTask{}, v2input.ErrInvalid
		}
	}
	digest, err := repairInputDigest("optical.perspective_lines", "1", params, exact.inputs)
	if err != nil {
		return PreparedTask{}, err
	}
	return PreparedTask{ID: exact.task, NodeKey: "optical.perspective_lines", Operation: "optical.perspective_lines", Version: "1", ResourceClass: "cpu", TimeoutSeconds: 600, Params: params, Inputs: exact.Inputs(), InputDigest: digest, ExecutionProfile: exact.Profile()}, nil
}

// Entries supplies the exact sorted typed cursor for the existing V2 closure
// builder. Publication and SQL authority checks remain the caller's duty.
func (p PerspectiveLinesPlan) Entries(params review.ArtifactRef) ([]v2input.Entry, error) {
	if _, err := PreparePerspectiveLinesTask(p, params); err != nil {
		return nil, err
	}
	entries := []v2input.Entry{{Ref: v2input.Ref{ArtifactID: params.ID, SHA256: params.SHA256}, Contract: "PerspectiveLineDetectionParams"}}
	for i, ref := range p.inputs {
		contract := "NpyArray"
		if i == 0 {
			contract = "WorkingImage"
		}
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Contract: contract})
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	return entries, nil
}
