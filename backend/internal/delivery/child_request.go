package delivery

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/v2input"
)

// FrozenExportRequest carries immutable content to the V30 export admission.
// It is not the legacy selection/download job request. The receiving repository
// must verify current parent expansion fence, actor, project, source/recipe,
// artifact ACL and quota before it creates the deterministic child run.
type FrozenExportRequest struct {
	SchemaVersion  int                           `json:"schema_version"`
	ParentRunID    uuid.UUID                     `json:"parent_run_id"`
	ChildRunID     uuid.UUID                     `json:"child_run_id"`
	SlotIndex      int                           `json:"slot_index"`
	Ordinal        int                           `json:"ordinal"`
	Binding        v2input.SessionVersionBinding `json:"binding"`
	Recipe         v2input.Ref                   `json:"recipe"`
	SnapshotDigest string                        `json:"snapshot_digest"`
	Preset         FrozenPreset                  `json:"preset"`
	ConfigJCS      string                        `json:"config_jcs"`
}

// ExportRequest recomputes child identity and selects config bytes only from
// the validated frozen proof. No mutable preset lookup or revision substitution.
func (p PreparePlan) ExportRequest(parent uuid.UUID, child ExportChildIntent) (FrozenExportRequest, []byte, error) {
 return p.exportRequest(parent,child,false)
}

// WatermarkExportRequest binds a selected child and immutable watermark pin to
// the same prepare proof. This proves values only; current SQL/physical source
// authorization remains mandatory before admission.
func(p PreparePlan)WatermarkExportRequest(parent uuid.UUID,child ExportChildIntent,scope WatermarkPinScope,pin FrozenWatermarkSource)(FrozenExportRequest,[]byte,error){
 var zero FrozenExportRequest
 request,raw,err:=p.exportRequest(parent,child,true);if err!=nil{return zero,nil,err}
 var proof PrepareProof
 if json.Unmarshal(p.proofJCS,&proof)!=nil||proof.ProjectID!=scope.Project||scope.Parent!=parent||scope.Preset!=request.Preset.PresetID||scope.PresetSHA!=request.Preset.PresetSHA{return zero,nil,v2input.ErrInvalid}
 if _,err:=DecodeWatermarkSourcePin(pin.JCS(),pin.SHA256(),scope,[]byte(request.ConfigJCS));err!=nil{return zero,nil,err}
 return request,raw,nil
}
func(p PreparePlan)exportRequest(parent uuid.UUID,child ExportChildIntent,watermark bool)(FrozenExportRequest,[]byte,error){
	var zero FrozenExportRequest
	page, err := p.ExportChildren(parent, child.SlotIndex, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0] != child {
		return zero, nil, v2input.ErrInvalid
	}
	var proof PrepareProof
	if json.Unmarshal(p.proofJCS, &proof) != nil {
		return zero, nil, v2input.ErrInvalid
	}
	var config string
	for _, item := range proof.ConfigsJCS {
		if item.PresetID == child.Preset.PresetID {
			if config != "" {
				return zero, nil, v2input.ErrInvalid
			}
			config = item.ConfigJCS
		}
	}
	parsed, err := exportpreset.DecodeFrozenConfig([]byte(config), child.Preset.PresetSHA)
	policy, policyErr := p.MetadataPolicy()
	if err != nil || policyErr != nil || child.Preset.Config.SHA256 != child.Preset.PresetSHA || (parsed.Watermark != nil) != watermark || parsed.MetadataPolicy != policy.MetadataPolicy {
		return zero, nil, v2input.ErrInvalid
	}
	request := FrozenExportRequest{SchemaVersion: 1, ParentRunID: parent, ChildRunID: child.RunID, SlotIndex: child.SlotIndex, Ordinal: child.Ordinal, Binding: child.Binding, Recipe: child.Recipe, SnapshotDigest: child.SnapshotDigest, Preset: child.Preset, ConfigJCS: config}
	raw, err := canonical.Marshal(request)
	if err != nil || len(raw) > 2<<20 {
		return zero, nil, v2input.ErrInvalid
	}
	return request, raw, nil
}

// MetadataPolicy returns owned consent from the already validated prepare proof.
func(p PreparePlan)MetadataPolicy()(Policy,error){
 var proof PrepareProof;var frozen FrozenInput
 if p.task==uuid.Nil||json.Unmarshal(p.proofJCS,&proof)!=nil||json.Unmarshal([]byte(proof.FrozenInputJCS),&frozen)!=nil{return Policy{},v2input.ErrInvalid}
 fields,err:=normalizeMetadataApproval(frozen.Policy.MetadataPolicy,frozen.Policy.ApprovedFields);if err!=nil{return Policy{},err}
 policy:=frozen.Policy;policy.ApprovedFields=fields
 policy.RequiredPresets=append([]uuid.UUID(nil),policy.RequiredPresets...);policy.OptionalPresets=append([]uuid.UUID{},policy.OptionalPresets...)
 return policy,nil
}

// WatermarkChildConfig exposes owned frozen config bytes for current pin reads.
// It proves selection values only; it is not an export admission request.
func(p PreparePlan)WatermarkChildConfig(parent uuid.UUID,child ExportChildIntent)([]byte,error){
 request,_,err:=p.exportRequest(parent,child,true);if err!=nil{return nil,err}
 return append([]byte(nil),[]byte(request.ConfigJCS)...),nil
}

// ChildUsesWatermark validates the frozen child/config before routing. Errors
// are refusals, never instructions to try a different admission protocol.
func(p PreparePlan)ChildUsesWatermark(parent uuid.UUID,child ExportChildIntent)(bool,error){
 page,err:=p.ExportChildren(parent,child.SlotIndex,1)
 if err!=nil||len(page.Items)!=1||page.Items[0]!=child{return false,v2input.ErrInvalid}
 var proof PrepareProof
 if json.Unmarshal(p.proofJCS,&proof)!=nil{return false,v2input.ErrInvalid}
 var raw string
 for _,item:=range proof.ConfigsJCS{if item.PresetID==child.Preset.PresetID{if raw!=""{return false,v2input.ErrInvalid};raw=item.ConfigJCS}}
 config,err:=exportpreset.DecodeFrozenConfig([]byte(raw),child.Preset.PresetSHA);if err!=nil{return false,err}
 watermark:=config.Watermark!=nil
 if _,_,err=p.exportRequest(parent,child,watermark);err!=nil{return false,err}
 return watermark,nil
}
