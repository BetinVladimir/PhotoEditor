package v2render

import (
 "crypto/sha256"
 "encoding/hex"

 "photoeditor/backend/internal/v2input"
)

// MaterializeAuthoredFinalParams consumes the actual versioned continuation
// publication reference. It never invents a future document UUID, rewrites the
// original recipe to the last canvas, or uses native proposal pixels as final.
// SQL must prove the published ref/body and transcript again when sealing.
func MaterializeAuthoredFinalParams(documents AuthoredFinalDocuments,publishedContinuation v2input.Ref)(CompiledAuthoredFinalParams,error){
 body,sha,registered,err:=CompileAuthoredFinalContinuation(documents)
 if err!=nil{return CompiledAuthoredFinalParams{},err}
 if !validLayerArtifactRef(publishedContinuation)||publishedContinuation.SHA256!=sha{return CompiledAuthoredFinalParams{},v2input.ErrInvalid}
 digest:=sha256.Sum256(body);if hex.EncodeToString(digest[:])!=publishedContinuation.SHA256{return CompiledAuthoredFinalParams{},v2input.ErrInvalid}
 inventory:=documents.Inventory;scope:=inventory.Context
 phases:=make([]v2input.Ref,len(inventory.Phases));for i,item:=range inventory.Phases{phases[i]=item.Receipt}
 return CompileAuthoredFinalParams(AuthoredFinalParams{SchemaVersion:1,TenantID:scope.Tenant,ParentRunID:scope.Parent,
  WorkflowID:scope.Workflow,InstructionID:scope.Instruction.Identity,ProgramSHA256:scope.ProgramSHA,
  Recipe:scope.OriginalRecipe,BaseReceipt:inventory.BaseReceipt,GeometryContinuation:publishedContinuation,
  Source:scope.Source,RegisteredOriginal:registered,PhaseReceipts:phases})
}
