package interchange
import (
 "context"
 "errors"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestSQLImportEntryAuthorityFactoryRefusesMissingInfrastructure(t *testing.T){
 f:=SQLImportEntryRecipeAuthorityFactory{}
 codec,qualifier,err:=f.BuildImportEntryRecipeAuthority(context.Background(),ImportPreparedScope{},nil,nil,nil,PreparedImportArtifactPublisher{},nil)
 if !errors.Is(err,v2input.ErrInvalid)||codec!=nil||qualifier!=nil{t.Fatalf("partial factory accepted: %v",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 codec,qualifier,err=f.BuildImportEntryRecipeAuthority(ctx,ImportPreparedScope{},nil,nil,nil,PreparedImportArtifactPublisher{},nil)
 if !errors.Is(err,context.Canceled)||codec!=nil||qualifier!=nil{t.Fatalf("cancelled factory accepted: %v",err)}
}
