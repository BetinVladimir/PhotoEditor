package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// PreparePlan owns the exact proof and params bytes used for recovery. Scoped
// publication, a complete authorized dependency resolver, and sealing remain
// mandatory caller steps; compilation does not create an executable task.
type PreparePlan struct {
 watermarkAssemblyRoots []v2input.Entry
	task                uuid.UUID
	key                 string
	params              v2input.Ref
	paramsJCS, proofJCS []byte
	roots               []v2input.Entry
	workflow            uuid.UUID
	input               v2input.Ref
	documentBytes       int64
	preparation         Preparation
	items               []v2input.SessionSnapshotItem
	presets             []FrozenPreset
}

func CompilePreparePlan(workflow, project, session uuid.UUID, paramsRef, frozenRef v2input.Ref, frozenRaw, snapshotRaw []byte, configs []PresetContent) (PreparePlan, error) {
 return compilePreparePlan(workflow,project,session,paramsRef,frozenRef,frozenRaw,snapshotRaw,configs,false)
}
func compilePreparePlan(workflow,project,session uuid.UUID,paramsRef,frozenRef v2input.Ref,frozenRaw,snapshotRaw []byte,configs []PresetContent,watermark bool)(PreparePlan,error){
	var zero PreparePlan
	_, proof, params, err := compilePrepareProof(project, session, frozenRef, frozenRaw, snapshotRaw, configs,watermark)
	if err != nil {
		return zero, err
	}
	if paramsRef.ArtifactID == uuid.Nil || paramsRef.SHA256 != digest(params) {
		return zero, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, "v2.delivery.prepare", 0)
	if err != nil {
		return zero, err
	}
	key, err := v2input.NodeKey("v2.delivery.prepare", 0)
	if err != nil {
		return zero, err
	}
	var frozen FrozenInput
	var snapshot v2input.SessionInputSnapshot
	if json.Unmarshal(frozenRaw, &frozen) != nil || json.Unmarshal(snapshotRaw, &snapshot) != nil {
		return zero, v2input.ErrInvalid
	}
	roots := []v2input.Entry{{Ref: frozenRef, Contract: "FrozenDeliveryInput"}, {Ref: frozen.Snapshot, Contract: "InputSnapshot"}}
	for _, preset := range frozen.Presets {
		roots = append(roots, v2input.Entry{Ref: preset.Config, Contract: "ExportPresetConfig"})
	}
	for _, item := range snapshot.Items {
		roots = append(roots, v2input.Entry{Ref: item.WorkingImage, Contract: "WorkingImage"}, v2input.Entry{Ref: item.Recipe, Contract: "FrozenRenderRecipe"})
	}
	documentBytes := int64(len(frozenRaw) + len(snapshotRaw) + len(params))
	for _, config := range configs {
		documentBytes += int64(len(config.Raw))
	}
	preparation := Preparation{SchemaVersion: 1, Input: frozenRef, SnapshotDigest: frozen.SnapshotDigest, ItemCount: len(snapshot.Items), SlotCount: len(snapshot.Items) * len(frozen.Presets)}
	return PreparePlan{items: snapshot.Items, presets: frozen.Presets, preparation: preparation, documentBytes: documentBytes, task: task, key: key, params: paramsRef, paramsJCS: params, proofJCS: proof, roots: roots, workflow: workflow, input: frozenRef}, nil
}

func (p PreparePlan) TaskID() uuid.UUID      { return p.task }
func (p PreparePlan) NodeKey() string        { return p.key }
func (p PreparePlan) ParamsJCS() []byte      { return append([]byte(nil), p.paramsJCS...) }
func (p PreparePlan) ProofJCS() []byte       { return append([]byte(nil), p.proofJCS...) }
func (p PreparePlan) Roots() []v2input.Entry { return append([]v2input.Entry(nil), p.roots...) }

// InputClosure verifies that the resolver returned every independently compiled
// root. It pins the supplied transitive entries but cannot prove their physical
// bytes, project authority or completeness; admission must establish those.
func (p PreparePlan) InputClosure(resolved []v2input.Entry) (v2input.CaptureClosure, error) {
	if p.task == uuid.Nil || len(p.proofJCS) == 0 || digest(p.paramsJCS) != p.params.SHA256 {
		return v2input.CaptureClosure{}, v2input.ErrInvalid
	}
	return v2input.CompileDeliveryPrepareClosure(p.workflow, v2input.DeliveryPrepareParams{SchemaVersion: 1, Input: p.input}, p.params, p.roots, resolved)
}

// PublishInputs writes transport documents only after a blocked task shell has
// committed. Graph registration and sealing must subsequently occur in TX.
func (p PreparePlan) PublishInputs(ctx context.Context, resolved []v2input.Entry, publisher v2input.Publisher) (v2input.GraphTask, error) {
	if publisher == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	closure, err := p.InputClosure(resolved)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	cursor, err := closure.Open(ctx)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	built, err := v2input.BuildInputClosure(ctx, p.task, cursor, publisher)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	return v2input.GraphTask{ID: p.task, Built: built, Open: closure.Open, Direct: append([]v2input.Ref(nil), closure.Direct...)}, nil
}
