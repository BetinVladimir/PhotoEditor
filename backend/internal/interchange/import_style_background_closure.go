package interchange
import (
 "context"
 "encoding/json"
 "sync"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// Portable identifiers are typed dependencies, not serialized ArtifactRefs.
// Only the exact generated UUID is admitted, with a separately qualified SHA.
func importClosureDocumentRefs(body []byte,contract string,children []v2input.Entry)(map[uuid.UUID]v2input.Ref,error){
 refs,err:=importDocumentRefs(body,contract);if err!=nil{return nil,err}
 if contract!="StyleV2"{return refs,nil}
 id,present,err:=importStyleBackgroundID(body);if err!=nil{return nil,err};if !present{return refs,nil}
 found:=false
 for _,child:=range children{if child.Ref.ArtifactID==id{
  if found||child.Contract!="StyleBackgroundAsset"||!validImportRef(child.Ref){return nil,v2input.ErrInvalid}
  if old,ok:=refs[id];ok&&old!=child.Ref{return nil,v2input.ErrInvalid}
  refs[id]=child.Ref;found=true
 }}
 if !found{return nil,v2input.ErrInvalid};return refs,nil
}
func importStyleBackgroundID(raw []byte)(uuid.UUID,bool,error){
 var style struct{Extensions struct{Background *struct{Mode string `json:"mode"`;AssetID *string `json:"asset_id"`} `json:"background"`} `json:"extensions"`}
 if err:=json.Unmarshal(raw,&style);err!=nil{return uuid.Nil,false,err}
 bg:=style.Extensions.Background;if bg==nil||bg.Mode!="asset"{return uuid.Nil,false,nil}
 if bg.AssetID==nil{return uuid.Nil,false,v2input.ErrInvalid}
 id,err:=uuid.Parse(*bg.AssetID);if err!=nil||id==uuid.Nil||id.String()!=*bg.AssetID{return uuid.Nil,false,v2input.ErrInvalid}
 return id,true,nil
}
type importBackgroundClosureWitnesses struct{mu sync.RWMutex;refs map[uuid.UUID]v2input.Ref}
type witnessedImportBackgroundRemapper struct{Base IndexedImportBackgroundRemapper;Witnesses *importBackgroundClosureWitnesses}
func(r witnessedImportBackgroundRemapper)RemapImportedStyleBackground(ctx context.Context,style v2input.Ref,portable string,target v2input.SessionVersionBinding)(string,error){
 if r.Witnesses==nil||r.Base.Index==nil{return "",v2input.ErrInvalid}
 source,err:=r.Base.Index.ResolveImportBackground(ctx,style,portable);if err!=nil{return "",err}
 id,err:=r.Base.RemapImportedStyleBackground(ctx,style,portable,target);if err!=nil{return "",err}
 parsed,err:=uuid.Parse(id);if err!=nil||parsed.String()!=id{return "",v2input.ErrInvalid}
 ref:=v2input.Ref{ArtifactID:parsed,SHA256:source.Original.SHA256};if !validImportRef(ref){return "",v2input.ErrInvalid}
 r.Witnesses.mu.Lock();defer r.Witnesses.mu.Unlock()
 if r.Witnesses.refs==nil{r.Witnesses.refs=map[uuid.UUID]v2input.Ref{}}
 if old,ok:=r.Witnesses.refs[parsed];ok&&old!=ref{return "",v2input.ErrInvalid}
 if _,exists:=r.Witnesses.refs[parsed];!exists&&len(r.Witnesses.refs)>=5000{return "",v2input.ErrInvalid}
 r.Witnesses.refs[parsed]=ref;return id,ctx.Err()
}
type importStyleBackgroundChildren struct{Other ImportTypedDocumentValidator;Witnesses *importBackgroundClosureWitnesses}
func(v importStyleBackgroundChildren)ImportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if v.Other==nil{return nil,v2input.ErrInvalid}
 children,err:=v.Other.ImportDocumentChildren(ctx,entry,append([]byte(nil),raw...));if err!=nil{return nil,err};children=append([]v2input.Entry(nil),children...)
 if entry.Contract!="StyleV2"{return children,ctx.Err()}
 id,present,err:=importStyleBackgroundID(raw);if err!=nil{return nil,err};if !present{return children,ctx.Err()}
 if v.Witnesses==nil{return nil,v2input.ErrInvalid}
 v.Witnesses.mu.RLock();ref,ok:=v.Witnesses.refs[id];v.Witnesses.mu.RUnlock();if !ok{return nil,v2input.ErrInvalid}
 for _,child:=range children{if child.Ref.ArtifactID==id{return nil,v2input.ErrInvalid}}
 children=append(children,v2input.Entry{Ref:ref,Contract:"StyleBackgroundAsset"});return children,ctx.Err()
}
