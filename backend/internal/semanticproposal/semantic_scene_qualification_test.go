package semanticproposal

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestSceneProducerQualificationRefusesMissingAuthorityAndCancelledIO(t *testing.T){
 reader:=SQLTransferSourceReader{}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 proof,err:=reader.readSceneProducerQualification(ctx,auth.Identity{},uuid.Nil,TransferSceneProducer{})
 if !errors.Is(err,context.Canceled)||proof!=(semanticSceneProducerProof{}){t.Fatal("cancelled producer retained proof")}
 for _,operation:=range []string{"semantic.transfer","face.parse","","scene.parse","depth.predict"}{
  configuration:=TransferSceneProducer{Expected:v2qualification.Expected{Operation:operation,Version:"1",Backend:"cuda"}}
  proof,err=reader.readSceneProducerQualification(context.Background(),auth.Identity{},uuid.Nil,configuration)
  if err==nil||proof!=(semanticSceneProducerProof{}){t.Fatal("absent physical/catalog authority qualified producer")}
 }
}
func TestSceneProducerAdmissionRefusesUnboundProofBeforeSQL(t *testing.T){
 reader:=SQLTransferSourceReader{ProjectID:uuid.New()}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 // A private proof cannot be reused with another project/user/session or with
 // no transaction. No SQL call is possible with the nil transaction below.
 proof:=semanticSceneProducerProof{tenant:uuid.New(),project:uuid.New(),actor:uuid.New(),session:uuid.New()}
 if err:=reader.lockSceneProducerQualification(context.Background(),nil,who,proof.tenant,proof);!errors.Is(err,v2input.ErrInvalid){t.Fatalf("unbound proof: %v",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=reader.lockSceneProducerQualification(ctx,nil,who,proof.tenant,proof);!errors.Is(err,context.Canceled){t.Fatalf("cancelled admission: %v",err)}
}
