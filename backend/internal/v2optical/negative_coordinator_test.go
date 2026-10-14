package v2optical

import (
 "context"
 "testing"
 "photoeditor/backend/internal/artifactjson"
)
func TestNegativeCapacityCeilingsNormalizeOnlyRequestPixelCount(t *testing.T) {
 required,err:=NegativeResources(10,10,true);if err!=nil { t.Fatal(err) }
 capacity:=required;capacity.PixelCount=200
 got,err:=negativeRequestCapacity(required,capacity)
 if err!=nil || got.PixelCount!=100 || capacity.PixelCount!=200 { t.Fatal("capacity normalization",err) }
 for _,fault:=range []string{"pixels","vram","ram","material","output_scratch","scratch","output","overflow"} {
  changed:=capacity
  switch fault {
  case "pixels":changed.PixelCount=99
  case "vram":changed.VRAMBytes=1
  case "ram":changed.RAMBytes--
  case "material":changed.MaterialScratchBytes--
  case "output_scratch":changed.OutputScratchBytes--
  case "scratch":changed.ScratchBytes--
  case "output":changed.OutputBytes--
  case "overflow":changed.RAMBytes=9007199254740992
  }
  if _,err:=negativeRequestCapacity(required,changed);err==nil { t.Fatal("capacity fault accepted",fault) }
 }
}
func TestPreparedNegativeBindsOwnerKeyAndOriginalBytes(t *testing.T) {
 draft,located,who,params:=negativeParamsFixture(t)
 qualified,_:=located.Qualification();_,ref,source,err:=draft.RestorePlan(context.Background(),qualified);if err!=nil { t.Fatal(err) }
 snapshot,_:=draft.Snapshot()
 p:=PreparedNegative{draft:draft,params:artifactjson.Ref{ID:ref.ArtifactID,SHA256:ref.SHA256},qualification:located}
 // The fixture key is intentionally unknown: neither a foreign key nor changed
 // source bytes may reuse this server-owned prepared capability.
 r:=NegativePrepareRequest{Tenant:snapshot.Owner.TenantID,Project:snapshot.Owner.ProjectID,Snapshot:snapshot.Owner.SnapshotID,Key:"foreign-prepared-key",Params:params,Source:source}
 if err:=p.bind(context.Background(),who,r);err==nil { t.Fatal("foreign idempotency key accepted") }
 r.Source=[]byte(`{}`)
 if err:=p.bind(context.Background(),who,r);err==nil { t.Fatal("foreign source accepted") }
}
