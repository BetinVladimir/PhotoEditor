package semanticproposal

import (
 "bytes"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestDetectorRecoveryFreezesScenePolicyWithoutChangingTask(t *testing.T){
 costs:=v2input.SemanticQualifiedCosts{RAMBytes:123,VRAMBytes:256,ScratchBytes:321}
 profile,err:=v2input.CompileSemanticDetectorProfile(8,8,1024,costs,1<<40,1<<40,1<<40,1<<40);if err!=nil{t.Fatal(err)}
 task:=v2input.DraftTask{Operation:"face.detect",ExecutionProfile:profile}
 report:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 legacy,err:=nativePlanRecoveryFromTask(task,&report,costs,16<<20);if err!=nil{t.Fatal(err)}
 old,err:=canonical.Marshal(legacy);if err!=nil{t.Fatal(err)}
 if bytes.Contains(old,[]byte("scene_policy")){t.Fatal("historical metadata changed")}
 policy:=sceneDeploymentPolicyFixture();legacy.ScenePolicy=&policy
 saved,err:=canonical.Marshal(legacy);if err!=nil{t.Fatal(err)}
 recovered,err:=decodeNativePlanRecoveryMetadata(saved,task);if err!=nil{t.Fatal(err)}
 if recovered.ScenePolicy==nil || recovered.ScenePolicy.Scene.Report!=policy.Scene.Report || recovered.ScenePolicy.Depth.Report!=policy.Depth.Report {t.Fatal("restart lost independent producer pins")}
 reconstructed,err:=canonical.Marshal(recovered);if err!=nil||!bytes.Equal(saved,reconstructed){t.Fatal("frozen accepted recovery bytes changed",err)}
 changed:=task;changed.Operation="face.landmarks"
 if _,err:=decodeNativePlanRecoveryMetadata(saved,changed);err==nil{t.Fatal("foreign stage accepted original parent policy")}
}
