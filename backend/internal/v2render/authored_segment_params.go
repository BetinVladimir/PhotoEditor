package v2render

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// AuthoredSegmentParams is one original layer occurrence in program2. A nil
// CurrentLayers denotes only an explicitly disabled/zero-opacity declaration;
// SQL and the worker independently prove that declaration before a no-op.
// This is neither a FrozenRenderRecipe replacement nor a qualification.
type AuthoredSegmentParams struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 ParentRunID uuid.UUID `json:"parent_run_id"`
 WorkflowID uuid.UUID `json:"workflow_id"`
 InstructionID uuid.UUID `json:"instruction_id"`
 Ordinal int `json:"ordinal"`
 ProgramSHA256 string `json:"program_sha256"`
 PredecessorSHA256 string `json:"predecessor_sha256"`
 Recipe v2input.Ref `json:"recipe"`
 BaseReceipt v2input.Ref `json:"base_receipt"`
 Source v2input.SessionVersionBinding `json:"source"`
 CurrentLayers *v2input.Ref `json:"current_layers"`
 ProtectedMask *v2input.Ref `json:"protected_mask"`
 PhaseReceipts []v2input.Ref `json:"phase_receipts"`
}

type CompiledAuthoredSegmentParams struct {
 Params AuthoredSegmentParams
 JSON []byte
}

func CompileAuthoredSegmentParams(params AuthoredSegmentParams)(CompiledAuthoredSegmentParams,error){
 invalid:=func()(CompiledAuthoredSegmentParams,error){return CompiledAuthoredSegmentParams{},v2input.ErrInvalid}
 if params.SchemaVersion!=1||params.TenantID==uuid.Nil||params.ParentRunID==uuid.Nil||params.WorkflowID==uuid.Nil||params.InstructionID==uuid.Nil||params.Ordinal<1||params.Ordinal>4998||!authoredFinalDigest(params.ProgramSHA256)||!authoredFinalDigest(params.PredecessorSHA256)||params.Source.AssetVersionID==uuid.Nil||!authoredFinalDigest(params.Source.CanvasHash)||len(params.PhaseReceipts)>4997||len(params.PhaseReceipts)!=params.Ordinal-1{return invalid()}
 for _,ref:=range []v2input.Ref{params.Recipe,params.BaseReceipt,params.Source.Source}{if !validLayerArtifactRef(ref){return invalid()}}
 for _,ref:=range []*v2input.Ref{params.CurrentLayers,params.ProtectedMask}{if ref!=nil&&!validLayerArtifactRef(*ref){return invalid()}}
 seen:=map[uuid.UUID]bool{}
 for _,ref:=range params.PhaseReceipts{if !validLayerArtifactRef(ref)||seen[ref.ArtifactID]{return invalid()};seen[ref.ArtifactID]=true}
 if params.CurrentLayers!=nil{ref:=*params.CurrentLayers;params.CurrentLayers=&ref}
 if params.ProtectedMask!=nil{ref:=*params.ProtectedMask;params.ProtectedMask=&ref}
 params.PhaseReceipts=append([]v2input.Ref{},params.PhaseReceipts...)
 raw,err:=canonical.Marshal(params);if err!=nil{return invalid()}
 return CompiledAuthoredSegmentParams{Params:params,JSON:raw},nil
}

// ValidateAuthoredSegmentParams requires the exact compiler-owned bytes at the
// actual published reference. Canonical source/adoption remains a SQL gate.
func ValidateAuthoredSegmentParams(compiled CompiledAuthoredSegmentParams,reference v2input.Ref)error{
 verified,err:=CompileAuthoredSegmentParams(compiled.Params)
 if err!=nil||!validLayerArtifactRef(reference)||!bytes.Equal(compiled.JSON,verified.JSON){return v2input.ErrInvalid}
 sum:=sha256.Sum256(verified.JSON)
 if hex.EncodeToString(sum[:])!=reference.SHA256{return v2input.ErrInvalid}
 return nil
}
