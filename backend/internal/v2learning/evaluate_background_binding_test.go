package v2learning

import (
 "testing"
 "photoeditor/backend/internal/v2draft"
)

func TestFrozenBackgroundStyleAndPinMustAgree(t *testing.T){
 pin:=&v2draft.FrozenBackgroundAsset{PortableAssetID:"installed"}
 for _,tc:=range []struct{name,style string;pin *v2draft.FrozenBackgroundAsset;valid bool}{
  {"legacy",`{}`,nil,true},
  {"asset",`{"extensions":{"background":{"mode":"asset","asset_id":"installed"}}}`,pin,true},
  {"missing",`{"extensions":{"background":{"mode":"asset","asset_id":"installed"}}}`,nil,false},
  {"foreign",`{"extensions":{"background":{"mode":"asset","asset_id":"foreign"}}}`,pin,false},
  {"null_id",`{"extensions":{"background":{"mode":"asset","asset_id":null}}}`,pin,false},
  {"extra",`{}`,pin,false},
  {"keep",`{"extensions":{"background":{"mode":"keep"}}}`,nil,true},
  {"unused",`{"extensions":{"background":{"mode":"solid"}}}`,pin,false},
  {"unknown",`{"extensions":{"background":{"mode":"unknown"}}}`,nil,false},
  {"malformed",`{"extensions":[]}`,nil,false},
 }{t.Run(tc.name,func(t *testing.T){err:=validateFrozenBackgroundBinding([]byte(tc.style),tc.pin);if (err==nil)!=tc.valid{t.Fatalf("unexpected binding result: %v",err)}})}
}
