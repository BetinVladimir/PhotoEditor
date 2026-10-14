package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/v2input"
)

type expansionPlanPayload struct {
	Proof  PrepareProof `json:"proof"`
	Params v2input.Ref  `json:"params"`
	Input  v2input.Ref  `json:"input"`
}

// LoadPlan revalidates current SQL source authority before reading immutable
// proof. Compilation owns every byte and repeats snapshot/config/hash semantics.
// A returned plan is not authority to write: every child admission/cursor commit
// must check the lease again in its own transaction.
func (s ExpansionLeaseStore) LoadPlan(ctx context.Context, lease ExpansionLease) (PreparePlan, error) {
 return s.loadPlan(ctx,lease,nil,nil)
}
type WatermarkExpansionPinReader interface{Read(context.Context,WatermarkPinScope,uuid.UUID,uuid.UUID,[]byte)(FrozenWatermarkSource,error)}
// LoadWatermarkPlan requires external run actor/project scope and a fenced pin
// reader; it restores all selected watermark pins before compiling the plan.
func(s ExpansionLeaseStore)LoadWatermarkPlan(ctx context.Context,lease ExpansionLease,scope WatermarkPinScope,pins WatermarkExpansionPinReader)(PreparePlan,error){
 if err:=ctx.Err();err!=nil{return PreparePlan{},err}
 if pins==nil||scope.Tenant!=lease.Tenant||scope.Parent!=lease.Run||scope.Actor==uuid.Nil||scope.Project==uuid.Nil{return PreparePlan{},v2input.ErrInvalid}
 return s.loadPlan(ctx,lease,&scope,pins)
}
func(s ExpansionLeaseStore)loadPlan(ctx context.Context,lease ExpansionLease,scope *WatermarkPinScope,pinReader WatermarkExpansionPinReader)(PreparePlan,error){
	var zero PreparePlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil {
		return zero, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	if !lease.valid() {
		return zero, domain.NewError("DELIVERY_LEASE_INVALID", 422)
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_expansion_plan($1,$2)`, lease.Expansion, lease.Token).Scan(&raw); err != nil {
		return zero, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return zero, domain.NewError("DELIVERY_LEASE_STALE", 409)
	}
	if len(raw) > 64<<20 {
		return zero, v2input.ErrInvalid
	}
	var payload expansionPlanPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF {
		return zero, v2input.ErrInvalid
	}
	var frozen FrozenInput
	if json.Unmarshal([]byte(payload.Proof.FrozenInputJCS), &frozen) != nil {
		return zero, v2input.ErrInvalid
	}
	byPreset := make(map[uuid.UUID]FrozenPreset, len(frozen.Presets))
	for _, preset := range frozen.Presets {
		if _, exists := byPreset[preset.PresetID]; exists {
			return zero, v2input.ErrInvalid
		}
		byPreset[preset.PresetID] = preset
	}
	configs := make([]PresetContent, 0, len(payload.Proof.ConfigsJCS))
	for _, config := range payload.Proof.ConfigsJCS {
		preset, exists := byPreset[config.PresetID]
		if !exists {
			return zero, v2input.ErrInvalid
		}
		configs = append(configs, PresetContent{ID: config.PresetID, SHA: preset.PresetSHA, Ref: preset.Config, Raw: []byte(config.ConfigJCS)})
	}
 var plan PreparePlan
 var pins []PreparedWatermarkPin
 if scope!=nil{
  if scope.Project!=payload.Proof.ProjectID{return zero,v2input.ErrInvalid}
  for _,item:=range configs{
   config,err:=exportpreset.DecodeFrozenConfig(item.Raw,item.SHA);if err!=nil{return zero,err};if config.Watermark==nil{continue}
   exact:=*scope;exact.Preset=item.ID;exact.PresetSHA=item.SHA
   pin,err:=pinReader.Read(ctx,exact,lease.Expansion,lease.Token,item.Raw);if err!=nil{return zero,err}
   pins=append(pins,PreparedWatermarkPin{Preset:item.ID,PresetSHA:item.SHA,Pin:pin})
  }
  if len(pins)==0{
   // Every config was decoded above and none selected watermark. This is an
   // explicit ordinary plan, never fallback after a refused pin/compiler.
   plan,err=CompilePreparePlan(lease.Workflow,scope.Project,payload.Proof.AuthSessionID,payload.Params,payload.Input,[]byte(payload.Proof.FrozenInputJCS),[]byte(payload.Proof.SnapshotJCS),configs)
  }else{plan,err=CompileWatermarkPreparePlan(*scope,payload.Proof.AuthSessionID,payload.Params,payload.Input,[]byte(payload.Proof.FrozenInputJCS),[]byte(payload.Proof.SnapshotJCS),configs,pins)}
 }else{plan,err=CompilePreparePlan(lease.Workflow,payload.Proof.ProjectID,payload.Proof.AuthSessionID,payload.Params,payload.Input,[]byte(payload.Proof.FrozenInputJCS),[]byte(payload.Proof.SnapshotJCS),configs)}
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(lease.Run)
	if err != nil || workflow != lease.Workflow || plan.task != lease.ParentTask || plan.preparation.SlotCount != lease.SlotCount {
		return zero, v2input.ErrInvalid
	}
	// Proof versions and inventory must match the full compiler output.
	proofRaw, err := canonical.Marshal(payload.Proof)
	if err != nil || !bytes.Equal(proofRaw, plan.proofJCS) {
		return zero, v2input.ErrInvalid
	}
 if scope!=nil{
  for _,pin:=range pins{
   var config []byte;for _,item:=range configs{if item.ID==pin.Preset{config=item.Raw;break}}
   exact:=*scope;exact.Preset=pin.Preset;exact.PresetSHA=pin.PresetSHA
   fresh,err:=pinReader.Read(ctx,exact,lease.Expansion,lease.Token,config);if err!=nil{return zero,err}
   if fresh.SHA256()!=pin.Pin.SHA256()||!bytes.Equal(fresh.JCS(),pin.Pin.JCS()){return zero,v2input.ErrInvalid}
  }
  var fresh []byte
  if err=s.DB.QueryRow(ctx,`SELECT photo.v30_delivery_expansion_plan($1,$2)`,lease.Expansion,lease.Token).Scan(&fresh);err!=nil{return zero,err}
  before,err:=canonical.Bytes(raw);if err!=nil{return zero,err};after,err:=canonical.Bytes(fresh);if err!=nil||!bytes.Equal(before,after){return zero,v2input.ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return zero,err};return plan,nil
}
