package interchange

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Bindings are constructed by the scheduler after canonical ingest, not read
// from bundle hints. Every use requalifies the exact old/new source pair.
type ImportCorrectionBinding struct{Entry uuid.UUID;Old,New v2input.SessionVersionBinding}
type ImportCorrectionPlanCodec struct{
 snapshot string
 bindings map[uuid.UUID]ImportCorrectionBinding
 sources ImportRecipeSourceQualifier
 fallback ImportDependencyCodec
}
func NewImportCorrectionPlanCodec(snapshot string,bindings []ImportCorrectionBinding,sources ImportRecipeSourceQualifier,fallback ImportDependencyCodec)(*ImportCorrectionPlanCodec,error){
 if sources==nil||len(bindings)<1||len(bindings)>5000{return nil,v2input.ErrInvalid}
 mapped:=map[uuid.UUID]ImportCorrectionBinding{};targets:=map[uuid.UUID]bool{};entries:=map[uuid.UUID]bool{}
 for _,item:=range bindings{
  valid:=func(b v2input.SessionVersionBinding)bool{return b.AssetVersionID!=uuid.Nil&&validImportRef(b.Source)&&validImportRef(v2input.Ref{ArtifactID:b.AssetVersionID,SHA256:b.CanvasHash})}
  if item.Entry==uuid.Nil||entries[item.Entry]||!valid(item.Old)||!valid(item.New)||item.Old.AssetVersionID==item.New.AssetVersionID||item.Old.Source.ArtifactID==item.New.Source.ArtifactID||targets[item.New.AssetVersionID]{return nil,v2input.ErrInvalid}
  if _,ok:=mapped[item.Old.AssetVersionID];ok{return nil,v2input.ErrInvalid}
  entries[item.Entry]=true;targets[item.New.AssetVersionID]=true;mapped[item.Old.AssetVersionID]=item
 }
 if !validImportRef(v2input.Ref{ArtifactID:bindings[0].Entry,SHA256:snapshot}){return nil,v2input.ErrInvalid}
 return &ImportCorrectionPlanCodec{snapshot:snapshot,bindings:mapped,sources:sources,fallback:fallback},nil
}
func(c *ImportCorrectionPlanCodec)RemapImportDocument(ctx context.Context,contract string,raw []byte,old,target v2input.SessionVersionBinding,resolve func(v2input.Ref,string)(v2input.Ref,error))([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if c==nil||c.sources==nil{return nil,v2input.ErrInvalid}
 if contract!="CorrectionPlan"{if c.fallback!=nil{return c.fallback.RemapImportDocument(ctx,contract,raw,old,target,resolve)};return nil,&ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 current,ok:=c.bindings[old.AssetVersionID];if !ok||current.Old!=old||current.New!=target{return nil,v2input.ErrInvalid}
 if err:=v2draft.ValidateCorrectionPlan(raw,old);err!=nil{return nil,err}
 var plan map[string]json.RawMessage;if err:=json.Unmarshal(raw,&plan);err!=nil{return nil,err}
 var items []map[string]json.RawMessage;if err:=json.Unmarshal(plan["items"],&items);err!=nil{return nil,err}
 for _,item:=range items{
  if err:=ctx.Err();err!=nil{return nil,err}
  var binding v2input.SessionVersionBinding;if err:=json.Unmarshal(item["binding"],&binding);err!=nil{return nil,err}
  mapping,ok:=c.bindings[binding.AssetVersionID]
  if !ok{return nil,&ImportRecipeUnsupported{Reason:"correction_source_not_imported"}}
  if mapping.Old!=binding{return nil,v2input.ErrInvalid}
  if err:=c.sources.QualifyImportRecipeSource(ctx,mapping.Entry,mapping.Old,mapping.New);err!=nil{return nil,err}
  encoded,err:=canonical.Marshal(mapping.New);if err!=nil{return nil,err};item["binding"]=encoded
 }
 var err error
 plan["items"],err=canonical.Marshal(items);if err!=nil{return nil,err}
 plan["snapshot_digest"],err=canonical.Marshal(c.snapshot);if err!=nil{return nil,err}
 prepared,err:=canonical.Marshal(plan);if err!=nil{return nil,err}
 if err:=c.QualifyRemappedImportDocument(ctx,contract,prepared,target);err!=nil{return nil,err}
 return prepared,ctx.Err()
}
func(c *ImportCorrectionPlanCodec)QualifyRemappedImportDocument(ctx context.Context,contract string,raw []byte,target v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 if c==nil||c.sources==nil{return v2input.ErrInvalid}
 if contract!="CorrectionPlan"{if c.fallback!=nil{return c.fallback.QualifyRemappedImportDocument(ctx,contract,raw,target)};return &ImportRecipeUnsupported{Reason:"dependency_contract_not_installed"}}
 if err:=v2draft.ValidateCorrectionPlan(raw,target);err!=nil{return err}
 var document struct{Snapshot string `json:"snapshot_digest"`;Items []struct{Binding v2input.SessionVersionBinding `json:"binding"`} `json:"items"`}
 if err:=json.Unmarshal(raw,&document);err!=nil{return err};if document.Snapshot!=c.snapshot{return v2input.ErrInvalid}
 byTarget:=map[uuid.UUID]ImportCorrectionBinding{};for _,item:=range c.bindings{byTarget[item.New.AssetVersionID]=item}
 for _,item:=range document.Items{
  mapping,ok:=byTarget[item.Binding.AssetVersionID];if !ok||mapping.New!=item.Binding{return v2input.ErrInvalid}
  if err:=c.sources.QualifyImportRecipeSource(ctx,mapping.Entry,mapping.Old,mapping.New);err!=nil{return err}
 }
 return ctx.Err()
}

func(c *ImportCorrectionPlanCodec)BindImportDocumentSource(source v2input.Ref)ImportDependencyCodec{
 if c==nil{return nil}
 owned:=*c
 if binder,ok:=owned.fallback.(ImportDocumentSourceBinder);ok{owned.fallback=binder.BindImportDocumentSource(source)}
 return &owned
}
