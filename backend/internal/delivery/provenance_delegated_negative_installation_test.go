package delivery

import "testing"

func TestDelegatedNegativeSigningClaimsRequiresOneValidScope(t *testing.T) {
 if claims,err:=NewDelegatedNegativeSigningClaims(SQLDelegatedTaskCatalog{},nil,nil,nil);
  err==nil||claims!=nil{t.Fatal("unconfigured negative signing claims installed")}
}
