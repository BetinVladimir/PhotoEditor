package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// AuthoredMaskTransformParams is the existing probability-mask worker contract.
// One child handles one original region/protection; regional batching is not
// silently substituted for independent provenance or output ownership.
type AuthoredMaskTransformParams struct {
	Mask   review.MaskRef  `json:"mask"`
	Target Ref             `json:"target"`
	Step   json.RawMessage `json:"step"`
	DType  string          `json:"dtype"`
}

type FrozenAuthoredMaskTransformTask struct {
	task    uuid.UUID
	key     string
	params  Ref
	profile string
	closure CaptureClosure
}

// PrepareAuthoredMaskTransformParams proves exactly one existing, validated
// geometry step. It grants no catalog/adoption authority to the supplied bytes.
func PrepareAuthoredMaskTransformParams(ctx context.Context, maskRaw, targetRaw []byte, target Ref) (AuthoredMaskTransformParams, []Entry, error) {
	var zero AuthoredMaskTransformParams
	if err := ctx.Err(); err != nil {
		return zero, nil, err
	}
	if !validRef(target) || len(maskRaw) < 2 || len(maskRaw) > 1048576 || len(targetRaw) < 2 || len(targetRaw) > 1048576 {
		return zero, nil, ErrInvalid
	}
	targetJCS, err := canonical.Bytes(targetRaw)
	if err != nil || !bytes.Equal(targetJCS, targetRaw) {
		return zero, nil, ErrInvalid
	}
	digest, err := canonical.Hash(json.RawMessage(targetRaw))
	if err != nil || digest != target.SHA256 {
		return zero, nil, ErrInvalid
	}
	maskJCS, err := canonical.Bytes(maskRaw)
	if err != nil || !bytes.Equal(maskJCS, maskRaw) {
		return zero, nil, ErrInvalid
	}
	mask, err := review.ParseMaskRef(maskRaw)
	if err != nil {
		return zero, nil, err
	}
	image, err := review.ParseWorkingImage(targetRaw)
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" {
		return zero, nil, ErrInvalid
	}
	step, err := review.LastTransformStep(image)
	if err != nil {
		return zero, nil, err
	}
	var sourceChain, targetChain struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}
	if json.Unmarshal(mask.TransformChain, &sourceChain) != nil || json.Unmarshal(image.TransformChain, &targetChain) != nil || len(targetChain.Steps) != len(sourceChain.Steps)+1 {
		return zero, nil, ErrInvalid
	}
	for i := range sourceChain.Steps {
		a, e := canonical.Bytes(sourceChain.Steps[i])
		if e != nil {
			return zero, nil, ErrInvalid
		}
		b, e := canonical.Bytes(targetChain.Steps[i])
		if e != nil || !bytes.Equal(a, b) {
			return zero, nil, ErrInvalid
		}
	}
	var geometry struct {
		From review.Canvas `json:"from_canvas"`
		To   review.Canvas `json:"to_canvas"`
	}
	if json.Unmarshal(step, &geometry) != nil || geometry.From != mask.Canvas || geometry.To != image.Canvas {
		return zero, nil, ErrInvalid
	}
	if int64(mask.Canvas.Width)*int64(mask.Canvas.Height) > 200000000 || int64(image.Canvas.Width)*int64(image.Canvas.Height) > 200000000 {
		return zero, nil, ErrInvalid
	}
	entries := []Entry{{target, "WorkingImage"}, {Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, "NpyArray"}, {Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}, "NpyArray"}}
	if image.Alpha != nil {
		entries = append(entries, Entry{Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, "NpyArray"})
	}
	// All retained inverse-field references are explicit readable dependencies,
	// including prior validated chain steps, never a broad source-catalog scan.
	for _, raw := range targetChain.Steps {
		var v struct {
			Inverse *Ref `json:"inverse_field"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return zero, nil, ErrInvalid
		}
		if v.Inverse != nil {
			entries = append(entries, Entry{*v.Inverse, "NpyArray"})
		}
	}
	unique := map[uuid.UUID]Entry{}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if !validEntry(entry) {
			return zero, nil, ErrInvalid
		}
		if old, ok := unique[entry.Ref.ArtifactID]; ok {
			if old != entry {
				return zero, nil, ErrInvalid
			}
			continue
		}
		unique[entry.Ref.ArtifactID] = entry
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Ref.ArtifactID[:], out[j].Ref.ArtifactID[:]) < 0 })
	return AuthoredMaskTransformParams{Mask: mask, Target: target, Step: step, DType: "float32"}, out, ctx.Err()
}

func CompileAuthoredMaskTransformTask(ctx context.Context, workflow uuid.UUID, maskRaw, targetRaw []byte, target, params Ref, entries []Entry, sizes []SemanticInputSize, ram, scratch, output int64) (FrozenAuthoredMaskTransformTask, error) {
	var zero FrozenAuthoredMaskTransformTask
	prepared, required, err := PrepareAuthoredMaskTransformParams(ctx, maskRaw, targetRaw, target)
	if err != nil {
		return zero, err
	}
	if !validRef(params) || workflow == uuid.Nil {
		return zero, ErrInvalid
	}
	required = append(required, Entry{params, "Params"})
	if len(entries) != len(required) {
		return zero, ErrInvalid
	}
	expected := map[uuid.UUID]Entry{}
	for _, entry := range required {
		if _, ok := expected[entry.Ref.ArtifactID]; ok {
			return zero, ErrInvalid
		}
		expected[entry.Ref.ArtifactID] = entry
	}
	owned := append([]Entry(nil), entries...)
	for _, entry := range owned {
		if expected[entry.Ref.ArtifactID] != entry {
			return zero, ErrInvalid
		}
		delete(expected, entry.Ref.ArtifactID)
	}
	if len(expected) != 0 {
		return zero, ErrInvalid
	}
	inputs := make([]Ref, 0, len(required)-1)
	for _, entry := range required {
		if entry.Ref != params {
			inputs = append(inputs, entry.Ref)
		}
	}
	paramsBody, err := canonical.Marshal(prepared)
	if err != nil {
		return zero, err
	}
	for _, size := range sizes {
		if size.Ref == params && size.Bytes != int64(len(paramsBody)) {
			return zero, ErrInvalid
		}
		if size.Ref == target && size.Bytes != int64(len(targetRaw)) {
			return zero, ErrInvalid
		}
	}
	inputBytes, err := exactSemanticInputBytes(params, inputs, sizes)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(targetRaw)
	if err != nil {
		return zero, err
	}
	profile, err := CompileAuthoredMaskTransformProfile(int64(prepared.Mask.Canvas.Width), int64(prepared.Mask.Canvas.Height), int64(image.Canvas.Width), int64(image.Canvas.Height), inputBytes, ram, scratch, output)
	if err != nil {
		return zero, err
	}
	task, err := TaskID(workflow, "mask.transform", 0)
	if err != nil {
		return zero, err
	}
	key, err := NodeKey("mask.transform", 0)
	if err != nil {
		return zero, err
	}
	closure, err := compileCaptureInventory(task, "mask.transform", prepared, params, owned)
	if err != nil {
		return zero, err
	}
	return FrozenAuthoredMaskTransformTask{task: task, key: key, params: params, profile: string(profile), closure: closure}, ctx.Err()
}
func (f FrozenAuthoredMaskTransformTask) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || !validRef(f.params) || f.closure.Open == nil || !json.Valid([]byte(f.profile)) {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: f.task, NodeKey: f.key, Operation: "mask.transform", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256, InputDigest: f.closure.InputDigest, ExecutionProfile: json.RawMessage(f.profile)}, nil
}
func PublishAuthoredMaskTransformTask(ctx context.Context, f FrozenAuthoredMaskTransformTask, p Publisher) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	if p == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	cursor, err := f.closure.Open(ctx)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	built, err := BuildInputClosure(ctx, f.task, cursor, p)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	return f.BindPublishedTransport(ctx, built)
}
func (f FrozenAuthoredMaskTransformTask) BindPublishedTransport(ctx context.Context, b Built) (PreparedSemanticTask, error) {
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	return bindFrozenTaskTransport(ctx, f.task, f.params, json.RawMessage(f.profile), f.key, f.closure, b)
}
func (f FrozenAuthoredMaskTransformTask) ParamsBytes() ([]byte, error) {
	if _, err := f.DraftTask(); err != nil {
		return nil, err
	}
	return append([]byte(nil), f.closure.ParamsJCS...), nil
}
