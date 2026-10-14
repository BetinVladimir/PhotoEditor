package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"unicode/utf8"
)

type SemanticAssembleParams struct {
	SchemaVersion int    `json:"schema_version"`
	Intent        Ref    `json:"intent"`
	Candidates    []Ref  `json:"candidates"`
	ModelLockSHA  string `json:"model_lock_sha"`
	Calibration   Ref    `json:"calibration"`
}
type SemanticCandidate struct {
	SchemaVersion int                   `json:"schema_version"`
	Target        SessionVersionBinding `json:"target"`
	Mask          *Ref                  `json:"mask"`
	Transform     *Ref                  `json:"transform"`
	Confidence    float64               `json:"confidence"`
	State         string                `json:"state"`
	Reason        *string               `json:"reason"`
}
type SemanticCandidateDocument struct {
	Ref        Ref
	Body       json.RawMessage
	TargetBody json.RawMessage
}
type FrozenSemanticAssembly struct {
	task    uuid.UUID
	params  Ref
	profile json.RawMessage
	closure CaptureClosure
}

func DecodeSemanticAssembleParams(raw []byte) (SemanticAssembleParams, error) {
	var p SemanticAssembleParams
	fields, ok := semanticObject(raw, nil, "schema_version", "intent", "candidates", "model_lock_sha", "calibration")
	if len(raw) > 8<<20 || !ok || !semanticRefJSON(fields["intent"]) || !semanticRefJSON(fields["calibration"]) || !semanticDecode(raw, &p) {
		return p, ErrInvalid
	}
	var refs []json.RawMessage
	if json.Unmarshal(fields["candidates"], &refs) != nil || len(refs) < 1 || len(refs) > 5000 {
		return SemanticAssembleParams{}, ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for n, ref := range p.Candidates {
		if !semanticRefJSON(refs[n]) || !validRef(ref) || seen[ref.ArtifactID] {
			return SemanticAssembleParams{}, ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	if p.SchemaVersion != 1 || !validRef(p.Intent) || !validRef(p.Calibration) || !digestPattern.MatchString(p.ModelLockSHA) {
		return SemanticAssembleParams{}, ErrInvalid
	}
	return p, nil
}
func DecodeSemanticCandidate(raw []byte) (SemanticCandidate, error) {
	var c SemanticCandidate
	fields, ok := semanticObject(raw, map[string]bool{"mask": true, "transform": true, "reason": true}, "schema_version", "target", "mask", "transform", "confidence", "state", "reason")
	if len(raw) > 1<<20 || !ok || !semanticBindingJSON(fields["target"]) || !semanticDecode(raw, &c) || c.SchemaVersion != 1 || !semanticBindingValid(c.Target) || !finiteRange(c.Confidence, 0, 1) {
		return SemanticCandidate{}, ErrInvalid
	}
	for key, ref := range map[string]*Ref{"mask": c.Mask, "transform": c.Transform} {
		if ref != nil && (!semanticRefJSON(fields[key]) || !validRef(*ref)) {
			return SemanticCandidate{}, ErrInvalid
		}
	}
	if c.Reason != nil && (!utf8.ValidString(*c.Reason) || utf8.RuneCountInString(*c.Reason) < 1 || utf8.RuneCountInString(*c.Reason) > 80) {
		return SemanticCandidate{}, ErrInvalid
	}
	switch c.State {
	case "proposed":
		if c.Mask == nil || c.Transform == nil || c.Reason != nil {
			return SemanticCandidate{}, ErrInvalid
		}
	case "unsupported":
		if c.Mask != nil || c.Transform != nil || c.Reason == nil {
			return SemanticCandidate{}, ErrInvalid
		}
	case "rejected":
		if (c.Mask == nil) != (c.Transform == nil) || c.Reason == nil {
			return SemanticCandidate{}, ErrInvalid
		}
	default:
		return SemanticCandidate{}, ErrInvalid
	}
	return c, nil
}
func CompileSemanticAssemblyProfile(ram, scratch, output int64) (json.RawMessage, error) {
	if ram < 256<<20 || scratch < 32<<20 || output < 8<<20 || ram > 9007199254740991 || scratch > 9007199254740991 || output > 9007199254740991 {
		return nil, ErrInvalid
	}
	return canonical.Marshal(sessionDescribeExecutionProfile{"cpu", "v2.2", sessionDescribeEstimate{RAMBytes: 256 << 20, ScratchBytes: 32 << 20, OutputBytes: 8 << 20}})
}

// CompileSemanticAssemblyTask freezes exact candidate content and complete
// snapshot coverage. Scoped adopted producer and calibration qualification
// authority must independently be established by the resolver/admission path.
func CompileSemanticAssemblyTask(workflow uuid.UUID, params SemanticAssembleParams, paramsRef Ref, intent SemanticEditIntent, snapshotRef Ref, snapshotBody json.RawMessage, candidates []SemanticCandidateDocument, profile json.RawMessage) (FrozenSemanticAssembly, error) {
	var empty FrozenSemanticAssembly
	raw, err := canonical.Marshal(params)
	if err != nil {
		return empty, ErrInvalid
	}
	owned, err := DecodeSemanticAssembleParams(raw)
	if err != nil || len(candidates) != len(owned.Candidates) {
		return empty, ErrInvalid
	}
	intentBody, err := canonical.Marshal(intent)
	if err != nil {
		return empty, ErrInvalid
	}
	intent, err = DecodeSemanticEditIntent(intentBody)
	if err != nil {
		return empty, ErrInvalid
	}
	intentHash, _ := canonical.Hash(intent)
	snapshot, err := DecodePinnedSessionInputSnapshot(snapshotRef, snapshotBody)
	if err != nil || intentHash != owned.Intent.SHA256 || snapshot.SnapshotID != intent.TargetSnapshotID || len(snapshot.Items) != len(candidates) {
		return empty, ErrInvalid
	}
	fields, ok := semanticObject(profile, nil, "backend", "renderer_version", "resource_estimate")
	if !ok {
		return empty, ErrInvalid
	}
	if _, ok := semanticObject(fields["resource_estimate"], nil, "ram_bytes", "vram_bytes", "scratch_bytes", "output_bytes", "pixel_count"); !ok {
		return empty, ErrInvalid
	}
	var execution sessionDescribeExecutionProfile
	if !semanticDecode(profile, &execution) || execution.Backend != "cpu" || execution.RendererVersion != "v2.2" || execution.ResourceEstimate.VRAMBytes != 0 || execution.ResourceEstimate.PixelCount != 0 {
		return empty, ErrInvalid
	}
	budget := execution.ResourceEstimate
	if _, err := CompileSemanticAssemblyProfile(budget.RAMBytes, budget.ScratchBytes, budget.OutputBytes); err != nil {
		return empty, err
	}
	targets := map[uuid.UUID]SessionVersionBinding{}
	for _, item := range snapshot.Items {
		targets[item.Binding.AssetVersionID] = item.Binding
	}
	entries := []Entry{{paramsRef, "Params"}, {owned.Intent, "EditIntent"}, {owned.Calibration, "Calibration"}, {snapshotRef, "InputSnapshot"}}
	for n, document := range candidates {
		if document.Ref != owned.Candidates[n] {
			return empty, ErrInvalid
		}
		sum := sha256.Sum256(document.Body)
		if hex.EncodeToString(sum[:]) != document.Ref.SHA256 {
			return empty, ErrInvalid
		}
		candidate, err := DecodeSemanticCandidate(document.Body)
		if err != nil || targets[candidate.Target.AssetVersionID] != candidate.Target {
			return empty, ErrInvalid
		}
		delete(targets, candidate.Target.AssetVersionID)
		sourceSum := sha256.Sum256(document.TargetBody)
		image, err := review.ParseWorkingImage(document.TargetBody)
		canvasHash, _ := canonical.Hash(image.Canvas)
		if err != nil || hex.EncodeToString(sourceSum[:]) != candidate.Target.Source.SHA256 || canvasHash != candidate.Target.CanvasHash {
			return empty, ErrInvalid
		}
		entries = append(entries, Entry{document.Ref, "TransferCandidate"}, Entry{candidate.Target.Source, "WorkingImage"})
		if candidate.Mask != nil {
			entries = append(entries, Entry{*candidate.Mask, "MaskRef"}, Entry{*candidate.Transform, "ResolvedTransform"})
		}
	}
	if len(targets) != 0 {
		return empty, ErrInvalid
	}
	task, err := TaskID(workflow, "v2.semantic.assemble", 0)
	if err != nil {
		return empty, err
	}
	closure, err := compileCaptureInventory(task, "v2.semantic.assemble", owned, paramsRef, entries)
	if err != nil {
		return empty, err
	}
	// Large candidate lists are transported by chunks, not wire direct inputs.
	closure.Direct = []Ref{paramsRef, owned.Intent, owned.Calibration, snapshotRef}
	return FrozenSemanticAssembly{task, paramsRef, append(json.RawMessage(nil), profile...), closure}, nil
}
func (f FrozenSemanticAssembly) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || !validRef(f.params) || f.closure.Open == nil || !json.Valid(f.profile) {
		return DraftTask{}, ErrInvalid
	}
	key, _ := NodeKey("v2.semantic.assemble", 0)
	return DraftTask{ID: f.task, NodeKey: key, Operation: "v2.semantic.assemble", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256, InputDigest: f.closure.InputDigest, ExecutionProfile: append(json.RawMessage(nil), f.profile...)}, nil
}
func PublishSemanticAssemblyTask(ctx context.Context, f FrozenSemanticAssembly, publisher Publisher) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	if publisher == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	cursor, err := f.closure.Open(ctx)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	built, err := BuildInputClosure(ctx, f.task, cursor, publisher)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	return PreparedSemanticTask{Graph: GraphTask{f.task, built, f.closure.Open, append([]Ref(nil), f.closure.Direct...)}, Params: f.params, ParamsJCS: append(json.RawMessage(nil), f.closure.ParamsJCS...), Profile: append(json.RawMessage(nil), f.profile...), InputDigest: f.closure.InputDigest, NodeKey: draft.NodeKey}, nil
}
