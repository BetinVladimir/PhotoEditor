package main
import("encoding/json";"os";"testing";"time";"photoeditor/backend/internal/workflow")
func TestActualSavedAdvanceScopeLimits(t *testing.T){
 raw,e:=os.ReadFile("/Users/freelancer/Documents/PhotoEditor/work/user-three-finalize-root-r1/scope.private.json");if e!=nil{t.Fatal(e)}
 var s Scope;if e=json.Unmarshal(raw,&s);e!=nil{t.Fatal(e)};s.Observed=time.Now().UTC()
 if e=validateAdvanceScope(s);e==nil{t.Fatal("missing actual limits accepted")};t.Log("actual saved scope rejected:",e)
 s.VolumeByteLimit=workflow.DefaultVolumeByteLimit;s.VolumeFileLimit=workflow.MaxDownloadItems
 if e=validateAdvanceScope(s);e!=nil{t.Fatal("normal limits still reject actual scope:",e)}
}
