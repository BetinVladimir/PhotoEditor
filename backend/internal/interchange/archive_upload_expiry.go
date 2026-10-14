package interchange
import (
 "context"
 "time"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)
// Expiration fences future upload actions; quota stays reserved until cleanup.
type ArchiveUploadExpiry struct{DB artifactjson.RowQuerier}
func(s ArchiveUploadExpiry)ProcessOne(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err};if s.DB==nil{return false,v2input.ErrInvalid}
 bounded,stop:=context.WithTimeout(ctx,5*time.Second);defer stop()
 var expired bool;err:=s.DB.QueryRow(bounded,`SELECT photo.expire_v30_archive_upload()`).Scan(&expired)
 if err!=nil{return false,err};if err:=bounded.Err();err!=nil{return false,err};return expired,nil
}
