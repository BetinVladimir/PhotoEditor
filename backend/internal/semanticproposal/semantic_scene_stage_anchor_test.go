package semanticproposal

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestScenePrerequisiteIdentityIsExactBindingAndParentBound(t *testing.T){
 raw,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil{t.Fatal(err)}
 var images []json.RawMessage;if json.Unmarshal(raw,&images)!=nil||len(images)==0{t.Fatal("image fixture")}
 body:=images[0];image,err:=review.ParseWorkingImage(body);if err!=nil{t.Fatal(err)}
 canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 digest:=sha256.Sum256(body)
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(digest[:])},CanvasHash:canvas}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};project:=uuid.New()
 request:=Request{TenantID:uuid.New(),RunID:uuid.New(),RequestSHA:strings.Repeat("a",64)}
 scene,err:=compileSemanticSceneStageAnchor(context.Background(),who,project,request,binding,body,"scene.parse");if err!=nil{t.Fatal(err)}
 reused,err:=compileSemanticSceneStageAnchor(context.Background(),who,project,request,binding,body,"scene.parse");if err!=nil||scene!=reused{t.Fatal("reference reuse changed durable child identity")}
 depth,err:=compileSemanticSceneStageAnchor(context.Background(),who,project,request,binding,body,"depth.predict");if err!=nil||scene.run==depth.run||scene.task==depth.task||scene.document==depth.document||scene.namespace!=depth.namespace{t.Fatal("analysis operations not independently pinned")}
 foreign:=request;foreign.RunID=uuid.New()
 if err:=verifySemanticSceneStageAnchor(context.Background(),who,project,foreign,scene);err==nil{t.Fatal("foreign parent accepted")}
 foreignWho:=who;foreignWho.SessionID=uuid.New()
 if err:=verifySemanticSceneStageAnchor(context.Background(),foreignWho,project,request,scene);err==nil{t.Fatal("foreign session accepted")}
 changed:=binding;changed.AssetVersionID=uuid.New()
 other,err:=compileSemanticSceneStageAnchor(context.Background(),who,project,request,changed,body,"scene.parse");if err!=nil||other.run==scene.run{t.Fatal("same bytes from another version lost identity")}
}

func TestScenePrerequisiteCancellationBeforeMetadataRead(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=compileSemanticSceneStageAnchor(ctx,auth.Identity{},uuid.Nil,Request{},v2input.SessionVersionBinding{},nil,"scene.parse");!errors.Is(err,context.Canceled){t.Fatalf("cancellation: %v",err)}
}
