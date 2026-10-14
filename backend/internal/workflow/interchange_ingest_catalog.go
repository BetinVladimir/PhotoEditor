package workflow
import(
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/artifactjson"
)
// ImportedCatalogAdopter reuses exact WorkingImage/metadata validation. Its DB
// is a caller-owned scheduler tenant transaction; no object IO occurs here.
type ImportedCatalogAdopter struct{DB artifactjson.RowQuerier;Who auth.Identity;WorkICCSHA string}
func(r ImportedCatalogAdopter)AdoptImage(ctx context.Context,tenant,run uuid.UUID,image CatalogImage)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if r.DB==nil||tenant==uuid.Nil||run==uuid.Nil||r.Who.UserID==uuid.Nil||r.Who.SessionID==uuid.Nil||image.Ordinal<0||image.Ordinal>4999||image.DecodeTaskID==uuid.Nil||image.MetadataRef.ID==uuid.Nil||image.Source.ID==uuid.Nil||image.Metadata.Source!=image.Source{return false,domain.NewError("VALIDATION_FAILED",422)}
 if _,err:=domain.ParseDigest(image.MetadataRef.SHA256);err!=nil{return false,err}
 raw,err:=canonical.Marshal(image.Image);if err!=nil{return false,err}
 if _,err:=ReadDecodedImage(ctx,bytes.NewReader(raw),image.Outputs,image.Metadata,r.WorkICCSHA);err!=nil{return false,err}
 metadata,err:=canonical.Marshal(image.Metadata);if err!=nil{return false,err}
 var accepted bool
 err=r.DB.QueryRow(ctx,`SELECT photo.adopt_v30_interchange_ingest_image($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,tenant,r.Who.UserID,r.Who.SessionID,run,image.Ordinal,image.Outputs.Manifest.ID,image.Outputs.Manifest.SHA256,string(raw),string(metadata),image.MetadataRef.ID,image.MetadataRef.SHA256).Scan(&accepted)
 return accepted,err
}

var _ IngestCatalogWriter=ImportedCatalogAdopter{}
func(r ImportedCatalogAdopter)RejectFile(ctx context.Context,tenant,run uuid.UUID,f IngestItemFailure)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if r.DB==nil||tenant==uuid.Nil||run==uuid.Nil||r.Who.UserID==uuid.Nil||r.Who.SessionID==uuid.Nil||f.Ordinal<0||f.Ordinal>4999||f.Source.ID==uuid.Nil||(f.Stage!="asset.verify"&&f.Stage!="asset.inspect"&&f.Stage!="asset.decode")||(f.Reason!="failed"&&f.Reason!="cancelled"){return false,domain.NewError("VALIDATION_FAILED",422)}
 if _,err:=domain.ParseDigest(f.Source.SHA256);err!=nil{return false,err}
 var accepted bool
 err:=r.DB.QueryRow(ctx,`SELECT photo.reject_v30_interchange_ingest($1,$2,$3,$4,$5,$6,$7,$8,$9)`,tenant,r.Who.UserID,r.Who.SessionID,run,f.Ordinal,f.Source.ID,f.Source.SHA256,f.Stage,f.Reason).Scan(&accepted)
 return accepted,err
}
func(r ImportedCatalogAdopter)Finish(ctx context.Context,tenant,run uuid.UUID)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 if r.DB==nil||tenant==uuid.Nil||run==uuid.Nil||r.Who.UserID==uuid.Nil||r.Who.SessionID==uuid.Nil{return "",domain.NewError("VALIDATION_FAILED",422)}
 var state *string
 if err:=r.DB.QueryRow(ctx,`SELECT photo.finish_v30_interchange_ingest($1,$2,$3,$4)`,tenant,r.Who.UserID,r.Who.SessionID,run).Scan(&state);err!=nil{return "",err}
 if state==nil{return "",nil}
 switch *state{case "completed","completed_with_errors","failed":return *state,nil}
 return "",domain.NewError("INTERCHANGE_SOURCE_STALE",409)
}
