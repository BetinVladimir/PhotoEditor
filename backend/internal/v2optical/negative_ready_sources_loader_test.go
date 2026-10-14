package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
)

func TestRequiredUnchangedBindingsIgnoreOldAttachmentsAndOwnResults(t *testing.T){
 params:=perspectiveIntent();raw,_,err:=params.Compile();if err!=nil{t.Fatal(err)}
 step:=PreparedMappedReplacementStep{}
 step.parents.step.intent=VerifiedOpticalFrozenIntent{raw:string(raw),locator:negativeFrozenIntentLocator{Operation:"optical.perspective"}}
 // An old attachment must not make the new full proof set appear complete.
 step.unchanged=[]VerifiedNegativeUnchangedArrays{{}}
 required,err:=step.requiredUnchangedBindings(context.Background())
 if err!=nil||len(required)!=1||required[0]!=params.Base{t.Fatal("unchanged original binding omitted",err)}
 required[0].Source.ArtifactID=uuid.New()
 again,err:=step.requiredUnchangedBindings(context.Background())
 if err!=nil||len(again)!=1||again[0]!=params.Base{t.Fatal("mutable source requirements",err)}
 step.mapping.sources=[]matchedNegativeAffectedSource{{Before:params.Base}}
 required,err=step.requiredUnchangedBindings(context.Background())
 if err!=nil||len(required)!=0{t.Fatal("changed source requested as unchanged",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=step.requiredUnchangedBindings(ctx);err!=context.Canceled{t.Fatal("cancellation",err)}
}

func TestReadySourceLoaderRefusesMissingReadersBeforeSQL(t *testing.T){
 db:=&negativeDraftStoreDB{};var prepared PreparedNegativeReplacementInputs
 mappings,unchanged,err:=prepared.LoadMappedReadySources(context.Background(),db,nil,nil,nil)
 if err==nil||mappings!=nil||unchanged!=nil||db.begins!=0{t.Fatal("incomplete loader reached SQL",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,_,err=prepared.LoadMappedReadySources(ctx,db,nil,nil,nil);err!=context.Canceled{t.Fatal("cancellation",err)}
}
