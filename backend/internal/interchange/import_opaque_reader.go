package interchange
import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "photoeditor/backend/internal/v2input"
)
// Opaque verification proves bytes and scope only. It does not qualify NPY masks,
// encoded images or recipe JSON for use as canonical processing authority.
type ImportOpaqueNodeResolver struct{Catalog ImportDocumentCatalog;Objects ImportDocumentObjects;MaxBytes int64}
func(r ImportOpaqueNodeResolver)ResolveImportNode(ctx context.Context,entry v2input.Entry)(ImportClosureNode,error){
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 if entry.Contract!="Artifact"||!validImportRef(entry.Ref)||r.Catalog==nil||r.Objects==nil||r.MaxBytes<1||r.MaxBytes>128<<30{return ImportClosureNode{},v2input.ErrInvalid}
 return resolveImportBinary(ctx,entry,r.Catalog,r.Objects,r.MaxBytes)
}
// Physical streaming is shared; callers select a role-specific scoped catalog.
func resolveImportBinary(ctx context.Context,entry v2input.Entry,catalog ImportDocumentCatalog,objects ImportDocumentObjects,maxBytes int64)(ImportClosureNode,error){
 object,err:=catalog.LookupImportDocument(ctx,entry);if err!=nil{return ImportClosureNode{},err}
 if object.Bytes<1||object.Bytes>maxBytes||object.SHA256!=entry.Ref.SHA256||object.ContentType!="application/octet-stream"||object.Key==""||object.VersionID==""||object.VersionID=="null"{return ImportClosureNode{},v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 reader,err:=objects.Open(ctx,object);if err!=nil{return ImportClosureNode{},err};if reader==nil{return ImportClosureNode{},v2input.ErrInvalid}
 hash:=sha256.New();limited:=io.LimitReader(reader,object.Bytes+1);buffer:=make([]byte,256<<10);var total int64
 for{
  if err:=ctx.Err();err!=nil{_ = reader.Close();return ImportClosureNode{},err}
  n,readErr:=limited.Read(buffer);if n>0{_,_ = hash.Write(buffer[:n]);total+=int64(n)}
  if readErr==io.EOF{break};if readErr!=nil{_ = reader.Close();return ImportClosureNode{},readErr}
  if n==0{_ = reader.Close();return ImportClosureNode{},io.ErrNoProgress}
 }
 if err:=reader.Close();err!=nil{return ImportClosureNode{},err}
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 if total!=object.Bytes||hex.EncodeToString(hash.Sum(nil))!=entry.Ref.SHA256{return ImportClosureNode{},v2input.ErrInvalid}
 current,err:=catalog.LookupImportDocument(ctx,entry);if err!=nil{return ImportClosureNode{},err}
 if current!=object{return ImportClosureNode{},v2input.ErrInvalid}
 return ImportClosureNode{Entry:entry},ctx.Err()
}
type ImportNodeResolverRouter struct{Prepared *ImportPreparedNodeRegistry;JSON ImportClosureNodeResolver;Opaque ImportClosureNodeResolver;Pixels ImportClosureNodeResolver;Warps ImportClosureNodeResolver}
func(r ImportNodeResolverRouter)ResolveImportNode(ctx context.Context,entry v2input.Entry)(ImportClosureNode,error){
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 if r.Prepared.Contains(entry){return r.Prepared.ResolveImportNode(ctx,entry)}
 if entry.Contract=="DenseWarpField"{if r.Warps==nil{return ImportClosureNode{},v2input.ErrInvalid};return r.Warps.ResolveImportNode(ctx,entry)}
 if entry.Contract=="Artifact"{if r.Opaque==nil{return ImportClosureNode{},v2input.ErrInvalid};return r.Opaque.ResolveImportNode(ctx,entry)}
 if entry.Contract=="WorkingPixels"||entry.Contract=="WorkingAlpha"{if r.Pixels==nil{return ImportClosureNode{},v2input.ErrInvalid};return r.Pixels.ResolveImportNode(ctx,entry)}
 if r.JSON==nil{return ImportClosureNode{},v2input.ErrInvalid};return r.JSON.ResolveImportNode(ctx,entry)
}

// Canonical authority comes from the witnessed ingest master and dedicated SQL
// producer gate, not merely successful streaming SHA verification.
type ImportCanonicalPixelNodeResolver struct{Catalog ImportDocumentCatalog;Objects ImportDocumentObjects;MaxBytes int64}
func(r ImportCanonicalPixelNodeResolver)ResolveImportNode(ctx context.Context,entry v2input.Entry)(ImportClosureNode,error){
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 if (entry.Contract!="WorkingPixels"&&entry.Contract!="WorkingAlpha")||!validImportRef(entry.Ref)||r.Catalog==nil||r.Objects==nil||r.MaxBytes<1||r.MaxBytes>128<<30{return ImportClosureNode{},v2input.ErrInvalid}
 return resolveImportBinary(ctx,entry,r.Catalog,r.Objects,r.MaxBytes)
}

// Uses only the dedicated witnessed canonical ingest warp catalogue.
type ImportCanonicalWarpNodeResolver struct{Catalog ImportDocumentCatalog;Objects ImportDocumentObjects;MaxBytes int64;Qualifier ExportBinaryQualifier}
func(r ImportCanonicalWarpNodeResolver)ResolveImportNode(ctx context.Context,entry v2input.Entry)(ImportClosureNode,error){
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 if entry.Contract!="DenseWarpField"||!validImportRef(entry.Ref)||r.Catalog==nil||r.Objects==nil||r.MaxBytes<1||r.MaxBytes>128<<30||r.Qualifier==nil{return ImportClosureNode{},v2input.ErrInvalid}
 object,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return ImportClosureNode{},err}
 if object.Bytes<1||object.Bytes>r.MaxBytes||object.SHA256!=entry.Ref.SHA256{return ImportClosureNode{},v2input.ErrInvalid}
 if err:=r.Qualifier.QualifyExportBinary(ctx,entry,object);err!=nil{return ImportClosureNode{},err}
 final,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return ImportClosureNode{},err};if final!=object{return ImportClosureNode{},v2input.ErrInvalid}
 return ImportClosureNode{Entry:entry},ctx.Err()
}

func(r ImportNodeResolverRouter)AdditionalImportRoots(ctx context.Context)([]v2input.Entry,error){return r.Prepared.AdditionalImportRoots(ctx)}

func(r ImportNodeResolverRouter)AdditionalImportRootsFor(ctx context.Context,reachable []v2input.Entry)([]v2input.Entry,error){return r.Prepared.AdditionalImportRootsFor(ctx,reachable)}
