package workflow

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

func TestSignedPipelineRefusesUnprovenBundleBeforePublication(t *testing.T){
 p:=DeliveryAssemblyPipeline{}
 err:=p.RegisterAndActivateSignedAssembly(context.Background(),delivery.ExpansionLease{},
  delivery.PreparePlan{},delivery.SignedAssemblyPublication{},uuid.New(),nil,nil,nil,nil)
 if !errors.Is(err,v2input.ErrInvalid){t.Fatal("missing signed authority reached publication",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 err=p.RegisterAndActivateSignedAssembly(ctx,delivery.ExpansionLease{},
  delivery.PreparePlan{},delivery.SignedAssemblyPublication{},uuid.New(),nil,nil,nil,nil)
 if !errors.Is(err,context.Canceled){t.Fatal("cancelled signed pipeline proceeded",err)}
}
