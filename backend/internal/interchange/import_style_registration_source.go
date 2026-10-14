package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// The original style and background come from the verified parsed inventory,
// never from client-selected registration hints. Prepared IDs are independently
// reconstructed for the selected entry before compiling registration.
func PrepareImportStyleRegistration(ctx context.Context,scope ImportPreparedScope,parsedRaw []byte,style v2input.Ref,body []byte,qualifier ImportStyleQualifier)(ImportStyleRegistration,error){
 if err:=ctx.Err();err!=nil{return ImportStyleRegistration{},err}
 if _,err:=validateParsedImportDocument(ctx,parsedRaw);err!=nil{return ImportStyleRegistration{},err}
 var parsed importParsedDocument;if err:=decodeImportDocument(parsedRaw,&parsed);err!=nil{return ImportStyleRegistration{},err}
 var original v2input.Ref;matches:=0
 for _,item:=range parsed.Manifest.Inventory{
  if item.Contract!="StyleV2"{continue}
  expected:=uuid.NewSHA1(scope.Workflow,[]byte("interchange:dependency:"+scope.Entry.String()+":StyleV2:"+item.ID.String()))
  if expected==style.ArtifactID{matches++;original=v2input.Ref{ArtifactID:item.ID,SHA256:item.SHA}}
 }
 if matches!=1{return ImportStyleRegistration{},v2input.ErrInvalid}
 var fields struct{Extensions struct{Background *struct{Mode string `json:"mode"`;AssetID string `json:"asset_id"`} `json:"background"`} `json:"extensions"`}
 if err:=json.Unmarshal(body,&fields);err!=nil{return ImportStyleRegistration{},err}
 var background *ImportBackgroundSource
 if fields.Extensions.Background!=nil&&fields.Extensions.Background.Mode=="asset"{
  index,err:=NewImportBackgroundIndex(ctx,parsedRaw);if err!=nil{return ImportStyleRegistration{},err}
  matches=0
  for _,mapping:=range parsed.Manifest.Backgrounds{
   if mapping.Style!=original{continue}
   source,err:=index.ResolveImportBackground(ctx,original,mapping.Portable);if err!=nil{return ImportStyleRegistration{},err}
   if importBackgroundID(scope.Workflow,source,scope.Binding).String()==fields.Extensions.Background.AssetID{owned:=source;background=&owned;matches++}
  }
  if matches!=1{return ImportStyleRegistration{},v2input.ErrInvalid}
 }
 return CompileImportStyleRegistration(ctx,scope,original,style,body,background,qualifier)
}
