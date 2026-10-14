package interchange

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// VerifiedExportBundleBytes proves the immutable object's physical SHA and
// length, not ZIP members/manifest/native image semantics. Archive qualification
// must be performed before final adoption; this receipt alone cannot finish it.
type VerifiedExportBundleBytes struct {ref v2input.Ref;object objectstore.Object}
func(v VerifiedExportBundleBytes)Ref()v2input.Ref{return v.ref}
func(v VerifiedExportBundleBytes)Bytes()int64{return v.object.Bytes}
type ExportBundleReader struct {Catalog ExportOutputCatalog;Objects ImportDocumentObjects}
func(r ExportBundleReader)Read(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,ref v2input.Ref)(VerifiedExportBundleBytes,error){
 var zero VerifiedExportBundleBytes
 if err:=ctx.Err();err!=nil{return zero,err}
 if r.Catalog==nil||r.Objects==nil||!validImportRef(ref){return zero,v2input.ErrInvalid}
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return zero,err}
 for _,root:=range compiled.roots{if root.Ref.ArtifactID==ref.ArtifactID{return zero,v2input.ErrInvalid}}
 var plan importPlanDocument;if err:=decodeImportDocument(compiled.planBody,&plan);err!=nil{return zero,err}
 entry:=v2input.Entry{Ref:ref,Contract:"InterchangeArchive"}
 object,err:=r.Catalog.LookupExportOutput(ctx,entry);if err!=nil{return zero,err}
 if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.ContentType!="application/zip"||object.SHA256!=ref.SHA256||object.Bytes<2||object.Bytes>plan.Archive{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 stream,err:=r.Objects.Open(ctx,object);if err!=nil{return zero,err};if stream==nil{return zero,v2input.ErrInvalid}
 digest:=sha256.New();buffer:=make([]byte,256<<10);total:=int64(0);empty:=0
 for {
  if err=ctx.Err();err!=nil{break}
  // Consume at most one byte beyond the claimed length to detect substitution.
  limit:=int64(len(buffer));if remain:=object.Bytes-total+1;remain<limit{limit=remain}
  n,readErr:=stream.Read(buffer[:int(limit)])
  if n<0||n>int(limit){err=v2input.ErrInvalid;break}
  if n>0{empty=0;total+=int64(n);if total>object.Bytes{err=v2input.ErrInvalid;break};_,_=digest.Write(buffer[:n])}else{empty++;if empty>=100&&readErr==nil{err=io.ErrNoProgress;break}}
  if readErr==io.EOF{break};if readErr!=nil{err=readErr;break}
 }
 closeErr:=stream.Close()
 if err!=nil{return zero,err};if err:=ctx.Err();err!=nil{return zero,err};if closeErr!=nil{return zero,closeErr}
 if total!=object.Bytes||hex.EncodeToString(digest.Sum(nil))!=ref.SHA256{return zero,v2input.ErrInvalid}
 current,err:=r.Catalog.LookupExportOutput(ctx,entry);if err!=nil{return zero,err};if current!=object{return zero,v2input.ErrInvalid}
 return VerifiedExportBundleBytes{ref:ref,object:object},ctx.Err()
}
