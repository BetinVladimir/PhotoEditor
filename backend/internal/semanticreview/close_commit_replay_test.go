package semanticreview

import (
    "context"
    "encoding/json"
    "errors"
    "testing"

    "github.com/google/uuid"
    "photoeditor/backend/internal/canonical"
    "photoeditor/backend/internal/idempotency"
)

func TestConcurrentCloseReplayRepeatsAuthorityAndOwnsResult(t *testing.T) {
    run,workflow,batch:=uuid.New(),uuid.New(),uuid.New()
    body,err:=canonical.Marshal(CloseResult{RunID:run,WorkflowID:workflow,Revision:"2",State:"completed",Outputs:[]json.RawMessage{}})
    if err!=nil { t.Fatal(err) }
    response:=idempotency.Response{Status:200,Body:append([]byte(nil),body...),ResourceID:&batch,Replayed:true}
    calls:=0
    result,err:=verifyCloseCommitReplay(context.Background(),response,run,workflow,batch,func(context.Context)(ReplayResult,error){calls++;return ReplayResult{Outcome:"replayed",Body:body},nil})
    if err!=nil || calls!=1 || !result.Replayed || string(result.Body)!=string(body) { t.Fatal(result,err,calls) }
    body[0]='x';response.Body[0]='y'
    if result.Body[0]!='{' { t.Fatal("replay body ownership lost") }
}

func TestConcurrentCloseReplayRefusesAuthorityOrReceiptChanges(t *testing.T) {
    for _,mode:=range []string{"denied","pending","conflict","absent","wrong_parent","wrong_workflow","changed_body","malformed","wrong_batch","missing_resource","status","not_replayed","cancel_before","cancel_during","missing_loader"} {
        t.Run(mode,func(t *testing.T){
            run,workflow,batch:=uuid.New(),uuid.New(),uuid.New()
            dto:=CloseResult{RunID:run,WorkflowID:workflow,Revision:"2",State:"completed",Outputs:[]json.RawMessage{}}
            body,err:=canonical.Marshal(dto);if err!=nil { t.Fatal(err) }
            response:=idempotency.Response{Status:200,Body:body,ResourceID:&batch,Replayed:true}
            ctx,cancel:=context.WithCancel(context.Background());defer cancel()
            calls:=0
            denied:=errors.New("editor authorization revoked")
            var load func(context.Context)(ReplayResult,error)=func(context.Context)(ReplayResult,error){
                calls++
                if mode=="denied" { return ReplayResult{},denied }
                if mode=="cancel_during" { cancel() }
                outcome:="replayed"
                switch mode {case "pending","conflict","absent":outcome=mode;case "wrong_parent":dto.RunID=uuid.New();case "wrong_workflow":dto.WorkflowID=uuid.New();case "changed_body":dto.Revision="3"}
                raw,err:=canonical.Marshal(dto);if err!=nil { t.Fatal(err) }
                if mode=="malformed" { raw=[]byte(`{}`) }
                return ReplayResult{Outcome:outcome,Body:raw},nil
            }
            switch mode {case "wrong_batch":other:=uuid.New();response.ResourceID=&other;case "missing_resource":response.ResourceID=nil;case "status":response.Status=202;case "not_replayed":response.Replayed=false;case "cancel_before":cancel();case "missing_loader":load=nil}
            result,err:=verifyCloseCommitReplay(ctx,response,run,workflow,batch,load)
            if err==nil || len(result.Body)!=0 || result.ResourceID!=nil { t.Fatal("unsafe receipt returned",result,err) }
            expectedCalls:=1
            switch mode {case "wrong_batch","missing_resource","status","not_replayed","cancel_before","missing_loader":expectedCalls=0}
            if calls!=expectedCalls { t.Fatal("authority callback count",calls,expectedCalls) }
            if mode=="denied" && !errors.Is(err,denied) { t.Fatal(err) }
            if (mode=="cancel_before" || mode=="cancel_during") && !errors.Is(err,context.Canceled) { t.Fatal(err) }
        })
    }
}
