package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type LensTaskRequest struct {
	Workflow                            uuid.UUID
	Ordinal                             int
	Params                              OpticalLensParams
	ParamsRef                           Ref
	Original                            LensOriginalBinding
	OriginalRef                         Ref
	ProfileJSON                         json.RawMessage
	SourceJSON                          json.RawMessage
	OriginalBytes                       int64
	RAMLimit, ScratchLimit, OutputLimit int64
}
type PreparedLensTask struct {
	Graph       GraphTask
	Params      Ref
	ParamsJCS   json.RawMessage
	Profile     json.RawMessage
	InputDigest string
	NodeKey     string
}

// FrozenLensTask separates validation from transport publication so the blocked
// shell can commit before scoped input documents are written.
type FrozenLensTask struct {
	task     uuid.UUID
	key      string
	params   Ref
	profile  json.RawMessage
	closure  CaptureClosure
	original LensOriginalBinding
}

func (f FrozenLensTask) DraftTask() (DraftTask, error) {
	if f.task == uuid.Nil || !validRef(f.params) || f.closure.Open == nil || !json.Valid(f.profile) {
		return DraftTask{}, ErrInvalid
	}
	return DraftTask{ID: f.task, NodeKey: f.key, Operation: "optical.lens", OperationVersion: "1", ResourceClass: "cpu",
		ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256, InputDigest: f.closure.InputDigest,
		ExecutionProfile: append(json.RawMessage(nil), f.profile...)}, nil
}

// CompileLensTask validates frozen source and dependency bytes without writes.
// Params and original binding refs must already exist in their authorized scope.
func CompileLensTask(request LensTaskRequest) (FrozenLensTask, error) {
	var result FrozenLensTask

	if len(request.SourceJSON) < 2 || len(request.SourceJSON) > 1048576 {
		return result, ErrInvalid
	}
	digest := sha256.Sum256(request.SourceJSON)
	if hex.EncodeToString(digest[:]) != request.Params.Base.Source.SHA256 {
		return result, ErrInvalid
	}
	image, err := review.ParseWorkingImage(request.SourceJSON)
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" ||
		image.Canvas.SourceSHA256 != request.Original.Original.SHA256 {
		return result, ErrInvalid
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != request.Params.Base.CanvasHash {
		return result, ErrInvalid
	}
	source := CaptureImageInputs{Pixels: Ref{image.Pixels.ID, image.Pixels.SHA256}}
	if image.Alpha != nil {
		alpha := Ref{image.Alpha.ID, image.Alpha.SHA256}
		source.Alpha = &alpha
	}
	var chain struct {
		Steps []struct {
			Kind         string `json:"kind"`
			InverseField *Ref   `json:"inverse_field"`
		} `json:"steps"`
	}
	if json.Unmarshal(image.TransformChain, &chain) != nil {
		return result, ErrInvalid
	}
	geometry := []Ref{}
	for _, step := range chain.Steps {
		if step.Kind == "dense_warp" {
			if step.InverseField == nil {
				return result, ErrInvalid
			}
			geometry = append(geometry, *step.InverseField)
		}
	}

	profileDigest := sha256.Sum256(request.ProfileJSON)
	if hex.EncodeToString(profileDigest[:]) != request.Params.Profile.SHA256 {
		return result, ErrInvalid
	}
	lens, err := ParseLensProfile(request.ProfileJSON)
	if err != nil || math.Abs(lens.FocalMM-request.Params.FocalMM) > 1e-6 || math.Abs(lens.Aperture-request.Params.Aperture) > 1e-6 {
		return result, ErrInvalid
	}
	task, err := TaskID(request.Workflow, "optical.lens", request.Ordinal)
	if err != nil {
		return result, err
	}
	profile, err := CompileLensProfile(int64(image.Canvas.Width), int64(image.Canvas.Height), request.Params.Strength,
		request.OriginalBytes, request.RAMLimit, request.ScratchLimit, request.OutputLimit)
	if err != nil {
		return result, err
	}
	closure, err := CompileLensClosure(task, request.Params, request.ParamsRef, source, geometry,
		lens.Qualification, request.Original, request.OriginalRef)
	if err != nil {
		return result, err
	}

	key, err := NodeKey("optical.lens", request.Ordinal)
	if err != nil {
		return result, err
	}
	return FrozenLensTask{task: task, key: key, params: request.ParamsRef, profile: profile, closure: closure, original: request.Original}, nil
}

// PublishLensTask runs after the corresponding blocked draft shell commits.
// Publication changes no database state; Graph must be registered/sealed in TX.
func PublishLensTask(ctx context.Context, frozen FrozenLensTask, publisher Publisher) (PreparedLensTask, error) {
	var result PreparedLensTask
	if publisher == nil || ctx.Err() != nil {
		return result, ErrInvalid
	}
	if _, err := frozen.DraftTask(); err != nil {
		return result, err
	}
	cursor, err := frozen.closure.Open(ctx)
	if err != nil {
		return result, err
	}
	built, err := BuildInputClosure(ctx, frozen.task, cursor, publisher)
	if err != nil {
		return result, err
	}
	return PreparedLensTask{Graph: GraphTask{frozen.task, built, frozen.closure.Open, append([]Ref(nil), frozen.closure.Direct...)},
		Params: frozen.params, ParamsJCS: append(json.RawMessage(nil), frozen.closure.ParamsJCS...),
		Profile: append(json.RawMessage(nil), frozen.profile...), InputDigest: frozen.closure.InputDigest, NodeKey: frozen.key}, nil
}

// PrepareLensTask is the convenience path for an already-created blocked shell.
func PrepareLensTask(ctx context.Context, request LensTaskRequest, publisher Publisher) (PreparedLensTask, error) {
	frozen, err := CompileLensTask(request)
	if err != nil {
		return PreparedLensTask{}, err
	}
	return PublishLensTask(ctx, frozen, publisher)
}

// LensDocumentPlan is embedded in the immutable run configuration before any
// lens document reservation. SHA and JCS repeat the exact intended content.
type LensDocumentPlan struct {
	Profile     json.RawMessage     `json:"profile"`
	TaskID      uuid.UUID           `json:"task_id"`
	Params      OpticalLensParams   `json:"params"`
	ParamsSHA   string              `json:"params_sha"`
	ParamsJCS   string              `json:"params_jcs"`
	Original    LensOriginalBinding `json:"original"`
	OriginalSHA string              `json:"original_sha"`
	OriginalJCS string              `json:"original_jcs"`
}

func (f FrozenLensTask) DocumentPlan() (LensDocumentPlan, error) {
	var result LensDocumentPlan
	if _, err := f.DraftTask(); err != nil {
		return result, err
	}
	var params OpticalLensParams
	if json.Unmarshal(f.closure.ParamsJCS, &params) != nil {
		return result, ErrInvalid
	}
	original, err := canonical.Marshal(f.original)
	if err != nil {
		return result, err
	}
	originalSHA, err := canonical.Hash(f.original)
	if err != nil {
		return result, err
	}
	return LensDocumentPlan{Profile: append(json.RawMessage(nil), f.profile...), TaskID: f.task, Params: params, ParamsSHA: f.params.SHA256, ParamsJCS: string(f.closure.ParamsJCS), Original: f.original, OriginalSHA: originalSHA, OriginalJCS: string(original)}, nil
}
