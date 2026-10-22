package httpapi

import (
 "net/http/httptest"
 "testing"
)

func TestRelightReplayHeaderRejectsAmbiguousModes(t *testing.T) {
 for _,values:=range [][]string{{"false"},{"TRUE"},{""},{"true,true"},{"true","true"},{"true","false"}} {
  r:=httptest.NewRequest("POST","/",nil)
  for _,value:=range values {r.Header.Add("Relight-Replay-Only",value)}
  if ctx,err:=relightRequestContext(r);err==nil||ctx!=nil {t.Fatal("unsafe replay mode",values)}
 }
}
func TestRelightReplayHeaderAcceptsOnlyExplicitTrue(t *testing.T) {
 r:=httptest.NewRequest("POST","/",nil)
 ctx,err:=relightRequestContext(r)
 if err!=nil||ctx!=r.Context() {t.Fatal("ordinary request changed",err)}
 r.Header.Set("Relight-Replay-Only","true")
 ctx,err=relightRequestContext(r)
 if err!=nil||ctx==nil||ctx==r.Context() {t.Fatal("explicit replay mode missing",err)}
}
