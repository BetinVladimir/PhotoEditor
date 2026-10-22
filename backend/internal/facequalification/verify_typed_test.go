package facequalification
import("context";"errors";"testing";"github.com/google/uuid";"photoeditor/backend/internal/auth";"photoeditor/backend/internal/v2input";"photoeditor/backend/internal/v2qualification")
func TestTypedFaceReaderRetainsCancellationAndNoAuthorityFailClosed(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel();reader:=Reader{}
 verified,err:=reader.VerifyTyped(ctx,auth.Identity{},uuid.Nil,v2input.Ref{},v2qualification.Expected{});if !errors.Is(err,context.Canceled)||verified.Resources().Valid(){t.Fatal("cancelled reader retained evidence",err)}
 verified,err=reader.VerifyTyped(context.Background(),auth.Identity{},uuid.Nil,v2input.Ref{},v2qualification.Expected{});if err==nil||verified.Resources().Valid(){t.Fatal("missing physical/SQL authority admitted")}
}
