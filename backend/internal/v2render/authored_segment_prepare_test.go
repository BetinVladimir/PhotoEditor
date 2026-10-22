package v2render

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

func TestAuthoredSegmentPreparationRefusesMissingActualPorts(t *testing.T){
 _,err:=(SQLAuthoredSegmentPreparation{}).Prepare(context.Background(),auth.Identity{},uuid.New(),uuid.New(),1,nil,nil)
 if !errors.Is(err,v2input.ErrInvalid){t.Fatalf("missing actual ports not refused: %v",err)}
}

func TestAuthoredSegmentPreparationPreservesCancellationBeforePublication(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err:=(SQLAuthoredSegmentPreparation{}).Prepare(ctx,auth.Identity{},uuid.New(),uuid.New(),1,nil,nil)
 if !errors.Is(err,context.Canceled){t.Fatalf("cancellation identity lost: %v",err)}
}
