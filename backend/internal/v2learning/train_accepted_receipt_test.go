package v2learning

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type acceptedTrainingRow struct { raw json.RawMessage; err error }
func(r acceptedTrainingRow) Scan(dst ...any)error{if r.err!=nil{return r.err};*dst[0].(*json.RawMessage)=r.raw;return nil}
type acceptedTrainingDB struct {row acceptedTrainingRow;args []any;calls int}
func(d *acceptedTrainingDB)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{d.calls++;d.args=args;return d.row}

func TestAcceptedTrainingReceiptPreservesExactOriginalIdentity(t *testing.T){
 _,_,request,_:=compiledTrainingDraftFixture(t)
 tenantID,profile:=uuid.New(),uuid.New();who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};key:=uuid.NewString()
 run,err:=v2input.RunID(tenantID,who.UserID,key);if err!=nil{t.Fatal(err)}
 want:=v2input.JobTicket{RunID:run,WorkflowID:uuid.NewSHA1(run,[]byte("workflow")),State:"queued"};raw,_:=json.Marshal(want)
 db:=&acceptedTrainingDB{row:acceptedTrainingRow{raw:raw}}
 got,found,err:=readAcceptedTrainingReceipt(context.Background(),db,who,tenantID,profile,key,request)
 if err!=nil||!found||got!=want{t.Fatal(got,found,err)}
 sha,_:=canonical.Hash(request)
 if len(db.args)!=7||db.args[0]!=tenantID||db.args[1]!=who.UserID||db.args[2]!=who.SessionID||db.args[3]!=profile||db.args[4]!=run||db.args[5]!=key||db.args[6]!=sha{t.Fatal("original receipt scope changed",db.args)}
 for _,raw:=range []json.RawMessage{nil,json.RawMessage("null")}{db.row.raw=raw;if _,found,err=readAcceptedTrainingReceipt(context.Background(),db,who,tenantID,profile,key,request);err!=nil||found{t.Fatal(found,err)}}
 for _,bad:=range []v2input.JobTicket{{RunID:uuid.New(),WorkflowID:want.WorkflowID,State:"queued"},{RunID:run,WorkflowID:uuid.New(),State:"queued"},{RunID:run,WorkflowID:want.WorkflowID,State:"completed"}}{db.row.raw,_=json.Marshal(bad);if _,_,err=readAcceptedTrainingReceipt(context.Background(),db,who,tenantID,profile,key,request);err==nil{t.Fatal("substituted receipt accepted",bad)}}
 ctx,cancel:=context.WithCancel(context.Background());cancel();db.calls=0
 if _,_,err=readAcceptedTrainingReceipt(ctx,db,who,tenantID,profile,key,request);!errors.Is(err,context.Canceled)||db.calls!=0{t.Fatal("cancelled recovery performed IO",err,db.calls)}
}
