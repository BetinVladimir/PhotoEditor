package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "strconv"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
// ImportClosureNode is produced by a scoped typed resolver after physical-byte
// verification. Binary leaves have nil Body and no Children; document nodes
// carry their exact canonical bytes. The resolver must enforce MIME, budgets,
// source binding, model qualification and current tenant/project provenance.
type ImportClosureNode struct{Entry v2input.Entry;Body []byte;Children []v2input.Entry}
type ImportClosureNodeResolver interface{ResolveImportNode(context.Context,v2input.Entry)(ImportClosureNode,error)}
func ResolveImportInputInventory(ctx context.Context,tenant uuid.UUID,documents ImportPreparedDocuments,resolver ImportClosureNodeResolver)(ImportInputInventory,error){
 if resolver==nil||tenant==uuid.Nil{return ImportInputInventory{},v2input.ErrInvalid}
 for _,doc:=range documents.Documents(){if err:=validateImportStorageDocument(documents.identity,doc);err!=nil{return ImportInputInventory{},err}}
 parsed,err:=importParamsRoots(documents);if err!=nil{return ImportInputInventory{},err}
 roots:=[]v2input.Entry{{Ref:documents.params.Ref,Contract:"Params"},{Ref:documents.plan.Ref,Contract:"InterchangePlan"},{Ref:parsed,Contract:"ParsedInterchange"}}
 if documents.evidence!=nil{roots=append(roots,v2input.Entry{Ref:documents.evidence.Ref,Contract:"FrozenInterchangeImportEvidence"})}
 states:=map[uuid.UUID]int{};seen:=map[uuid.UUID]v2input.Entry{};entries:=[]v2input.Entry{}
 var visit func(v2input.Entry,int)error
 visit=func(entry v2input.Entry,depth int)error{
  if err:=ctx.Err();err!=nil{return err}
  if depth>32||entry.Ref.ArtifactID==uuid.Nil||len(entry.Contract)<1||len(entry.Contract)>120||len(entry.Ref.SHA256)!=64{return v2input.ErrInvalid}
  for _,c:=range entry.Ref.SHA256{if !(c>='0'&&c<='9'||c>='a'&&c<='f'){return v2input.ErrInvalid}}
  if previous,ok:=seen[entry.Ref.ArtifactID];ok{
   if previous!=entry||states[entry.Ref.ArtifactID]==1{return v2input.ErrInvalid};return nil
  }
  if len(seen)>=v2input.MaxEntries{return v2input.ErrInvalid}
  seen[entry.Ref.ArtifactID]=entry;states[entry.Ref.ArtifactID]=1
  node,err:=resolver.ResolveImportNode(ctx,entry);if err!=nil{return err}
  if err:=ctx.Err();err!=nil{return err}
  if node.Entry!=entry{return v2input.ErrInvalid}
  if (entry.Contract=="Params"||entry.Contract=="InterchangePlan"||entry.Contract=="ParsedInterchange")&&node.Body==nil{return v2input.ErrInvalid}
  if entry.Ref==documents.params.Ref&&!bytes.Equal(node.Body,documents.params.Body){return v2input.ErrInvalid}
  if entry.Ref==documents.plan.Ref&&!bytes.Equal(node.Body,documents.plan.Body){return v2input.ErrInvalid}
  if documents.evidence!=nil&&entry.Ref==documents.evidence.Ref&&!bytes.Equal(node.Body,documents.evidence.Body){return v2input.ErrInvalid}
  body:=append([]byte(nil),node.Body...);children:=append([]v2input.Entry(nil),node.Children...)
  if node.Body!=nil{
   if len(body)<2||len(body)>8<<20||rawSHA(body)!=entry.Ref.SHA256{return v2input.ErrInvalid}
   normalized,err:=canonical.Bytes(body);if err!=nil||!bytes.Equal(normalized,body){return v2input.ErrInvalid}
   refs,err:=importClosureDocumentRefs(body,entry.Contract,children);if err!=nil{return err}
   supplied:=map[uuid.UUID]v2input.Ref{}
   for _,child:=range children{
    if old,ok:=supplied[child.Ref.ArtifactID];ok&&old!=child.Ref{return v2input.ErrInvalid};supplied[child.Ref.ArtifactID]=child.Ref
   }
   if len(refs)!=len(supplied){return v2input.ErrInvalid}
   for id,ref:=range refs{if supplied[id]!=ref{return v2input.ErrInvalid}}
  }else if len(children)!=0{return v2input.ErrInvalid}
  for _,child:=range children{if err:=visit(child,depth+1);err!=nil{return err}}
  states[entry.Ref.ArtifactID]=2;entries=append(entries,entry);return nil
 }
 for _,root:=range roots{if err:=visit(root,0);err!=nil{return ImportInputInventory{},err}}
 return FreezeImportInputs(tenant,documents,entries)
}
// Untrusted manifest.inventory identities are inline metadata, not ArtifactRef
// objects. Typed node resolution must validate their separate schema semantics.
func importDocumentRefs(body []byte,contract string)(map[uuid.UUID]v2input.Ref,error){
 var root any;if err:=json.Unmarshal(body,&root);err!=nil{return nil,err}
 refs:=map[uuid.UUID]v2input.Ref{}
 var walk func(any,int,string)error
 walk=func(value any,depth int,path string)error{
  if depth>32{return v2input.ErrInvalid}
  switch node:=value.(type){
  case map[string]any:
   id,hasID:=node["artifact_id"];sha,hasSHA:=node["sha256"]
   if hasID||hasSHA{
    inline:=contract=="ParsedInterchange"&&strings.HasPrefix(path,"$.manifest.inventory[")&&strings.HasSuffix(path,"]")&&!strings.Contains(strings.TrimPrefix(path,"$.manifest.inventory["),".")
    inlineMask:=contract=="ParsedInterchange"&&strings.HasPrefix(path,"$.manifest.masks[")&&strings.HasSuffix(path,"]")&&!strings.Contains(strings.TrimPrefix(path,"$.manifest.masks["),".")
    if inlineMask&&!hasID&&hasSHA&&len(node)==3{
     _,hasPath:=node["path"];_,hasCanvas:=node["canvas_hash"]
     if hasPath&&hasCanvas{return nil}
    }
    if inline&&len(node)==4{
     _,hasPath:=node["path"];_,hasContract:=node["contract"]
     if hasID&&hasSHA&&hasPath&&hasContract{return nil}
    }
    if !hasID||!hasSHA||len(node)!=2{return v2input.ErrInvalid}

    text,ok:=id.(string);if !ok{return v2input.ErrInvalid};parsed,err:=uuid.Parse(text);if err!=nil||parsed==uuid.Nil{return v2input.ErrInvalid}
    hash,ok:=sha.(string);if !ok{return v2input.ErrInvalid};ref:=v2input.Ref{ArtifactID:parsed,SHA256:hash}
    if old,ok:=refs[parsed];ok&&old!=ref{return v2input.ErrInvalid};refs[parsed]=ref;return nil
   }
   for key,child:=range node{if err:=walk(child,depth+1,path+"."+key);err!=nil{return err}}
  case []any:for index,child:=range node{if err:=walk(child,depth+1,path+"["+strconv.Itoa(index)+"]");err!=nil{return err}}
  }
  return nil
 }
 if err:=walk(root,0,"$");err!=nil{return nil,err};return refs,nil
}
