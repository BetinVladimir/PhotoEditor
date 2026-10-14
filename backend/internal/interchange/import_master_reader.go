package interchange

import (
 "bytes"
 "context"
 "io"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ImportObjectMasterReader struct{Catalog ImportDocumentCatalog;Objects ImportDocumentObjects}

// ReadImportMaster performs bounded immutable object IO between two independent
// SQL authority lookups. It proves the typed document and its physical bytes;
// CompileImportPlan still checks original provenance, color and canonical canvas.
func(r ImportObjectMasterReader)ReadImportMaster(ctx context.Context,ref v2input.Ref,maxBytes int64)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if !validImportRef(ref)||r.Catalog==nil||r.Objects==nil||maxBytes<2||maxBytes>1<<20{return nil,v2input.ErrInvalid}
 entry:=v2input.Entry{Ref:ref,Contract:"WorkingImage"}
 object,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return nil,err}
 if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.ContentType!="application/json"||object.Bytes<2||object.Bytes>maxBytes||object.SHA256!=ref.SHA256{return nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return nil,err}
 reader,err:=r.Objects.Open(ctx,object);if err!=nil{if reader!=nil{_ = reader.Close()};return nil,err};if reader==nil{return nil,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx:ctx,reader:reader},object.Bytes+1));closeErr:=reader.Close()
 if err:=ctx.Err();err!=nil{return nil,err}
 if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr}
 if int64(len(raw))!=object.Bytes||rawSHA(raw)!=ref.SHA256{return nil,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return nil,v2input.ErrInvalid}
 var typed review.WorkingImage;if err:=decodeImportDocument(raw,&typed);err!=nil{return nil,err}
 if _,err:=review.ParseWorkingImage(raw);err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err}
 current,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return nil,err}
 if current!=object{return nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return nil,err}
 return raw,nil
}

func NewSQLImportMasterReader(tenant,workflow uuid.UUID,parsedRaw []byte,deps ImportResolverDependencies)(ImportObjectMasterReader,error){
 if tenant==uuid.Nil||workflow==uuid.Nil||deps.DB==nil||deps.Objects==nil||len(deps.Bucket)<3||len(deps.Bucket)>63{return ImportObjectMasterReader{},v2input.ErrInvalid}
 parsedRaw=append([]byte(nil),parsedRaw...)
 if _,err:=validateParsedImportDocument(context.Background(),parsedRaw);err!=nil{return ImportObjectMasterReader{},err}
 return ImportObjectMasterReader{Catalog:SQLImportDocumentCatalog{DB:deps.DB,Tenant:tenant,Workflow:workflow,Parsed:parsedRaw,Bucket:deps.Bucket},Objects:deps.Objects},nil
}
