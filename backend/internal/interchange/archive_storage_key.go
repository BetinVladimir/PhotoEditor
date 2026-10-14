package interchange

import (
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)

// A nil generation denotes the historical, deterministic archive key. The
// SQL reader additionally proves that a non-nil generation is bound to this
// upload and the same multipart receipt; syntax alone grants no authority.
func archiveStorageGeneration(tenant,upload uuid.UUID,key string)(uuid.UUID,error){
 legacy,err:=objectstore.InterchangeUploadKey(tenant,upload)
 if err!=nil{return uuid.Nil,err}
 if key==legacy{return uuid.Nil,nil}
 gotTenant,gotUpload,generation,err:=objectstore.ParseInterchangeGenerationKey(key)
 if err!=nil||gotTenant!=tenant||gotUpload!=upload{return uuid.Nil,objectstore.ErrIntegrity}
 return generation,nil
}
