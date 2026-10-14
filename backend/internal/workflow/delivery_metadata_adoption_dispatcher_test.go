package workflow

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/google/uuid"
)
type metadataAdoptionTestQueue struct{candidate *MetadataAdoptionCandidate;receipts []bool;reads int;after []uuid.UUID;state string;settled []string}
func(q *metadataAdoptionTestQueue)Next(_ context.Context,after uuid.UUID)(*MetadataAdoptionCandidate,error){q.after=append(q.after,after);return q.candidate,nil}
func(q *metadataAdoptionTestQueue)Committed(_ context.Context,_ MetadataAdoptionCandidate)(bool,error){value:=q.receipts[q.reads];q.reads++;return value,nil}
func(q *metadataAdoptionTestQueue)SettleInvalid(_ context.Context,_ MetadataAdoptionCandidate,code string)(string,error){q.settled=append(q.settled,code);return q.state,nil}
type metadataAdoptionTestRunner struct{calls int;err error}
func(r *metadataAdoptionTestRunner)Run(context.Context,MetadataAdoptionCandidate)error{r.calls++;return r.err}
func TestMetadataAdoptionDispatcherRequiresDurableReceipt(t *testing.T){
 failure:=errors.New("process outcome unknown")
 for _,mode:=range []string{"success","recovered","already","false_success","failure","idle"}{
  t.Run(mode,func(t *testing.T){
   queue:=&metadataAdoptionTestQueue{candidate:&MetadataAdoptionCandidate{Tenant:uuid.New(),Task:uuid.New(),Result:uuid.New(),Attempt:1,Fence:2},receipts:[]bool{false,true}}
   runner:=&metadataAdoptionTestRunner{}
   switch mode{case "recovered":runner.err=failure;case "already":queue.receipts=[]bool{true};case "false_success":queue.receipts=[]bool{false,false};case "failure":runner.err=failure;queue.receipts=[]bool{false,false};case "idle":queue.candidate=nil}
   dispatcher:=&MetadataAdoptionDispatcher{Candidates:queue,Runner:runner,Timeout:time.Minute}
   done,err:=dispatcher.ProcessOne(context.Background())
   if mode=="success"||mode=="recovered"||mode=="already"{if !done||err!=nil{t.Fatal(mode,err)}}else if mode=="idle"{if done||err!=nil{t.Fatal(mode,err)}}else if done||err==nil{t.Fatal("unconfirmed adoption")}
   if mode=="already"||mode=="idle"{if runner.calls!=0{t.Fatal("unnecessary runner")}}
   if mode=="failure"&&!errors.Is(err,failure){t.Fatal("lost process failure")}
   queue.candidate=nil
   if _,err:=dispatcher.ProcessOne(context.Background());err!=nil||dispatcher.after!=uuid.Nil{t.Fatal("cursor did not wrap",err)}
  })
 }
}

func TestMetadataAdoptionDispatcherSettlesOnlyPhysicalRefusals(t *testing.T){
 for _,mode:=range []string{"deferred","rejected","stale","authority","io","committed"}{t.Run(mode,func(t *testing.T){
  queue:=&metadataAdoptionTestQueue{candidate:&MetadataAdoptionCandidate{Tenant:uuid.New(),Task:uuid.New(),Result:uuid.New(),Attempt:1,Fence:2},receipts:[]bool{false,false},state:mode}
  runner:=&metadataAdoptionTestRunner{err:MetadataJobRefusal{Code:"DELIVERY_METADATA_RASTER_BYTES"}}
  if mode=="authority"{runner.err=MetadataJobRefusal{Code:"DELIVERY_METADATA_AUTHORITY_MISSING"}}
  if mode=="io"{runner.err=errors.New("object store unavailable")}
  if mode=="committed"{queue.receipts[1]=true}
  dispatcher:=&MetadataAdoptionDispatcher{Candidates:queue,Runner:runner,Timeout:time.Minute}
  done,err:=dispatcher.ProcessOne(context.Background())
  if mode=="committed"{if !done||err!=nil||len(queue.settled)!=0{t.Fatal("committed receipt overridden",err)};return}
  if mode=="deferred"||mode=="rejected"{if done||err!=nil||len(queue.settled)!=1||queue.settled[0]!="DELIVERY_METADATA_RASTER_BYTES"{t.Fatal("settlement lost",mode,err)};return}
  if done||err==nil{t.Fatal("unconfirmed or nonphysical refusal accepted",mode)}
  if mode=="stale"&&len(queue.settled)!=1{t.Fatal("stale settlement was not attempted")}
  if (mode=="authority"||mode=="io")&&len(queue.settled)!=0{t.Fatal("nonphysical failure touched SQL")}
 })}
}
