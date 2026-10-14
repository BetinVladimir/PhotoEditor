package v2optical

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestManualPerspectiveReplacementReceiptRequiresCurrentProofs(t *testing.T){
 p:=PreparedNegativeReplacementInputs{bucket:"photos"};db:=&negativeDraftStoreDB{}
 got,err:=p.ReadManualPerspectiveReplacementQueuedReceipt(context.Background(),db,"photos",nil,nil,nil,nil,PerspectiveReplacementDraft{raw:"{}"},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{},v2input.Ref{})
 if err==nil||got.Task!=uuid.Nil||db.begins!=0{t.Fatal("unproved receipt read reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=p.ReadManualPerspectiveReplacementQueuedReceipt(ctx,db,"photos",nil,nil,nil,nil,PerspectiveReplacementDraft{},uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{},v2input.Ref{});!errors.Is(err,context.Canceled){t.Fatal(err)}
}

func TestManualPerspectiveReplacementReceiptParserRejectsUnscopedPacket(t *testing.T){
 for _,raw:=range [][]byte{nil,[]byte(`{}`),[]byte(`{"schema_version":1}`),make([]byte,65537)}{
  if _,err:=parseManualPerspectiveReplacementReceipt(context.Background(),perspectiveReplacementDraftSnapshot{},[]byte(`{}`),PerspectiveQualificationPin{},raw);err==nil{t.Fatal("unscoped receipt accepted")}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=parseManualPerspectiveReplacementReceipt(ctx,perspectiveReplacementDraftSnapshot{},nil,PerspectiveQualificationPin{},nil);!errors.Is(err,context.Canceled){t.Fatal(err)}
}
