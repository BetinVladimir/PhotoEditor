package interchange

import (
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)

func TestArchiveStorageGenerationRequiresExactScopedKey(t *testing.T){
 tenant,upload,generation:=uuid.New(),uuid.New(),uuid.New()
 legacy,_:=objectstore.InterchangeUploadKey(tenant,upload)
 got,err:=archiveStorageGeneration(tenant,upload,legacy);if err!=nil||got!=uuid.Nil{t.Fatal(got,err)}
 key,_:=objectstore.InterchangeGenerationKey(tenant,upload,generation)
 got,err=archiveStorageGeneration(tenant,upload,key);if err!=nil||got!=generation{t.Fatal(got,err)}
 for _,bad:=range []string{key+"/extra",legacy+"/extra"}{if _,err:=archiveStorageGeneration(tenant,upload,bad);err==nil{t.Fatal("accepted malformed key",bad)}}
 if _,err:=archiveStorageGeneration(uuid.New(),upload,key);err==nil{t.Fatal("accepted foreign tenant")}
 if _,err:=archiveStorageGeneration(tenant,uuid.New(),key);err==nil{t.Fatal("accepted foreign upload")}
}
