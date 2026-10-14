package interchange

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestSeriesMaskPolicyRouterUsesOnlyExactOperation(t *testing.T){
 for _,mode:=range []string{"person","prompt","unknown","version","missing"}{t.Run(mode,func(t *testing.T){
  person:=&seriesMaskPolicyStub{};prompt:=&seriesMaskPolicyStub{}
  router:=ExportSeriesMaskPolicyRouter{Person:person,Prompt:prompt}
  operation:="mask.person";version:="1";if mode=="prompt"||mode=="missing"{operation="mask.prompt"};if mode=="unknown"{operation="mask.face_skin"};if mode=="version"{version="2"};if mode=="missing"{router.Prompt=nil}
  _,err:=router.ResolveExportSeriesMaskPolicy(context.Background(),ExportSeriesMaskOrigin{Operation:operation,Version:version},v2input.Ref{})
  if mode=="person"{if err!=nil||person.calls!=1||prompt.calls!=0{t.Fatal(err)}}else if mode=="prompt"{if err!=nil||person.calls!=0||prompt.calls!=1{t.Fatal(err)}}else if err==nil||person.calls!=0||prompt.calls!=0{t.Fatal("unsupported operation used fallback",mode,err)}
 })}
}
func TestPromptPolicyInstallationRequiresSQLRequestScope(t *testing.T){
 if _,err:=(SQLExportPromptMaskPolicyInstallation{}).BuildExportPromptMaskPolicy(context.Background(),CreateIntent{},FrozenExportDocuments{},nil,nil);err!=v2input.ErrInvalid{t.Fatal(err)}
}
