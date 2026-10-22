package v2draft

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

func TestSemanticAnalysisPublicationRejectsBeforeStorage(t *testing.T){
 // A zero Publisher deliberately has no store: these failures must be checked
 // before publication can reserve quota or access an object backend.
 p:=Publisher{}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=p.PublishSemanticAnalysisParams(ctx,auth.Identity{},uuid.New(),uuid.New(),"scene.parse",v2input.SessionVersionBinding{},nil,"");!errors.Is(err,context.Canceled){t.Fatalf("cancel: %v",err)}
 for _,operation:=range []string{"scene.parse","depth.predict","face.detect"}{
  if _,err:=p.PublishSemanticAnalysisParams(context.Background(),auth.Identity{},uuid.New(),uuid.New(),operation,v2input.SessionVersionBinding{},nil,"");err==nil{t.Fatalf("missing actor accepted for %s",operation)}
 }
}
