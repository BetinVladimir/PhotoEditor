package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// This catalogue is invocation-local to one selected DecideImport target.
// It permits preparation of an approval witness without making unaccepted
// imported dependencies visible to ordinary rendering or another principal.
type ImportApprovalCatalog struct{
 tenant uuid.UUID
 who auth.Identity
 prepared map[v2input.Ref]string
 reader ImportDocumentCatalog
 fallback v2draft.ArtifactCatalog
}
func NewImportApprovalCatalog(tenant uuid.UUID,who auth.Identity,entries []v2input.Entry,reader ImportDocumentCatalog,fallback v2draft.ArtifactCatalog)(*ImportApprovalCatalog,error){
 if tenant==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||reader==nil||fallback==nil||len(entries)<1||len(entries)>4096{return nil,v2input.ErrInvalid}
 out:=&ImportApprovalCatalog{tenant:tenant,who:who,prepared:map[v2input.Ref]string{},reader:reader,fallback:fallback}
 ids:=map[uuid.UUID]bool{}
 for _,entry:=range entries{
  switch entry.Contract{case "StyleV2","CorrectionPlan","Override","LayerStack","MaskRef","Params":default:return nil,v2input.ErrInvalid}
  if !validImportRef(entry.Ref)||ids[entry.Ref.ArtifactID]{return nil,v2input.ErrInvalid}
  ids[entry.Ref.ArtifactID]=true;out.prepared[entry.Ref]=entry.Contract
 }
 return out,nil
}
func(c *ImportApprovalCatalog)Lookup(ctx context.Context,who auth.Identity,tenant uuid.UUID,node v2draft.LayerRef)(objectstore.Object,error){
 if err:=ctx.Err();err!=nil{return objectstore.Object{},err}
 if c==nil||tenant!=c.tenant||who.UserID!=c.who.UserID||who.SessionID!=c.who.SessionID||!validImportRef(node.Ref){return objectstore.Object{},v2input.ErrInvalid}
 contract,selected:=c.prepared[node.Ref]
 if !selected{
  // A changed hash for an admitted UUID must not fall through to another ACL.
  for ref:=range c.prepared{if ref.ArtifactID==node.Ref.ArtifactID{return objectstore.Object{},v2input.ErrInvalid}}
  return c.fallback.Lookup(ctx,who,tenant,node)
 }
 if contract!=node.Contract{return objectstore.Object{},v2input.ErrInvalid}
 // No fallback on stale/forbidden selected refs. The bound prepared catalogue
 // rechecks exact workflow/entry/target/source certificates on every call.
 return c.reader.LookupImportDocument(ctx,v2input.Entry{Ref:node.Ref,Contract:node.Contract})
}
var _ v2draft.ArtifactCatalog=(*ImportApprovalCatalog)(nil)
