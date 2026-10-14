package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ExposureQualificationPin is deployment-selected CPU qualification metadata.
// The caller must independently verify the exact report bytes and current SQL
// policy before admitting the child; a structurally valid pin is not authority.
type ExposureQualificationPin v2input.SemanticQualificationPin

type ExposureExecutionProfile struct {
	Backend          string                   `json:"backend"`
	RendererVersion  string                   `json:"renderer_version"`
	ResourceEstimate ExposureResourceEstimate `json:"resource_estimate"`
	ModelLockSHA     string                   `json:"model_lock_sha"`
	RuntimeLockSHA   string                   `json:"runtime_lock_sha"`
	DeviceProfileSHA string                   `json:"device_profile_sha"`
	DatasetSHA       string                   `json:"dataset_sha"`
	Qualification    ExposureQualificationPin `json:"qualification"`
}

func validExposureQualification(p ExposureQualificationPin) bool {
	validRef := func(r v2input.Ref) bool { return r.ArtifactID != uuid.Nil && shaPattern.MatchString(r.SHA256) }
	return p.PolicyID != uuid.Nil && p.QualificationID != uuid.Nil && p.IssuerID != uuid.Nil && p.Backend == "cpu" &&
		validRef(p.Report) && validRef(p.Calibration) && p.Report.ArtifactID != p.Calibration.ArtifactID &&
		shaPattern.MatchString(p.PolicySHA) && shaPattern.MatchString(p.ModelLockSHA) && shaPattern.MatchString(p.RuntimeLockSHA) &&
		shaPattern.MatchString(p.DeviceProfileSHA) && shaPattern.MatchString(p.DatasetSHA)
}

// FrozenExposureTask owns the complete profile, Params and input cursor.
// It cannot be dispatched before the review decision/child and transport
// inventory have been atomically committed and sealed by SQL admission.
type FrozenExposureTask struct {
	task, target uuid.UUID
	params       v2input.Ref
	profile      json.RawMessage
	closure      v2input.CaptureClosure
}

func CompileExposureTask(child uuid.UUID, draft ApprovedTaskDraft, dependencies v2input.ExposureDependencies,
	width, height int64, owner ExposureOwnerCosts, limits ExposureResourceEstimate, pin ExposureQualificationPin) (FrozenExposureTask, error) {
	var empty FrozenExposureTask
	if !validExposureQualification(pin) || draft.Kind != "local_exposure" {
		return empty, ErrInvalid
	}
	// Publication pins the original bytes, not a reserialized equivalent.
	sum := sha256.Sum256(draft.Body)
	if hex.EncodeToString(sum[:]) != draft.Params.SHA256 {
		return empty, ErrInvalid
	}
	task, closure, err := v2input.CompileExposureClosure(child, draft.Body, draft.Params, dependencies)
	if err != nil || task != draft.TaskID {
		return empty, ErrInvalid
	}
	var edit ApprovedEdit
	if decodeStrict(draft.Body, &edit) != nil || edit.TargetID != draft.TargetID {
		return empty, ErrInvalid
	}
	cost, err := CompileExposureResources(width, height, owner, limits)
	if err != nil {
		return empty, err
	}
	profile, err := canonical.Marshal(ExposureExecutionProfile{"cpu", "v2.2", cost, pin.ModelLockSHA, pin.RuntimeLockSHA, pin.DeviceProfileSHA, pin.DatasetSHA, pin})
	if err != nil {
		return empty, ErrInvalid
	}
	return FrozenExposureTask{task, draft.TargetID, draft.Params, profile, closure}, nil
}

func (f FrozenExposureTask) DraftTask() (v2input.DraftTask, error) {
	if f.task == uuid.Nil || f.target == uuid.Nil || f.closure.Open == nil || len(f.profile) == 0 {
		return v2input.DraftTask{}, ErrInvalid
	}
	return v2input.DraftTask{ID: f.task, NodeKey: "semantic.edit:" + f.target.String(), Operation: "semantic.local_exposure", OperationVersion: "1", ResourceClass: "cpu",
		ParamsArtifactID: f.params.ArtifactID, ParamsSHA: f.params.SHA256, InputDigest: f.closure.InputDigest, ExecutionProfile: append(json.RawMessage(nil), f.profile...)}, nil
}

type PreparedExposureTask struct {
	Graph     v2input.GraphTask
	Draft     v2input.DraftTask
	Params    v2input.Ref
	ParamsJCS json.RawMessage
}

// PublishExposureTask publishes through an authenticated scoped publisher,
// including the pre-child parent-review contour. The publisher enforces
// tenant/task ownership; this method neither creates a child nor seals/adopts
// it. Failed publication remains non-dispatchable.
func PublishExposureTask(ctx context.Context, frozen FrozenExposureTask, publisher v2input.Publisher) (PreparedExposureTask, error) {
	var empty PreparedExposureTask
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if publisher == nil {
		return empty, ErrInvalid
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return empty, err
	}
	cursor, err := frozen.closure.Open(ctx)
	if err != nil {
		return empty, err
	}
	built, err := v2input.BuildInputClosure(ctx, frozen.task, cursor, publisher)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PreparedExposureTask{Graph: v2input.GraphTask{ID: frozen.task, Built: built, Open: frozen.closure.Open, Direct: append([]v2input.Ref(nil), frozen.closure.Direct...)},
		Draft: draft, Params: frozen.params, ParamsJCS: append(json.RawMessage(nil), frozen.closure.ParamsJCS...)}, nil
}
