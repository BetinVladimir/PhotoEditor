package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// FrozenSemanticDefectTask owns the native worker Params/profile/input inventory.
// Catalog/source/model/protection authority and SQL admission are caller duties.
type FrozenSemanticDefectTask struct {
	task    uuid.UUID
	key     string
	params  Ref
	profile string
	closure CaptureClosure
}

func CompileSemanticDefectTask(ctx context.Context, workflow uuid.UUID, ordinal int, binding SessionVersionBinding, imageBody, paramsBody []byte, paramsRef Ref, entries []Entry, sizes []SemanticInputSize, costs SemanticQualifiedCosts, ram, vram, scratch, output int64) (FrozenSemanticDefectTask, error) {
	var zero FrozenSemanticDefectTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !validRef(paramsRef) || len(entries) < 3 || len(entries) > 4097 || len(imageBody) < 2 || len(imageBody) > 1048576 {
		return zero, ErrInvalid
	}
	task, err := TaskID(workflow, "defects.propose", ordinal)
	if err != nil {
		return zero, err
	}
	key, err := NodeKey("defects.propose", ordinal)
	if err != nil {
		return zero, err
	}
	imageDigest := sha256.Sum256(imageBody)
	if binding.Source.SHA256 != hex.EncodeToString(imageDigest[:]) {
		return zero, ErrInvalid
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return zero, ErrInvalid
	}
	metadata, err := ResolveSemanticSourceMetadata(binding, imageBody)
	if err != nil {
		return zero, err
	}
	params, err := canonical.Bytes(paramsBody)
	if err != nil || !bytes.Equal(params, paramsBody) {
		return zero, ErrInvalid
	}
	owned := append([]Entry(nil), entries...)
	allowed := map[string]bool{"Params": true, "WorkingImage": true, "FaceLandmarks": true, "MaskRef": true, "NpyArray": true}
	seen := map[uuid.UUID]bool{}
	inputRefs := make([]Ref, 0, len(entries)-1)
	nativeInputs := make([]review.ArtifactRef, 0, len(entries)-1)
	paramsCount := 0
	sourceFound := false
	pixelsFound := false
	for _, entry := range owned {
		if !validEntry(entry) || !allowed[entry.Contract] || seen[entry.Ref.ArtifactID] {
			return zero, ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = true
		if entry.Ref == paramsRef {
			if entry.Contract != "Params" {
				return zero, ErrInvalid
			}
			paramsCount++
			continue
		}
		if entry.Contract == "Params" {
			return zero, ErrInvalid
		}
		if entry.Ref == binding.Source {
			if entry.Contract != "WorkingImage" {
				return zero, ErrInvalid
			}
			sourceFound = true
		}
		if entry.Ref == metadata.Inputs.Pixels {
			if entry.Contract != "NpyArray" {
				return zero, ErrInvalid
			}
			pixelsFound = true
		}
		inputRefs = append(inputRefs, entry.Ref)
		nativeInputs = append(nativeInputs, review.ArtifactRef{ID: entry.Ref.ArtifactID, SHA256: entry.Ref.SHA256})
	}
	if paramsCount != 1 || !sourceFound || !pixelsFound {
		return zero, ErrInvalid
	}
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	if err := review.VerifyDefectProposalDeclaredInputs(review.ArtifactRef{ID: paramsRef.ArtifactID, SHA256: paramsRef.SHA256}, params, source, nativeInputs); err != nil {
		return zero, err
	}
	profile, err := CompileSemanticDefectProfileFromInputs(image.Canvas.Width, image.Canvas.Height, paramsRef, inputRefs, sizes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	closure, err := compileCaptureInventory(task, "defects.propose", json.RawMessage(params), paramsRef, owned)
	if err != nil {
		return zero, err
	}
	// Bootstrap refs stay bounded; the manifest and frozen cursor retain every input.
	closure.Direct = []Ref{paramsRef, binding.Source, metadata.Inputs.Pixels}
	sort.Slice(closure.Direct, func(i, j int) bool {
		return bytes.Compare(closure.Direct[i].ArtifactID[:], closure.Direct[j].ArtifactID[:]) < 0
	})
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return FrozenSemanticDefectTask{task: task, key: key, params: paramsRef, profile: string(profile), closure: closure}, nil
}

func (f FrozenSemanticDefectTask) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || !validRef(f.params) || f.closure.Open == nil || !json.Valid([]byte(f.profile)) {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: f.task, NodeKey: f.key, Operation: "defects.propose", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256, InputDigest: f.closure.InputDigest, ExecutionProfile: json.RawMessage(f.profile)}, nil
}

// Publishes immutable transport for the frozen task; SQL must separately create
// and register the blocked task before activation.
func PublishSemanticDefectTask(ctx context.Context, frozen FrozenSemanticDefectTask, publisher Publisher) (PreparedSemanticTask, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSemanticTask{}, err
	}
	if publisher == nil {
		return PreparedSemanticTask{}, ErrInvalid
	}
	if _, err := frozen.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	cursor, err := frozen.closure.Open(ctx)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	built, err := BuildInputClosure(ctx, frozen.task, cursor, publisher)
	if err != nil {
		return PreparedSemanticTask{}, err
	}
	return frozen.BindPublishedTransport(ctx, built)
}

func (f FrozenSemanticDefectTask) BindPublishedTransport(ctx context.Context, built Built) (PreparedSemanticTask, error) {
	if _, err := f.DraftTask(); err != nil {
		return PreparedSemanticTask{}, err
	}
	return bindFrozenTaskTransport(ctx, f.task, f.params, json.RawMessage(f.profile), f.key, f.closure, built)
}
