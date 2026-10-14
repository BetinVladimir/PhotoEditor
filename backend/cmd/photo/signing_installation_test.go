package main

import (
 "context"
 "errors"
 "path/filepath"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/provenance"
)

func TestAPISigningDeclarationRefusesPartialOrUnprovisionedStartup(t *testing.T){
 path:=filepath.Join(t.TempDir(),"signing.json")
 runtime:=apiProcessDependencies{SigningTenants:map[uuid.UUID]provenance.SigningAPITenantRuntime{uuid.New():{}}}
 for _,test:=range []struct{name,path,pin string;runtime apiProcessDependencies;valid bool}{
  {"disabled","","",apiProcessDependencies{},true},
  {"declaration",path,strings.Repeat("a",64),runtime,true},
  {"file_only",path,"",runtime,false},
  {"pin_only","",strings.Repeat("a",64),runtime,false},
  {"relative","signing.json",strings.Repeat("a",64),runtime,false},
  {"uppercase",path,strings.Repeat("A",64),runtime,false},
  {"no_native_bootstrap",path,strings.Repeat("a",64),apiProcessDependencies{},false},
  {"runtime_without_declaration","","",runtime,false},
 }{t.Run(test.name,func(t *testing.T){if err:=validateAPISigningDeclaration(test.path,test.pin,test.runtime);(err==nil)!=test.valid{t.Fatalf("valid=%v err=%v",test.valid,err)}})}
}
func TestAPISigningDisabledAndCancelledDoNotOpenResources(t *testing.T){
 installed,err:=installAPISigning(context.Background(),"","","invalid DSN",nil,nil,"","",apiProcessDependencies{})
 if err!=nil||installed!=nil{t.Fatal("disabled signing opened resources",err)}
 installed.Close()
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=installAPISigning(ctx,"","","invalid DSN",nil,nil,"","",apiProcessDependencies{});err!=context.Canceled{t.Fatal("startup cancellation lost",err)}
}

func TestSchedulerSigningFactoryRejectsUnpinnedStartup(t *testing.T) {
	factory := schedulerSigningFactory("", "", apiProcessDependencies{})
	_, closeRuntime, err := factory(context.Background(), "invalid DSN", nil,
		nil, "photos", strings.Repeat("a", 64))
	if err == nil || closeRuntime != nil {
		t.Fatal("scheduler signing started without pinned deployment", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = factory(ctx, "invalid DSN", nil, nil,
		"photos", strings.Repeat("a", 64))
	if !errors.Is(err, context.Canceled) {
		t.Fatal("scheduler startup cancellation lost", err)
	}
}
