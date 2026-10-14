package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

func TestFrozenSignedAssemblyRegistersOnlySignedInventory(t *testing.T){
	plan,parent,_,_,_:=deliveryExportPlanFixture(t,nil)
	workflow,_:=v2input.WorkflowID(parent)
	lease:=delivery.ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Tenant:uuid.New(),Package:uuid.New(),
		Run:parent,Workflow:workflow,ParentTask:plan.TaskID(),Until:time.Now().Add(time.Minute),SlotCount:1,Cursor:1}
	makeEntry:=func(contract string,n int)v2input.Entry{return v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),
		SHA256:fmt.Sprintf("%064x",n)},Contract:contract}}
	roots:=[]v2input.Entry{makeEntry("SignedDeliveryAssemblyEnvelope",1),makeEntry("DeliveryAssemblyInput",2),
		makeEntry("SignedDeliveryAssemblyClosure",3),makeEntry("DeliverySlotShard",4),
		makeEntry("ExportPresetConfig",5),makeEntry("EncodedExport",6),
		makeEntry("SignedExport",7),makeEntry("C2PAManifest",8)}
	params:=v2input.DeliveryAssembleParams{SchemaVersion:1,Assembly:roots[0].Ref}
	sha,err:=canonical.Hash(params);if err!=nil{t.Fatal(err)}
	profile:=json.RawMessage(`{"backend":"cpu","renderer_version":"v2.2","resource_estimate":{"ram_bytes":134217728,"vram_bytes":0,"scratch_bytes":8388608,"output_bytes":8388608,"pixel_count":0}}`)
	frozen,err:=FreezeSignedDeliveryAssemblyTask(lease,plan,roots[0].Ref,
		v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},roots,roots,profile)
	if err!=nil{t.Fatal(err)}
	graph,err:=frozen.PublishInputs(context.Background(),deliveryTransportPublisher{})
	if err!=nil{t.Fatal(err)}
	tx:=&deliveryAssemblyTransportTx{}
	if err:=frozen.RegisterInputsInTransaction(context.Background(),tx,lease,plan,graph);err!=nil||
		len(tx.calls)!=5||!strings.Contains(tx.calls[0],"signed_assembly_transport_ready")||!tx.ready{
		t.Fatal("signed transport not registered",err,tx.calls)
	}
	tx=&deliveryAssemblyTransportTx{ready:true}
	if err:=frozen.RegisterInputsInTransaction(context.Background(),tx,lease,plan,graph);err!=nil||len(tx.calls)!=1{
		t.Fatal("ready signed transport republished",err,tx.calls)
	}
	bad:=append([]v2input.Entry(nil),roots...)
	bad[6].Contract="EncodedExport"
	if _,err:=FreezeSignedDeliveryAssemblyTask(lease,plan,roots[0].Ref,
		v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},roots,bad,profile);err==nil{
		t.Fatal("foreign signed contract accepted")
	}
	for _,scenario:=range []string{"fresh","ready","create_error","renew_error","register_error","publish_error"}{
		t.Run(scenario,func(t *testing.T){
			store:=&signedAssemblyRegistrationStub{ready:scenario=="ready",fail:scenario}
			publisher:=&assemblyRegistrationPublisher{fail:scenario=="publish_error"}
			id,err:=frozen.RegisterPreparedSignedAssembly(context.Background(),store,publisher,lease,plan)
			if scenario=="fresh"{
				if err!=nil||id==uuid.Nil||strings.Join(store.steps,",")!="registered,create,renew,renew,register"||publisher.calls==0{
					t.Fatal("signed coordinator order",err,store.steps)
				}
			}else if scenario=="ready"{
				if err!=nil||id==uuid.Nil||len(store.steps)!=1||publisher.calls!=0{t.Fatal("ready replay",err,store.steps)}
			}else if err==nil||id!=uuid.Nil{t.Fatal("failed signed registration returned task",scenario)}
		})
	}
}

type signedAssemblyRegistrationStub struct{ready bool;fail string;steps []string}
func(s *signedAssemblyRegistrationStub)Registered(context.Context,FrozenSignedDeliveryAssemblyTask,delivery.ExpansionLease,delivery.PreparePlan)(bool,error){
	s.steps=append(s.steps,"registered");return s.ready,nil
}
func(s *signedAssemblyRegistrationStub)Create(context.Context,FrozenSignedDeliveryAssemblyTask,delivery.ExpansionLease,delivery.PreparePlan)error{
	s.steps=append(s.steps,"create");if s.fail=="create_error"{return errors.New("create failed")};return nil
}
func(s *signedAssemblyRegistrationStub)Renew(context.Context,delivery.ExpansionLease)error{
	s.steps=append(s.steps,"renew");if s.fail=="renew_error"{return errors.New("lease lost")};return nil
}
func(s *signedAssemblyRegistrationStub)Register(_ context.Context,_ FrozenSignedDeliveryAssemblyTask,
	_ delivery.ExpansionLease,_ delivery.PreparePlan,g v2input.GraphTask)error{
	s.steps=append(s.steps,"register")
	if g.ID==uuid.Nil||s.fail=="register_error"{return errors.New("registration failed")}
	return nil
}
