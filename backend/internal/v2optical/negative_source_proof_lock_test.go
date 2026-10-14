package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

func TestNegativeSourceProofRejectsAbsentReceiptAndCancellation(t *testing.T) {
 if err:=lockNegativeSourceProof(context.Background(),nil,"bucket",auth.Identity{},uuid.Nil,uuid.Nil,uuid.Nil,NegativeParams{},nil,nil);err!=v2input.ErrInvalid { t.Fatalf("absent receipt: %v",err) }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=lockNegativeSourceProof(ctx,nil,"bucket",auth.Identity{},uuid.Nil,uuid.Nil,uuid.Nil,NegativeParams{},nil,nil);err!=context.Canceled { t.Fatalf("cancelled: %v",err) }
}

func TestNegativeSourceObjectDecoderBoundsBeforeMultiplication(t *testing.T) {
 spec,_,_:=perspectiveArrayFixture(t,3,[]float32{0,1,2})
 spec.Width=9223372036854775807
 if _,err:=decodeNegativeSourceArrayObject([]byte(`{}`),"bucket",spec);err!=v2input.ErrInvalid { t.Fatalf("overflow dimensions: %v",err) }
}
