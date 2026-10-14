package provenance

import (
 "testing"
 "github.com/google/uuid"
)

func TestInstalledSigningTenantBindsRegistryAndMeasuredResourcesTogether(t *testing.T){
 config,_,_:=signingRegistryFixture(t)
 registry,err:=NewSigningPolicyRegistry(config,&signingAuthorityStub{});if err!=nil{t.Fatal(err)}
 measured:=&MeasuredSigningResourceQualifier{registry:registry}
 service:=&SQLSignExportService{Preparation:SigningActivationPreparation{Registry:registry,Resources:measured}}
 tenant:=uuid.New()
 if err=bindInstalledSigningServiceTenant(service,tenant);err!=nil{t.Fatal(err)}
 bound:=service.Preparation.Registry
 if bound.tenant!=tenant||bound.matchesTenant(uuid.New())||!bound.matchesTenant(tenant){t.Fatal("registry tenant not bound")}
 resource,ok:=service.Preparation.Resources.(*MeasuredSigningResourceQualifier)
 if !ok||resource==measured||resource.registry!=bound||measured.registry!=registry||registry.tenant!=uuid.Nil{t.Fatal("resource projection ownership")}
 if err=bindInstalledSigningServiceTenant(service,uuid.New());err==nil{t.Fatal("foreign rebind accepted")}
 if service.Preparation.Registry!=bound{t.Fatal("failed rebind mutated installation")}
}
func TestInstalledSigningTenantRefusesMixedRegistryBeforeMutation(t *testing.T){
 registry:=&SigningPolicyRegistry{}
 service:=&SQLSignExportService{Preparation:SigningActivationPreparation{Registry:registry,Resources:&MeasuredSigningResourceQualifier{registry:&SigningPolicyRegistry{}}}}
 if err:=bindInstalledSigningServiceTenant(service,uuid.New());err==nil{t.Fatal("mixed resource registry accepted")}
 if service.Preparation.Registry!=registry{t.Fatal("refusal mutated registry")}
}
