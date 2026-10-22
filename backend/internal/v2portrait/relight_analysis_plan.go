package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type RelightAnalysisRuntime struct {
	Backend          string                `json:"backend"`
	ModelLockSHA     string                `json:"model_lock_sha"`
	RuntimeLockSHA   string                `json:"runtime_lock_sha"`
	DeviceProfileSHA string                `json:"device_profile_sha"`
	Qualification    v2input.Ref           `json:"qualification"`
	Resources        LocalResourceEstimate `json:"resource_estimate"`
}
type RelightAnalysisPlanRequest struct {
	Tenant, Project, Actor, Session, Snapshot uuid.UUID
	Key, Operation                            string
	Base                                      v2input.SessionVersionBinding
	SourceRaw                                 []byte
	Runtime                                   RelightAnalysisRuntime
	Capacity                                  LocalResourceEstimate
}
type RelightAnalysisPlanSnapshot struct {
	SourceJCS      string          `json:"source_jcs"`
	Run            uuid.UUID       `json:"run_id"`
	Workflow       uuid.UUID       `json:"workflow_id"`
	Task           uuid.UUID       `json:"task_id"`
	ParamsDocument uuid.UUID       `json:"params_document_id"`
	Operation      string          `json:"operation"`
	Params         json.RawMessage `json:"params"`
	ParamsSHA      string          `json:"params_sha"`
	Profile        json.RawMessage `json:"execution_profile"`
	Inputs         []v2input.Entry `json:"inputs"`
	ReservedBytes  int64           `json:"reserved_bytes"`
	Config         json.RawMessage `json:"config"`
}

// Plan owns the frozen compiler result; never a live qualification/admission token.
type RelightAnalysisPlan struct{ snapshot RelightAnalysisPlanSnapshot }

