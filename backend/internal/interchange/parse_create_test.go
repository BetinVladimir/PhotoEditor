package interchange
import("context";"errors";"testing";"photoeditor/backend/internal/idempotency")
func TestCoordinateParseCreateRacesAndFailures(t *testing.T){
 prepareFailure:=errors.New("object store unavailable");denied:=errors.New("access revoked")
 cases:=[]struct{name string;initialDone,concurrentDone,pending bool;prepareErr,recoveryErr,wantErr error;wantPrepare,wantActivate int}{
 {name:"completed replay",initialDone:true},
 {name:"fresh activation",pending:true,wantPrepare:1,wantActivate:1},
 {name:"concurrent completion during failure",concurrentDone:true,prepareErr:prepareFailure,wantPrepare:1},
 {name:"incomplete preserves preparation error",prepareErr:prepareFailure,wantErr:prepareFailure,wantPrepare:1},
 {name:"access revocation wins",prepareErr:prepareFailure,recoveryErr:denied,wantErr:denied,wantPrepare:1},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  reads,prepares,activations:=0,0,0
  expected:=idempotency.Response{Status:202,Body:[]byte(`{"state":"queued"}`),Replayed:true}
  got,err:=coordinateParseCreate(context.Background(),parseCreatePhases{
   recover:func(context.Context)(ParseRecovery,error){reads++;if reads==1{return ParseRecovery{Completed:tc.initialDone,Response:expected},nil};if tc.recoveryErr!=nil{return ParseRecovery{},tc.recoveryErr};return ParseRecovery{Completed:tc.concurrentDone,HasPending:tc.pending,Response:expected},nil},
   prepare:func(context.Context)error{prepares++;return tc.prepareErr},
   activate:func(context.Context,ParsePlan)(idempotency.Response,error){activations++;return expected,nil},
  })
  if !errors.Is(err,tc.wantErr)||prepares!=tc.wantPrepare||activations!=tc.wantActivate{t.Fatal("wrong transitions",err,prepares,activations)}
  if tc.wantErr==nil&&got.Status!=202{t.Fatal("missing ticket")}
 })}
}
func TestCoordinateParseCreateCancellationStopsRecoveryAndActivation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());reads:=0
 _,err:=coordinateParseCreate(ctx,parseCreatePhases{
 recover:func(context.Context)(ParseRecovery,error){reads++;return ParseRecovery{},nil},
 prepare:func(context.Context)error{cancel();return errors.New("interrupted")},
 activate:func(context.Context,ParsePlan)(idempotency.Response,error){t.Fatal("activated after cancellation");return idempotency.Response{},nil},
 })
 if err!=context.Canceled||reads!=1{t.Fatal("cancellation did not stop coordinator",err,reads)}
}
func TestCoordinateParseCreateRecoversUncertainActivation(t *testing.T){
 lostAck:=errors.New("commit acknowledgement lost")
 revoked:=errors.New("access revoked")
 cases:=[]struct{name string;completed bool;recoveryErr,wantErr error;wantTicket bool}{
  {name:"committed ticket replay",completed:true,wantTicket:true},
  {name:"uncommitted activation preserves error",wantErr:lostAck},
  {name:"revoked recovery takes precedence",recoveryErr:revoked,wantErr:revoked},
 }
 for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
  reads,activations:=0,0
  ticket:=idempotency.Response{Status:202,Body:[]byte(`{"state":"queued"}`),Replayed:true}
  got,err:=coordinateParseCreate(context.Background(),parseCreatePhases{
   recover:func(context.Context)(ParseRecovery,error){
    reads++
    if reads==3&&tc.recoveryErr!=nil{return ParseRecovery{},tc.recoveryErr}
    return ParseRecovery{HasPending:true,Completed:reads==3&&tc.completed,Response:ticket},nil
   },
   prepare:func(context.Context)error{return nil},
   activate:func(context.Context,ParsePlan)(idempotency.Response,error){activations++;return idempotency.Response{},lostAck},
  })
  if !errors.Is(err,tc.wantErr)||reads!=3||activations!=1{t.Fatal("wrong activation recovery",err,reads,activations)}
  if tc.wantTicket&&(got.Status!=202||!got.Replayed){t.Fatal("missing recovered ticket",got)}
 })}
}
