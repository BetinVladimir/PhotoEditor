package interchange

import (
 "bytes"
 "context"
 "crypto/ed25519"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func historicalReceiptID(workflow,entry uuid.UUID,kind string,original uuid.UUID)uuid.UUID{
 return uuid.NewSHA1(workflow,[]byte("interchange:historical-origin:"+entry.String()+":"+kind+":"+original.String()))
}
func(p PreparedImportArtifactPublisher)publishHistoricalOrigin(ctx context.Context,payload HistoricalOriginPayload,raw []byte)(v2input.Ref,error){
 if validateHistoricalOriginPayload(payload)!=nil||payload.Tenant!=p.Scope.Tenant||payload.Workflow!=p.Scope.Workflow||payload.Run!=p.Scope.Run||payload.Entry!=p.Scope.Entry||payload.Target!=p.Scope.Binding{return v2input.Ref{},v2input.ErrInvalid}
 copy:=payload;p.Scope.Historical=&copy
 ref:=v2input.Ref{ArtifactID:historicalReceiptID(payload.Workflow,payload.Entry,payload.Contract,payload.Original.ArtifactID),SHA256:rawSHA(raw)}
 return p.PublishImportDependency(ctx,payload.Entry,"HistoricalOriginReceipt",ref,raw)
}
// Every resolution reads actual immutable bytes and repeats native authority;
// cached signed bytes never substitute for current SQL/physical receipt checks.
type historicalOriginNodeValidator struct{Issuer HistoricalOriginIssuer;Expected HistoricalOriginPayload;Prepared ImportClosureNodeResolver}
func(v historicalOriginNodeValidator)ImportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 public,ok:=v.Issuer.Signer.Public().(ed25519.PublicKey);if !ok{return nil,v2input.ErrInvalid}
 actual,err:=VerifyHistoricalOriginReceipt(raw,v.Issuer.Issuer,v.Issuer.KeyID,public);if err!=nil{return nil,err}
 expected,_:=canonical.Marshal(v.Expected);received,_:=canonical.Marshal(actual)
 if entry.Contract!="HistoricalOriginReceipt"||entry.Ref.ArtifactID!=historicalReceiptID(actual.Workflow,actual.Entry,actual.Contract,actual.Original.ArtifactID)||!bytes.Equal(expected,received){return nil,v2input.ErrInvalid}
 node,err:=v.Prepared.ResolveImportNode(ctx,v2input.Entry{Ref:actual.Prepared,Contract:actual.Contract});if err!=nil{return nil,err}
 if err=v.Issuer.checkScope(ctx);err!=nil{return nil,err}
 authority:=v.Issuer.Authority;authority.Foreign=nil;authority.source=actual.Prepared
 if err=authority.QualifyPreparedHistoricalDocument(ctx,actual.Contract,node.Body,actual.Target);err!=nil{return nil,err}
 return []v2input.Entry{},ctx.Err()
}
func(q registeredPreparedImportRecipeQualifier)historicalReceipts(ctx context.Context,nodes map[v2input.Entry]ImportClosureNodeResolver)(map[v2input.Entry]ImportClosureNodeResolver,error){
 result:=map[v2input.Entry]ImportClosureNodeResolver{}
 for entry,resolver:=range nodes{
  if entry.Contract!="WorkingImage"&&entry.Contract!="FaceBoxes"&&entry.Contract!="FaceLandmarks"{continue}
  if entry.Ref==q.Scope.Binding.Source{continue}
  if entry.Contract=="WorkingImage"&&q.HistoricalSource==nil{continue}
  if entry.Contract=="WorkingImage"&&q.HistoricalSource!=nil{var parsed importParsedDocument;if decodeImportDocument(q.HistoricalSource.Catalog.Store.Parsed,&parsed)!=nil{return nil,v2input.ErrInvalid};historical:=false;for _,item:=range parsed.Manifest.Inventory{if item.Contract=="WorkingImage"&&entry.Ref.ArtifactID==uuid.NewSHA1(q.Scope.Workflow,[]byte("interchange:dependency:"+q.Scope.Entry.String()+":WorkingImage:"+item.ID.String())){historical=true;break}};if !historical{continue}}
  if q.Historical==nil||q.Historical.Signer==nil||q.Publisher==nil{return nil,&ImportRecipeUnsupported{Reason:"historical_receipt_issuer_not_installed"}}
  node,err:=resolver.ResolveImportNode(ctx,entry);if err!=nil{return nil,err}
  receipt,err:=q.Historical.Issue(ctx,entry.Contract,entry.Ref,node.Body);if err!=nil{return nil,err}
  raw,err:=canonical.Marshal(receipt);if err!=nil{return nil,err}
  public:=q.Historical.Signer.Public().(ed25519.PublicKey)
  payload,err:=VerifyHistoricalOriginReceipt(raw,receipt.Issuer,receipt.Key,public);if err!=nil{return nil,err}
  ref,err:=q.Publisher.publishHistoricalOrigin(ctx,payload,raw);if err!=nil{return nil,err}
  receiptEntry:=v2input.Entry{Ref:ref,Contract:"HistoricalOriginReceipt"}
  result[receiptEntry]=ImportJSONNodeResolver{Catalog:q.Catalog,Objects:q.Objects,Validator:historicalOriginNodeValidator{Issuer:*q.Historical,Expected:payload,Prepared:resolver}}
 }
 return result,ctx.Err()
}