func (p RelightAnalysisPlan) Snapshot() RelightAnalysisPlanSnapshot {
	result := p.snapshot
	result.Params = append(json.RawMessage(nil), result.Params...)
	result.Profile = append(json.RawMessage(nil), result.Profile...)
	result.Config = append(json.RawMessage(nil), result.Config...)
	result.Inputs = append([]v2input.Entry(nil), result.Inputs...)
	return result
}
func CompileRelightAnalysisPlan(ctx context.Context, r RelightAnalysisPlanRequest) (RelightAnalysisPlan, error) {
	var zero RelightAnalysisPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if r.Tenant == uuid.Nil || r.Project == uuid.Nil || r.Actor == uuid.Nil || r.Session == uuid.Nil || r.Snapshot == uuid.Nil || !idempotency.ValidKey(r.Key) || len(r.SourceRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	runtime := r.Runtime
	if runtime.Backend != "cpu" && runtime.Backend != "mps" && runtime.Backend != "cuda" || !validRelightTransportRef(runtime.Qualification) {
		return zero, v2input.ErrInvalid
	}
	for _, sha := range []string{runtime.ModelLockSHA, runtime.RuntimeLockSHA, runtime.DeviceProfileSHA} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	source, err := VerifyDonorWorkingImage(r.Base, r.SourceRaw)
	if err != nil {
		return zero, err
	}
	minimum, err := RelightAnalysisMinimumResources(r)
	if err != nil {
		return zero, err
	}
	cost := runtime.Resources
	pixels := source.Canvas.Width * source.Canvas.Height
	if cost.PixelCount != pixels || cost.RAMBytes < minimum.RAMBytes || cost.ScratchBytes < minimum.ScratchBytes || cost.OutputBytes < minimum.OutputBytes || cost.VRAMBytes < 0 || runtime.Backend == "cpu" && cost.VRAMBytes != 0 || runtime.Backend != "cpu" && cost.VRAMBytes < 1 {
		return zero, v2input.ErrInvalid
	}
	need := []int64{cost.PixelCount, cost.RAMBytes, cost.ScratchBytes, cost.OutputBytes, cost.VRAMBytes}
	have := []int64{r.Capacity.PixelCount, r.Capacity.RAMBytes, r.Capacity.ScratchBytes, r.Capacity.OutputBytes, r.Capacity.VRAMBytes}
	for i, n := range need {
		if n > 9007199254740991 || have[i] < n || have[i] > 9007199254740991 {
			return zero, v2input.ErrInvalid
		}
	}
	params, paramsSHA, err := RelightAnalysisParams(r.Operation, r.Base.Source, runtime.ModelLockSHA)
	if err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(r.Tenant, []byte("V28.Relight.Prepare:"+r.Project.String()+":"+r.Actor.String()+":"+r.Operation+":"+r.Key))
	workflow, _ := v2input.WorkflowID(run)
	task, _ := v2input.TaskID(workflow, r.Operation, 0)
	document := uuid.NewSHA1(task, []byte("params"))
	entries := []v2input.Entry{{Ref: r.Base.Source, Contract: "WorkingImage"}, {Ref: runtime.Qualification, Contract: "QualificationReport"}, {Ref: v2input.Ref{ArtifactID: document, SHA256: paramsSHA}, Contract: "Params"}, {Ref: v2input.Ref{ArtifactID: source.Pixels.ID, SHA256: source.Pixels.SHA256}, Contract: "NpyArray"}}
	if source.Alpha != nil {
		entries = append(entries, v2input.Entry{Ref: v2input.Ref{ArtifactID: source.Alpha.ID, SHA256: source.Alpha.SHA256}, Contract: "NpyArray"})
	}
	var chain struct {
		Steps []struct {
			Kind    string       `json:"kind"`
			Inverse *v2input.Ref `json:"inverse_field"`
		} `json:"steps"`
	}
	if json.Unmarshal(source.TransformChain, &chain) != nil {
		return zero, v2input.ErrInvalid
	}
	for _, step := range chain.Steps {
		if step.Kind == "dense_warp" {
			if step.Inverse == nil {
				return zero, v2input.ErrInvalid
			}
			entries = append(entries, v2input.Entry{Ref: *step.Inverse, Contract: "NpyArray"})
		}
	}
	seen := map[uuid.UUID]v2input.Entry{}
	unique := []v2input.Entry{}
	for _, entry := range entries {
		if !validRelightTransportRef(entry.Ref) {
			return zero, v2input.ErrInvalid
		}
		if old, exists := seen[entry.Ref.ArtifactID]; exists {
			if old != entry {
				return zero, v2input.ErrInvalid
			}
			continue
		}
		seen[entry.Ref.ArtifactID] = entry
		unique = append(unique, entry)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].Ref.ArtifactID.String() < unique[j].Ref.ArtifactID.String() })
	profile, err := canonical.Marshal(struct {
		RelightAnalysisRuntime
		RendererVersion string `json:"renderer_version"`
	}{runtime, "v2.2"})
	if err != nil {
		return zero, err
	}
	config, err := canonical.Marshal(map[string]any{"schema_version": 1, "tenant_id": r.Tenant, "project_id": r.Project, "actor_id": r.Actor, "auth_session_id": r.Session, "snapshot_id": r.Snapshot, "operation": r.Operation, "base": r.Base, "params_sha": paramsSHA, "execution_profile": json.RawMessage(profile), "inputs": unique})
	if err != nil {
		return zero, err
	}
	if cost.OutputBytes > 9007199254740991-(8<<20) {
		return zero, v2input.ErrInvalid
	}
	return RelightAnalysisPlan{snapshot: RelightAnalysisPlanSnapshot{SourceJCS: string(r.SourceRaw), Run: run, Workflow: workflow, Task: task, ParamsDocument: document, Operation: r.Operation, Params: params, ParamsSHA: paramsSHA, Profile: profile, Inputs: unique, ReservedBytes: cost.OutputBytes + (8 << 20), Config: config}}, nil
}
