package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

// AuthoredNativeTaskPlan is an actual native operation child, never a blend or
// an old producer tuple promoted to authored readiness. No result is nominated.
type AuthoredNativeTaskPlan struct {
	Run, Workflow uuid.UUID
	Task          v2input.DraftTask
	ParamsRef     v2input.Ref
	Entries       []v2input.Entry
	OutputBytes   int64
	config        []byte
	fingerprint   string
}

func authoredNativeQualificationRequest(m AuthoredNativeMaterialization) ([]byte, error) {
	return canonical.Marshal(struct {
		Authored string                    `json:"authored_params_jcs"`
		Context  AuthoredInstructionSource `json:"context"`
		Params   string                    `json:"params_jcs"`
	}{string(m.authored), m.source, string(m.selected.Params.Body)})
}
func authoredNativeIdentities(m AuthoredNativeMaterialization) (uuid.UUID, uuid.UUID, uuid.UUID, error) {
	if m.source.Tenant == uuid.Nil || m.source.Actor == uuid.Nil || m.source.Parent == uuid.Nil || m.source.Instruction.Identity == uuid.Nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	// The source program/instruction deterministically selects one proposal;
	// current prefix mismatches cannot replace a sealed descriptor in SQL.
	run := uuid.NewSHA1(m.source.Tenant, []byte("V2.Admit:"+m.source.Actor.String()+":authored-native/"+m.source.Instruction.Identity.String()))
	workflow, e := v2input.WorkflowID(run)
	if e != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, e
	}
	task, e := v2input.TaskID(workflow, m.selected.Operation, 0)
	return run, workflow, task, e
}
func compileAuthoredNativePlan(m AuthoredNativeMaterialization, paramsRef v2input.Ref, evidence v2draft.TaskInputEvidence, profile []byte, output int64, qualification json.RawMessage, sourceBodies [][]byte) (AuthoredNativeTaskPlan, error) {
	var zero AuthoredNativeTaskPlan
	if len(qualification) < 2 || len(m.proof) < 2 || !validLayerArtifactRef(paramsRef) || paramsRef.SHA256 != m.selected.Params.SHA256 || output < 1 {
		return zero, v2input.ErrInvalid
	}
	current, e := MaterializeAuthoredInstruction(m.source, m.authored)
	if e != nil || current.Operation != m.selected.Operation || !bytes.Equal(current.Params.Body, m.selected.Params.Body) {
		return zero, v2input.ErrInvalid
	}
	run, workflow, task, e := authoredNativeIdentities(m)
	if e != nil {
		return zero, e
	}
	entries := append([]v2input.Entry(nil), evidence.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	if len(entries) < 1 || len(entries) > 4096 || len(evidence.Occurrences) < len(entries) || validateRenderClosureDocuments(evidence.Documents, entries) != nil {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	required := map[v2input.Entry]bool{{Ref: paramsRef, Contract: "Params"}: false, {Ref: m.source.Source.Source, Contract: "WorkingImage"}: false}
	for _, x := range entries {
		if !validLayerArtifactRef(x.Ref) || seen[x.Ref.ArtifactID] || x.Contract == "" {
			return zero, v2input.ErrInvalid
		}
		seen[x.Ref.ArtifactID] = true
		if _, ok := required[x]; ok {
			required[x] = true
		}
	}
	for _, found := range required {
		if !found {
			return zero, v2input.ErrInvalid
		}
	}
	digest, e := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, m.selected.Operation, "1", entries})
	if e != nil {
		return zero, e
	}
	key, e := v2input.NodeKey(m.selected.Operation, 0)
	if e != nil {
		return zero, e
	}
	type inputObject struct {
		Ref     v2input.Ref `json:"ref"`
		Key     string      `json:"object_key"`
		Version string      `json:"object_version"`
		Bytes   int64       `json:"bytes"`
	}
	objects := make([]inputObject, 0, len(entries))
	var inputBytes int64
	for _, entry := range entries {
		var found *inputObject
		for _, occurrence := range evidence.Occurrences {
			if occurrence.Node.Ref == entry.Ref {
				current := inputObject{Ref: entry.Ref, Key: occurrence.Object.Key, Version: occurrence.Object.VersionID, Bytes: occurrence.Object.Bytes}
				if current.Bytes < 1 || current.Key == "" || current.Version == "" || current.Version == "null" || found != nil && *found != current {
					return zero, v2input.ErrInvalid
				}
				found = &current
			}
		}
		if found == nil || inputBytes > 9007199254740991-found.Bytes {
			return zero, v2input.ErrInvalid
		}
		objects = append(objects, *found)
		inputBytes += found.Bytes
	}
	sources := make([]string, len(sourceBodies))
	for i, body := range sourceBodies {
		sources[i] = string(body)
	}
	config, e := canonical.Marshal(map[string]any{"project_id": m.source.Project, "authored_native": map[string]any{"task_id": task, "entry_count": len(entries), "entries": entries, "objects": objects, "input_bytes": inputBytes, "sources": sources, "params": paramsRef, "input_digest": digest, "profile": json.RawMessage(profile), "qualification": qualification}, "authored_phase_proposal": map[string]any{"parent_run_id": m.source.Parent, "ordinal": m.source.Instruction.Ordinal, "instruction_id": m.source.Instruction.Identity, "program_sha256": m.source.ProgramSHA, "predecessor_sha256": m.source.PredecessorSHA, "input_source": m.source.Source, "authored_params": m.selected.Params.Authored, "authored_params_jcs": string(m.authored), "params_jcs": string(m.selected.Params.Body)}})
	if e != nil {
		return zero, e
	}
	resourceClass, e := authoredNativeResourceClass(m.selected.Operation)
	if e != nil {
		return zero, e
	}
	plan := AuthoredNativeTaskPlan{Run: run, Workflow: workflow, Task: v2input.DraftTask{ID: task, NodeKey: key, Operation: m.selected.Operation, OperationVersion: "1", ResourceClass: resourceClass, ParamsArtifactID: paramsRef.ArtifactID, ParamsSHA: paramsRef.SHA256, InputDigest: digest, ExecutionProfile: append(json.RawMessage(nil), profile...)}, ParamsRef: paramsRef, Entries: entries, OutputBytes: output, config: config}
	plan.fingerprint, e = canonical.Hash(struct {
		Plan   AuthoredNativeTaskPlan `json:"plan"`
		Config json.RawMessage        `json:"config"`
	}{plan, config})
	return plan, e
}
func compileAuthoredMultiProposal(ctx context.Context, m AuthoredNativeMaterialization, paramsRef v2input.Ref, inputs v2draft.TaskInputEvidence, sources [][]byte, qualified v2optical.VerifiedMultiQualification, capacity v2optical.MultiResourceEstimate) (AuthoredNativeTaskPlan, error) {
	if e := ctx.Err(); e != nil {
		return AuthoredNativeTaskPlan{}, e
	}
	if m.selected.Operation != "optical.panorama" && m.selected.Operation != "optical.focus_stack" {
		return AuthoredNativeTaskPlan{}, v2input.ErrInvalid
	}
	actual, e := v2optical.CompileMultiTaskPlan(ctx, m.selected.Operation, m.selected.Params.Body, sources, qualified, capacity)
	if e != nil {
		return AuthoredNativeTaskPlan{}, e
	}
	if actual.ParamsSHA != paramsRef.SHA256 {
		return AuthoredNativeTaskPlan{}, v2input.ErrInvalid
	}
	present := map[v2input.Ref]bool{}
	for _, entry := range inputs.Entries {
		present[entry.Ref] = true
	}
	for _, ref := range actual.DirectInputs {
		if !present[ref] {
			return AuthoredNativeTaskPlan{}, v2input.ErrInvalid
		}
	}

	// Preserve the actual native compiler's qualified CPU compute profile;
	// independently account for the exact packed typed input index below.
	profile, e := authoredNativeIndexedProfile(actual.Profile, len(inputs.Entries))
	if e != nil {
		return AuthoredNativeTaskPlan{}, e
	}
	pin, e := qualified.Pin()
	if e != nil {
		return AuthoredNativeTaskPlan{}, e
	}
	authority, e := canonical.Marshal(map[string]any{"kind": "multi", "pin": pin})
	if e != nil {
		return AuthoredNativeTaskPlan{}, e
	}
	return compileAuthoredNativePlan(m, paramsRef, inputs, profile, actual.ReservedBytes, authority, sources)
}
func authoredNativeIndexedProfile(raw []byte, count int) ([]byte, error) {
	if count < 1 || count > 4096 {
		return nil, v2input.ErrInvalid
	}
	var p map[string]any
	if json.Unmarshal(raw, &p) != nil {
		return nil, v2input.ErrInvalid
	}
	if _, exists := p["input_index_scratch_bytes"]; exists {
		return nil, v2input.ErrInvalid
	}
	resources, ok := p["resource_estimate"].(map[string]any)
	if !ok {
		return nil, v2input.ErrInvalid
	}
	scratch, ok := resources["scratch_bytes"].(float64)
	if !ok || math.IsNaN(scratch) || math.IsInf(scratch, 0) || scratch < 0 || math.Trunc(scratch) != scratch || scratch > 9007199254740991-(16<<20) {
		return nil, v2input.ErrInvalid
	}
	index := int64(16 << 20)
	if int64(count)*512 > index {
		index = int64(count) * 512
	}
	resources["scratch_bytes"] = int64(scratch) + index
	p["input_index_scratch_bytes"] = index
	p["renderer_version"] = "v2.2"
	p["contracts_sha"] = AuthoredGeometrySchemasSHA
	return canonical.Marshal(p)
}
func compileAuthoredUpscaleProposal(ctx context.Context, m AuthoredNativeMaterialization, paramsRef v2input.Ref, inputs v2draft.TaskInputEvidence, imageRaw []byte, inputBytes int64, qualified AuthoredGeometryNativeQualification) (AuthoredNativeTaskPlan, error) {
	zero := AuthoredNativeTaskPlan{}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	request, e := authoredNativeQualificationRequest(m)
	if e != nil || qualified.tenant != m.source.Tenant || qualified.request != string(request) || m.selected.Operation != "image.upscale" || inputBytes < 1 {
		return zero, v2qualification.ErrUnqualified
	}
	var expected v2qualification.Expected
	if json.Unmarshal([]byte(qualified.expected), &expected) != nil || (expected.Backend == "cpu" && qualified.costs.VRAMBytes != 0) {
		return zero, v2qualification.ErrUnqualified
	}
	var p struct {
		Image   v2input.Ref `json:"image"`
		Scale   int         `json:"scale"`
		Model   string      `json:"model_id"`
		Lock    string      `json:"model_lock_id"`
		Color   string      `json:"color_adapter_id"`
		Tile    int         `json:"tile"`
		Overlap int         `json:"overlap"`
	}
	if decodeAuthoredParams(m.selected.Params.Body, &p) != nil || p.Image != m.source.Source.Source || p.Lock != expected.ModelLockSHA {
		return zero, v2input.ErrInvalid
	}
	sum := sha256.Sum256(imageRaw)
	if hex.EncodeToString(sum[:]) != m.source.Source.Source.SHA256 {
		return zero, v2input.ErrInvalid
	}
	image, e := review.ParseWorkingImage(imageRaw)
	if e != nil {
		return zero, e
	}
	if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" {
		return zero, v2input.ErrInvalid
	}
	pixels := int64(image.Canvas.Width) * int64(image.Canvas.Height)
	if pixels < 1 || (p.Scale != 2 && p.Scale != 4) || pixels > 200000000/int64(p.Scale*p.Scale) {
		return zero, v2input.ErrInvalid
	}
	outPixels := pixels * int64(p.Scale*p.Scale)
	bpp := int64(12)
	if image.Alpha != nil {
		bpp += 2
	}
	output := outPixels*bpp + 2*1048576
	max := func(a, b int64) int64 {
		if a > b {
			return a
		}
		return b
	}
	ram := int64(2 << 30)
	if image.Alpha != nil {
		ram = max(ram, (768<<20)+outPixels*8)
	}
	ram = max(ram, qualified.costs.RAMBytes)
	vram := int64(0)
	if expected.Backend != "cpu" {
		vram = 1 << 30
	}
	vram = max(vram, qualified.costs.VRAMBytes)
	profile, e := canonical.Marshal(map[string]any{"backend": expected.Backend, "precision": "float32", "tile_size": 256, "model_lock_id": p.Lock, "resource_estimate": map[string]int64{"ram_bytes": ram, "vram_bytes": vram, "scratch_bytes": max(inputBytes+output+4*1048576, qualified.costs.ScratchBytes), "output_bytes": output, "pixel_count": outPixels}})
	if e != nil {
		return zero, e
	}
	profile, e = authoredNativeIndexedProfile(profile, len(inputs.Entries))
	if e != nil {
		return zero, e
	}
	authority, e := canonical.Marshal(map[string]any{"kind": "upscale", "expected": json.RawMessage(qualified.expected), "report": qualified.report, "report_jcs": qualified.reportJCS})
	if e != nil {
		return zero, e
	}
	return compileAuthoredNativePlan(m, paramsRef, inputs, profile, output, authority, [][]byte{imageRaw})
}

func (p AuthoredNativeTaskPlan) validateFrozen() error {
	fingerprint, e := canonical.Hash(struct {
		Plan   AuthoredNativeTaskPlan `json:"plan"`
		Config json.RawMessage        `json:"config"`
	}{p, p.config})
	if e != nil || fingerprint != p.fingerprint || len(p.fingerprint) != 64 {
		return v2input.ErrInvalid
	}
	return nil
}

func authoredNativeResourceClass(operation string) (string, error) {
	switch operation {
	case "image.upscale":
		return "gpu", nil
	case "optical.panorama", "optical.focus_stack":
		return "cpu", nil
	default:
		return "", v2input.ErrInvalid
	}
}
