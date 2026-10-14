package interchange

import (
 "context"
 "photoeditor/backend/internal/v2input"
)

// ImportBackgroundSource identifies an exact staged, hashed archive member.
// Resolving this reference does not qualify pixels or publish a local asset.
type ImportBackgroundSource struct {
 Style v2input.Ref `json:"style"`
 Portable string `json:"portable_asset_id"`
 Original v2input.Ref `json:"original"`
 Staged v2input.Ref `json:"staged"`
}

type ImportBackgroundIndex struct { sources map[v2input.Ref]map[string]ImportBackgroundSource }

func NewImportBackgroundIndex(ctx context.Context, parsedRaw []byte) (*ImportBackgroundIndex,error) {
 if err:=ctx.Err();err!=nil{return nil,err}
 owned:=append([]byte(nil),parsedRaw...)
 if _,err:=validateParsedImportDocument(ctx,owned);err!=nil{return nil,err}
 var parsed importParsedDocument
 if err:=decodeImportDocument(owned,&parsed);err!=nil{return nil,err}
 files:=map[string]v2input.Ref{}
 for _,file:=range parsed.Files { files[file.Path]=file.Content }
 inventory:=map[string]v2input.Ref{}
 for _,item:=range parsed.Manifest.Inventory { inventory[item.Path]=v2input.Ref{ArtifactID:item.ID,SHA256:item.SHA} }
 result:=&ImportBackgroundIndex{sources:map[v2input.Ref]map[string]ImportBackgroundSource{}}
 for _,mapping:=range parsed.Manifest.Backgrounds {
  staged,ok:=files[mapping.Path];original,exists:=inventory[mapping.Path]
  if !ok||!exists||!validImportRef(staged)||!validImportRef(original)||original.SHA256!=mapping.SHA||staged.SHA256!=mapping.SHA{return nil,v2input.ErrInvalid}
  if result.sources[mapping.Style]==nil {result.sources[mapping.Style]=map[string]ImportBackgroundSource{}}
  result.sources[mapping.Style][mapping.Portable]=ImportBackgroundSource{Style:mapping.Style,Portable:mapping.Portable,Original:original,Staged:staged}
 }
 return result,ctx.Err()
}

func(index *ImportBackgroundIndex)ResolveImportBackground(ctx context.Context,style v2input.Ref,portable string)(ImportBackgroundSource,error){
 if err:=ctx.Err();err!=nil{return ImportBackgroundSource{},err}
 if index==nil||!validImportRef(style){return ImportBackgroundSource{},v2input.ErrInvalid}
 source,ok:=index.sources[style][portable]
 if !ok{return ImportBackgroundSource{},v2input.ErrInvalid}
 return source,nil
}
