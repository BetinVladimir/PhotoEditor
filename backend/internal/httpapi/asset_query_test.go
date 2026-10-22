package httpapi

import (
 "net/url"
 "testing"
)

func TestAssetUploadStateQueryPreservesDocumentedFilterAndLegacyAlias(t *testing.T){
 for _,tc:=range []struct{raw,want string;bad bool}{
  {"","",false},{"upload_state=verified","verified",false},{"status=pending","pending",false},
  {"upload_state=verified&status=verified","verified",false},
  {"upload_state=verified&status=rejected","",true},
  {"upload_state=pending&upload_state=verified","",true},{"status=pending&status=pending","",true},
 }{q,e:=url.ParseQuery(tc.raw);if e!=nil{t.Fatal(e)};got,e:=assetUploadStateQuery(q);if (e!=nil)!=tc.bad||got!=tc.want{t.Fatal(tc,got,e)}}
}
