package interchange
import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/idempotency"
)
func TestCoordinateExportCreateConcurrencyAndFailures(t *testing.T){
 preparationFailure:=errors.New("preparation failed");activationFailure:=errors.New("uncertain commit");denied:=errors.New("access revoked")
 for _,tc:=range []struct{name string;foundAt,recoveryFailureAt int;prepareErr,activateErr,wantErr error;wantReads,wantPrepare,wantActivate int}{
  {name:"initial ticket",foundAt:1,wantReads:1},
  {name:"fresh",wantReads:2,wantPrepare:1,wantActivate:1},
  {name:"concurrent preparation",foundAt:2,prepareErr:preparationFailure,wantReads:2,wantPrepare:1},
  {name:"preparation failure",prepareErr:preparationFailure,wantErr:preparationFailure,wantReads:2,wantPrepare:1},
  {name:"denied initially",recoveryFailureAt:1,wantErr:denied,wantReads:1},
  {name:"denied after preparation",recoveryFailureAt:2,prepareErr:preparationFailure,wantErr:denied,wantReads:2,wantPrepare:1},
  {name:"uncertain activation recovered",foundAt:3,activateErr:activationFailure,wantReads:3,wantPrepare:1,wantActivate:1},
  {name:"activation failure",activateErr:activationFailure,wantErr:activationFailure,wantReads:3,wantPrepare:1,wantActivate:1},
  {name:"denied after activation failure",recoveryFailureAt:3,activateErr:activationFailure,wantErr:denied,wantReads:3,wantPrepare:1,wantActivate:1},
 }{t.Run(tc.name,func(t *testing.T){
  reads,prepares,activations:=0,0,0;ticket:=idempotency.Response{Status:202,Body:[]byte(`{"state":"queued"}`),Replayed:true}
  response,err:=coordinateExportCreate(context.Background(),exportCreatePhases{
   recover:func(context.Context)(idempotency.Response,bool,error){reads++;if reads==tc.recoveryFailureAt{return idempotency.Response{},false,denied};return ticket,reads==tc.foundAt,nil},
   prepare:func(context.Context)(PreparedExportActivation,error){prepares++;return PreparedExportActivation{},tc.prepareErr},
   activate:func(context.Context,PreparedExportActivation)(idempotency.Response,error){activations++;return ticket,tc.activateErr},
  })
  if !errors.Is(err,tc.wantErr)||reads!=tc.wantReads||prepares!=tc.wantPrepare||activations!=tc.wantActivate{t.Fatal("wrong phase sequence",err,reads,prepares,activations)}
  if tc.wantErr==nil&&response.Status!=202{t.Fatal("missing ticket")};if tc.wantErr!=nil&&response.Status!=0{t.Fatal("failure leaked ticket")}
 })}
}
func TestCoordinateExportCreateCancellationAtEachPhase(t *testing.T){
 for _,phase:=range []string{"initial","prepare","activate"}{t.Run(phase,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());reads,prepares,activations:=0,0,0;if phase=="initial"{cancel()}
  response,err:=coordinateExportCreate(ctx,exportCreatePhases{
   recover:func(context.Context)(idempotency.Response,bool,error){reads++;return idempotency.Response{},false,nil},
   prepare:func(context.Context)(PreparedExportActivation,error){prepares++;if phase=="prepare"{cancel()};return PreparedExportActivation{},nil},
   activate:func(context.Context,PreparedExportActivation)(idempotency.Response,error){activations++;cancel();return idempotency.Response{Status:202},nil},
  });defer cancel()
  if err!=context.Canceled||response.Status!=0{t.Fatal("cancelled create returned ticket",err)}
  if phase=="initial"&&(reads!=0||prepares!=0||activations!=0){t.Fatal("initial cancel performed work")}
  if phase=="prepare"&&(reads!=1||activations!=0){t.Fatal("prepare cancel continued")}
  if phase=="activate"&&reads!=2{t.Fatal("cancelled activation attempted recovery")}
 })}
}
