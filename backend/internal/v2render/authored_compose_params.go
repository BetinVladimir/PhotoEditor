package v2render

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// AuthoredFinalParams is render.compose/2, not the compose/1 FrozenRenderRecipe
// ABI. Every reference names an already published input; future outputs are not
// inputs. Scoped SQL admission separately proves order and current authority.
type AuthoredFinalParams struct {
 SchemaVersion int `json:"schema_version"`
 TenantID uuid.UUID `json:"tenant_id"`
 ParentRunID uuid.UUID `json:"parent_run_id"`
 WorkflowID uuid.UUID `json:"workflow_id"`
 InstructionID uuid.UUID `json:"instruction_id"`
 ProgramSHA256 string `json:"program_sha256"`
 Recipe v2input.Ref `json:"recipe"`
 BaseReceipt v2input.Ref `json:"base_receipt"`
 GeometryContinuation v2input.Ref `json:"geometry_continuation"`
 Source v2input.SessionVersionBinding `json:"source"`
 RegisteredOriginal v2input.Ref `json:"registered_original"`
 PhaseReceipts []v2input.Ref `json:"phase_receipts"`
}

type CompiledAuthoredFinalParams struct {
 Params AuthoredFinalParams
 JSON []byte
}

func authoredFinalDigest(value string) bool {
 raw, err := hex.DecodeString(value)
 return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

// CompileAuthoredFinalParams owns the ordered input slice and emits canonical
// bytes. It does not qualify, reserve, publish, approve, or enqueue a task.
func CompileAuthoredFinalParams(params AuthoredFinalParams) (CompiledAuthoredFinalParams, error) {
 invalid := func() (CompiledAuthoredFinalParams,error) { return CompiledAuthoredFinalParams{}, v2input.ErrInvalid }
 if params.SchemaVersion != 1 || params.TenantID == uuid.Nil || params.ParentRunID == uuid.Nil || params.WorkflowID == uuid.Nil || params.InstructionID == uuid.Nil || !authoredFinalDigest(params.ProgramSHA256) || params.Source.AssetVersionID == uuid.Nil || !authoredFinalDigest(params.Source.CanvasHash) || len(params.PhaseReceipts) < 1 || len(params.PhaseReceipts) > 4998 { return invalid() }
 for _, ref := range []v2input.Ref{params.Recipe,params.BaseReceipt,params.GeometryContinuation,params.Source.Source,params.RegisteredOriginal} { if !validLayerArtifactRef(ref) { return invalid() } }
 seen := make(map[uuid.UUID]bool,len(params.PhaseReceipts))
 for _, ref := range params.PhaseReceipts {
  if !validLayerArtifactRef(ref) || seen[ref.ArtifactID] || ref == params.Recipe || ref == params.BaseReceipt { return invalid() }
  seen[ref.ArtifactID] = true
 }
 params.PhaseReceipts = append([]v2input.Ref(nil),params.PhaseReceipts...)
 body,err := canonical.Marshal(params)
 if err != nil { return invalid() }
 return CompiledAuthoredFinalParams{Params:params,JSON:body},nil
}

// AuthoredFinalInputRoots verifies the cached wire bytes and inventories direct
// inputs. A scoped resolver MUST expand all typed receipt/continuation/recipe/
// WorkingImage children and prove actual preceding canonical task authority
// before publishing an InputClosure. These roots alone are not an admission.
func AuthoredFinalInputRoots(compiled CompiledAuthoredFinalParams,paramsRef v2input.Ref) ([]v2input.Entry,error) {
 verified,err := CompileAuthoredFinalParams(compiled.Params)
 if err != nil || !bytes.Equal(verified.JSON,compiled.JSON) || !validLayerArtifactRef(paramsRef) { return nil,v2input.ErrInvalid }
 sum := sha256.Sum256(compiled.JSON)
 if hex.EncodeToString(sum[:]) != paramsRef.SHA256 { return nil,v2input.ErrInvalid }
 entries := []v2input.Entry{
  {Ref:paramsRef,Contract:"Params"},
  {Ref:verified.Params.Recipe,Contract:"FrozenRenderRecipe"},
  {Ref:verified.Params.BaseReceipt,Contract:"AuthoredBaseReceipt"},
  {Ref:verified.Params.GeometryContinuation,Contract:"AuthoredCompoundContinuation"},
  {Ref:verified.Params.Source.Source,Contract:"WorkingImage"},
  {Ref:verified.Params.RegisteredOriginal,Contract:"WorkingImage"},
 }
 for _,ref := range verified.Params.PhaseReceipts { entries=append(entries,v2input.Entry{Ref:ref,Contract:"AuthoredCanonicalPhaseReceipt"}) }
 sort.Slice(entries,func(i,j int) bool { return bytes.Compare(entries[i].Ref.ArtifactID[:],entries[j].Ref.ArtifactID[:])<0 })
 result:=make([]v2input.Entry,0,len(entries))
 for _,entry:=range entries {
  if len(result)>0 && result[len(result)-1].Ref.ArtifactID==entry.Ref.ArtifactID {
   if result[len(result)-1] != entry { return nil,v2input.ErrInvalid };continue
  }
  result=append(result,entry)
 }
 return result,nil
}
