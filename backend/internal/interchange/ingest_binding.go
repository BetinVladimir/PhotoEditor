package interchange
import(
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "strconv"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
type ImportedCatalogIdentity struct{AssetID,VersionID,ItemID uuid.UUID}

// BindImportedIngest calls the dedicated scheduler admission inside caller's
// tenant transaction. Published/adopted parser bytes remain mandatory in SQL.
func BindImportedIngest(ctx context.Context,tx ProofTransaction,who auth.Identity,tenant,job,run uuid.UUID,parsed v2input.Ref,body []byte,ordinal int)(ImportedCatalogIdentity,error){
 invalid:=func()(ImportedCatalogIdentity,error){return ImportedCatalogIdentity{},domain.NewError("VALIDATION_FAILED",422)}
 if err:=ctx.Err();err!=nil{return ImportedCatalogIdentity{},err}
 if tx==nil||tenant==uuid.Nil||job==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||parsed.ArtifactID==uuid.Nil||ordinal<0||ordinal>4999||len(body)<2||len(body)>8<<20||run!=uuid.NewSHA1(job,[]byte("interchange:canonical-ingest:1")){return invalid()}
 sum:=sha256.Sum256(body);if hex.EncodeToString(sum[:])!=parsed.SHA256{return invalid()}
 var result ImportedCatalogIdentity
 err:=tx.QueryRow(ctx,`SELECT asset_id,version_id,item_id FROM photo.bind_v30_interchange_ingest($1,$2,$3,$4,$5,$6,$7,$8,$9)`,tenant,who.UserID,who.SessionID,run,job,parsed.ArtifactID,parsed.SHA256,string(body),ordinal).Scan(&result.AssetID,&result.VersionID,&result.ItemID)
 if errors.Is(err,pgx.ErrNoRows){return ImportedCatalogIdentity{},domain.NewError("INTERCHANGE_SOURCE_STALE",409)}
 if err!=nil{return ImportedCatalogIdentity{},err}
 if result.AssetID!=uuid.NewSHA1(run,[]byte("asset:"+strconv.Itoa(ordinal)))||result.VersionID!=uuid.NewSHA1(run,[]byte("version:"+strconv.Itoa(ordinal)))||result.ItemID!=uuid.NewSHA1(run,[]byte("item:"+strconv.Itoa(ordinal))){return invalid()}
 return result,nil
}
