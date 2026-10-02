package workflow

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// ArtifactSize must be loaded from a ready artifact row in the same tenant.
// Planning does not establish ownership; insertion must recheck those rows.
type ArtifactSize struct {
	Ref   review.ArtifactRef
	Bytes int64
}
type PreparedTask struct {
	ID               uuid.UUID            `json:"id"`
	NodeKey          string               `json:"node_key"`
	Operation        string               `json:"operation"`
	Version          string               `json:"operation_version"`
	ResourceClass    string               `json:"resource_class"`
	TimeoutSeconds   int                  `json:"timeout_seconds"`
	Params           review.ArtifactRef   `json:"params"`
	Inputs           []review.ArtifactRef `json:"inputs"`
	InputDigest      string               `json:"input_digest"`
	ExecutionProfile json.RawMessage      `json:"execution_profile"`
}

// PreparedDefectTask keeps the existing defect-branch API.
type PreparedDefectTask = PreparedTask

// PrepareDefectTask produces an immutable insertion plan, never dispatches it.
// Scratch conservatively includes all declared files (even streamed JSON), the
// full float32 RGB output and two control documents. RAM is 2GiB for bounded
// <=4MP Telea ROIs; larger ROIs retain the worker's explicit resource failure.
func PrepareDefectTask(runID, reviewID uuid.UUID, canvas review.Canvas, branch MaterializedDefectBranch, sizes []ArtifactSize) (PreparedDefectTask, error) {
	fail := func() (PreparedDefectTask, error) {
		return PreparedDefectTask{}, errors.New("invalid repair task materialization")
	}
	const max = int64(9007199254740991)
	if runID == uuid.Nil || reviewID == uuid.Nil || branch.Route != "repair" || branch.Params == nil || branch.Selection == nil || branch.BaseImage != nil || branch.Params.ID == branch.Selection.ID ||
		len(branch.Inputs) < 1 || len(branch.Inputs) > 128 || canvas.Width < 1 || canvas.Height < 1 || canvas.Width > 200000000 || canvas.Height > 200000000 || canvas.Width*canvas.Height > 200000000 {
		return fail()
	}
	pixels := canvas.Width * canvas.Height
	known := map[uuid.UUID]ArtifactSize{}
	for _, a := range sizes {
		if a.Ref.ID == uuid.Nil || a.Bytes < 1 || a.Bytes > max {
			return fail()
		}
		if _, err := domain.ParseDigest(a.Ref.SHA256); err != nil {
			return fail()
		}
		if _, exists := known[a.Ref.ID]; exists {
			return fail()
		}
		known[a.Ref.ID] = a
	}
	inputs := append([]review.ArtifactRef(nil), branch.Inputs...)
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].ID.String() < inputs[j].ID.String() })
	seen := map[uuid.UUID]bool{}
	scratch := 12*pixels + 8192 + 2*1048576
	selectionDeclared := false
	for _, ref := range append(append([]review.ArtifactRef(nil), inputs...), *branch.Params) {
		a, exists := known[ref.ID]
		if !exists || a.Ref != ref || seen[ref.ID] || scratch > max-a.Bytes {
			return fail()
		}
		seen[ref.ID] = true
		scratch += a.Bytes
		if ref == *branch.Selection {
			selectionDeclared = true
		}
	}
	if !selectionDeclared || len(known) != len(seen) || known[branch.Params.ID].Bytes > 1048576 {
		return fail()
	}
	registry, err := LoadRegistry()
	if err != nil {
		return PreparedDefectTask{}, err
	}
	op, exists := registry["defects.repair"]
	if !exists || op.ResourceClass != "cpu" || op.Model != nil {
		return fail()
	}
	profile, err := canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		TileSize  int              `json:"tile_size"`
		MaxPixels int64            `json:"max_pixels"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", 256, 200000000, ResourceEstimate{RAMBytes: 2 * 1024 * 1024 * 1024, ScratchBytes: scratch, OutputBytes: 12*pixels + 8192 + 2*1048576, PixelCount: pixels}})
	if err != nil {
		return PreparedDefectTask{}, err
	}
	if _, err = ParseResourceEstimate(profile); err != nil {
		return fail()
	}
	digest, err := repairInputDigest(op.Name, op.Version, *branch.Params, inputs)
	if err != nil {
		return PreparedDefectTask{}, err
	}
	key := "defects.repair/review/" + reviewID.String()
	return PreparedDefectTask{ID: uuid.NewSHA1(runID, []byte(key)), NodeKey: key, Operation: op.Name, Version: op.Version, ResourceClass: op.ResourceClass,
		TimeoutSeconds: op.TimeoutSeconds, Params: *branch.Params, Inputs: inputs, InputDigest: digest, ExecutionProfile: profile}, nil
}

func repairInputDigest(operation, version string, params review.ArtifactRef, inputs []review.ArtifactRef) (string, error) {
	return canonical.Hash(struct {
		Version          string               `json:"version"`
		Operation        string               `json:"operation"`
		OperationVersion string               `json:"operation_version"`
		Params           review.ArtifactRef   `json:"params"`
		Inputs           []review.ArtifactRef `json:"inputs"`
	}{"task_input_v1", operation, version, params, inputs})
}
