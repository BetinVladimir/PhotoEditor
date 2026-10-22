package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/v2input"
)

// CreateDocument is immutable publication intent, not an uploaded artifact.
type CreateDocument struct {
	Purpose string
	Ref     v2input.Ref
	Body    []byte
}
type CreateDocuments struct {
	Snapshot CreateDocument
	Presets  []CreateDocument
	Policy   CreateDocument
	Frozen   CreateDocument
	Params   CreateDocument
	Plan     PreparePlan
}

// CompileDocuments requires independently authorized recipe/source bindings.
// It owns every byte and never resolves latest recipes or performs object IO.
// SQL still must prove source inventory and closure before final admission.
func (i CreateIntent) CompileDocuments(ctx context.Context, whoSession uuid.UUID, catalog CreateCatalog, items []v2input.SessionSnapshotItem) (CreateDocuments, error) {
 return i.compileDocuments(ctx,whoSession,catalog,items,nil,false)
}
// CompileWatermarkDocuments keeps ordinary document identities and immutable
// bytes while requiring all frozen watermark pins before building prepare plan.
func(i CreateIntent)CompileWatermarkDocuments(ctx context.Context,whoSession uuid.UUID,catalog CreateCatalog,items []v2input.SessionSnapshotItem,pins []PreparedWatermarkPin)(CreateDocuments,error){
 return i.compileDocuments(ctx,whoSession,catalog,items,pins,true)
}
func(i CreateIntent)compileDocuments(ctx context.Context,whoSession uuid.UUID,catalog CreateCatalog,items []v2input.SessionSnapshotItem,pins []PreparedWatermarkPin,watermark bool)(CreateDocuments,error){
 docs,err:=i.projectCreateDocuments(ctx,whoSession,catalog,items);if err!=nil{return CreateDocuments{},err}
 configs:=make([]PresetContent,len(docs.Presets));for n,doc:=range docs.Presets{configs[n]=PresetContent{ID:catalog.Presets[n].ID,SHA:doc.Ref.SHA256,Ref:doc.Ref,Raw:doc.Body}}
 if watermark{
  scope:=WatermarkPinScope{Tenant:i.tenant,Project:i.request.ProjectID,Actor:i.actor,Parent:i.identity.Run}
  docs.Plan,err=CompileWatermarkPreparePlan(scope,whoSession,docs.Params.Ref,docs.Frozen.Ref,docs.Frozen.Body,docs.Snapshot.Body,configs,pins)
 }else{docs.Plan,err=CompilePreparePlan(i.identity.Workflow,i.request.ProjectID,whoSession,docs.Params.Ref,docs.Frozen.Ref,docs.Frozen.Body,docs.Snapshot.Body,configs)}
 if err!=nil{return CreateDocuments{},err};return docs,ctx.Err()
}
// projectCreateDocuments creates immutable selection values only. Its Plan is
// always zero; no task/closure/admission authority exists before pins are ready.
func(i CreateIntent)projectCreateDocuments(ctx context.Context,whoSession uuid.UUID,catalog CreateCatalog,items []v2input.SessionSnapshotItem)(CreateDocuments,error){
	invalid := func() (CreateDocuments, error) {
		return CreateDocuments{}, domain.NewError("DELIVERY_CREATE_SOURCE_INVALID", 409)
	}
	if err := ctx.Err(); err != nil {
		return CreateDocuments{}, err
	}
	if i.identity.Run == uuid.Nil || whoSession == uuid.Nil || catalog.Snapshot != i.request.SnapshotID || catalog.ItemCount != len(items) || len(items) < 1 || len(items) > 5000 {
		return invalid()
	}
	ids := append(append([]uuid.UUID{}, i.request.Policy.RequiredPresets...), i.request.Policy.OptionalPresets...)
	if len(catalog.Presets) != len(ids) {
		return invalid()
	}
	// Recompile the pinned snapshot. Matching inventory/recipe authority is a
	// separate SQL obligation; arbitrary client-supplied recipes cannot reach here.
	_, snapshotRaw, err := v2input.CompilePinnedSessionInputSnapshot(catalog.Snapshot, items)
	if err != nil {
		return invalid()
	}
	snapshot := createDocument("v30_delivery_snapshot", uuid.NewSHA1(i.identity.PrepareTask, []byte("delivery:snapshot:1")), snapshotRaw)
	docs := make([]CreateDocument, 0, len(ids))
	configs := make([]PresetContent, 0, len(ids))
	for index, preset := range catalog.Presets {
		if err := ctx.Err(); err != nil {
			return CreateDocuments{}, err
		}
		if (i.request.ExpectedPresetSHA != nil && i.request.ExpectedPresetSHA[preset.ID] != preset.SHA256) || preset.ID != ids[index] || !shaPattern.MatchString(preset.SHA256) || digest(preset.ConfigJCS) != preset.SHA256 {
			return invalid()
		}
		var config exportpreset.Config
		if json.Unmarshal(preset.ConfigJCS, &config) != nil || exportpreset.ValidateConfig(config) != nil {
			return invalid()
		}
		body, err := canonical.Marshal(config)
		if err != nil || digest(body) != preset.SHA256 {
			return invalid()
		}
		document := createDocument("v30_delivery_preset", uuid.NewSHA1(i.identity.Workflow, []byte("delivery:preset:"+preset.ID.String()+":"+preset.SHA256)), body)
		docs = append(docs, document)
		configs = append(configs, PresetContent{ID: preset.ID, SHA: preset.SHA256, Ref: document.Ref, Raw: document.Body})
	}
	frozenInput, frozenRaw, _, err := CompileFrozenInput(snapshot.Ref, snapshot.Body, i.Request().Policy, configs)
	if err != nil {
		return CreateDocuments{}, err
	}
	frozen := createDocument("v30_delivery_input", i.identity.FrozenInput, frozenRaw)
	policyRaw, err := canonical.Marshal(frozenInput.Policy)
	if err != nil {
		return invalid()
	}
	policy := createDocument("v30_delivery_policy", i.identity.Policy, policyRaw)
	paramsRaw, err := canonical.Marshal(PrepareParams{SchemaVersion: 1, Input: frozen.Ref})
	if err != nil {
		return invalid()
	}
	params := createDocument("task_params", i.identity.Params, paramsRaw)
	if err := ctx.Err(); err != nil {
		return CreateDocuments{}, err
	}
	return CreateDocuments{Snapshot: snapshot, Presets: docs, Policy: policy, Frozen: frozen, Params: params}, nil
}
func createDocument(purpose string, id uuid.UUID, raw []byte) CreateDocument {
	owned := append([]byte(nil), raw...)
	return CreateDocument{Purpose: purpose, Ref: v2input.Ref{ArtifactID: id, SHA256: digest(owned)}, Body: owned}
}
