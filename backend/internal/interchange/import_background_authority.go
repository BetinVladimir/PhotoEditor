package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// Owns scheduler-created mappings; archive metadata cannot nominate a target.
// Source qualification re-reads the authorized master on every invocation.
type BoundImportBackgroundAuthority struct {
 bindings map[uuid.UUID]ImportCorrectionBinding
 sources ImportRecipeSourceQualifier
}
func NewBoundImportBackgroundAuthority(bindings []ImportCorrectionBinding,sources ImportRecipeSourceQualifier)(*BoundImportBackgroundAuthority,error){
 if sources==nil||len(bindings)<1||len(bindings)>5000{return nil,v2input.ErrInvalid}
 out:=&BoundImportBackgroundAuthority{bindings:map[uuid.UUID]ImportCorrectionBinding{},sources:sources}
 entries:=map[uuid.UUID]bool{}
 for _,binding:=range bindings{
  if binding.Entry==uuid.Nil||entries[binding.Entry]||binding.Old.AssetVersionID==uuid.Nil||binding.New.AssetVersionID==uuid.Nil||binding.Old.AssetVersionID==binding.New.AssetVersionID||!validImportRef(binding.Old.Source)||!validImportRef(binding.New.Source)||binding.Old.Source.ArtifactID==binding.New.Source.ArtifactID||!validImportRef(v2input.Ref{ArtifactID:binding.Entry,SHA256:binding.Old.CanvasHash})||!validImportRef(v2input.Ref{ArtifactID:binding.Entry,SHA256:binding.New.CanvasHash}){return nil,v2input.ErrInvalid}
  if _,ok:=out.bindings[binding.New.AssetVersionID];ok{return nil,v2input.ErrInvalid}
  entries[binding.Entry]=true;out.bindings[binding.New.AssetVersionID]=binding
 }
 return out,nil
}
func(a *BoundImportBackgroundAuthority)QualifyImportBackgroundTarget(ctx context.Context,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if a==nil||a.sources==nil{return v2input.ErrInvalid}
 binding,ok:=a.bindings[target.AssetVersionID]
 if !ok||binding.New!=target{return v2input.ErrInvalid}
 if err:=a.sources.QualifyImportRecipeSource(ctx,binding.Entry,binding.Old,binding.New);err!=nil{return err}
 return ctx.Err()
}
