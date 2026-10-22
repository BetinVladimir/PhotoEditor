package v2region

import (
 "bytes"
 "strings"
 "testing"
 "photoeditor/backend/internal/canonical"
)

func TestAuthoredRuntimeProfileCannotCarryRequestScope(t *testing.T){
 policy:=SQLAuthoredRegionPolicy{PresentationRuntimeSHA:strings.Repeat("a",64),PresentationICCLockSHA:strings.Repeat("b",64)}
 baseline:=map[string]any{"backend":"cpu","renderer_version":"v2.2","runtime_lock_sha":policy.PresentationRuntimeSHA,"icc_lock_sha":policy.PresentationICCLockSHA}
 body,err:=canonical.Marshal(baseline);if err!=nil{t.Fatal(err)}
 good,err:=validateAuthoredRuntimeProfile(policy,body);if err!=nil||!bytes.Equal(good,body){t.Fatalf("installed profile refused: %v",err)}
 good[0]='x';if body[0]!='{'{t.Fatal("returned profile aliases caller bytes")}
 for _,key:=range []string{"contracts_sha","v2_input_manifest","v2_entry_count","v2_entries_sha","authored_region_intent_sha256","authored_region_input_bytes","resource_estimate"}{
  t.Run(key,func(t *testing.T){m:=map[string]any{};for k,v:=range baseline{m[k]=v};m[key]=nil;raw,e:=canonical.Marshal(m);if e!=nil{t.Fatal(e)};if _,e=validateAuthoredRuntimeProfile(policy,raw);e!=ErrRegionInvalid{t.Fatalf("request-scoped key accepted: %v",e)}})
 }
 for _,key:=range []string{"backend","renderer_version","runtime_lock_sha","icc_lock_sha"}{
  t.Run("mismatch_"+key,func(t *testing.T){m:=map[string]any{};for k,v:=range baseline{m[k]=v};m[key]="foreign";raw,e:=canonical.Marshal(m);if e!=nil{t.Fatal(e)};if _,e=validateAuthoredRuntimeProfile(policy,raw);e!=ErrRegionInvalid{t.Fatalf("foreign installation accepted: %v",e)}})
 }
}
