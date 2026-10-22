package collection

import (
 "errors"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/domain"
)
func TestIntegrationShareUsageCurrentTenantParentAndNoSecrets(t *testing.T){
 ctx,admin,service,who,tid,collection,other:=sectionAbandonFixture(t);service.Pepper=[]byte(strings.Repeat("p",32))
 created,err:=service.CreateShare(ctx,who,tid,collection,uuid.NewString(),shareIntentRequest());if err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.share_links SET used_files=2,used_bytes=1048576,reserved_files=1,reserved_bytes=500 WHERE tenant_id=$1 AND id=$2`,tid,created.ID);err!=nil{t.Fatal(err)}
 usage,err:=service.ShareUsage(ctx,who,tid,collection,created.ID);if err!=nil||usage.ShareID!=created.ID||usage.UploadedFiles!="0"||usage.UploadedBytes!="0"||usage.QuotaUsedFiles!="2"||usage.QuotaUsedBytes!="1048576"||usage.ReservedFiles!="1"||usage.ReservedBytes!="500"||usage.DownloadAuthorizations!="0"||usage.DownloadJobs!="0"{t.Fatalf("usage: %+v %v",usage,err)}
 _,err=service.ShareUsage(ctx,who,tid,other,created.ID);var failure *domain.Error;if !errors.As(err,&failure)||failure.Code!="NOT_FOUND"{t.Fatalf("cross collection usage: %v",err)}
}
