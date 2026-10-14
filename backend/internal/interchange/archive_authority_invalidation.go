package interchange
import (
 "context"
 "time"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)
// Marks lost-authority queued uploads as aborted intent. Physical revocation and
// purge remain independently leased stages; this never changes usage accounting.
type ArchiveAuthorityInvalidation struct{DB artifactjson.RowQuerier}
func(s ArchiveAuthorityInvalidation)ProcessOne(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err};if s.DB==nil{return false,v2input.ErrInvalid}
 bounded,stop:=context.WithTimeout(ctx,5*time.Second);defer stop()
 var changed bool
 if err:=s.DB.QueryRow(bounded,`SELECT photo.invalidate_v30_archive_authority()`).Scan(&changed);err!=nil{return false,err}
 return changed,bounded.Err()
}
