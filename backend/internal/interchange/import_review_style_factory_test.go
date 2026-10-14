package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
)
func TestReviewStyleFactoryOwnsInstalledPolicyAndRefusesMissingScope(t *testing.T){
 scratch,err:=NewExportArchiveScratch(t.TempDir(),1000);if err!=nil{t.Fatal(err)}
 models:=map[string]string{"model":"weight"};runtime:=rawSHA([]byte("runtime"));icc:=rawSHA([]byte("icc"))
 factory,err:=NewNativeImportReviewStyleFactory(scratch,&exportStyleProbeStub{runtime:runtime},models,runtime,icc);if err!=nil{t.Fatal(err)}
 models["model"]="changed";if factory.models["model"]!="weight"{t.Fatal("shared model policy")}
 if _,err:=factory.BuildReviewStyleQualifier(context.Background(),SQLImportApprovalCatalog{},uuid.New(),&importReaderStub{});err==nil{t.Fatal("accepted absent target scope")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=factory.BuildReviewStyleQualifier(ctx,SQLImportApprovalCatalog{},uuid.New(),&importReaderStub{});!errors.Is(err,context.Canceled){t.Fatal("lost cancellation",err)}
 if _,err:=NewNativeImportReviewStyleFactory(scratch,&exportStyleProbeStub{},models,"invalid",icc);err==nil{t.Fatal("accepted invalid runtime pin")}
}
