package delivery

import "testing"

func TestDelegatedRenderSigningClaimsRequireOneScope(t *testing.T) {
	claims, err := NewDelegatedRenderSigningClaims(SQLDelegatedTaskCatalog{}, nil, nil)
	if err == nil || claims != nil { t.Fatal("unconfigured render claims installed") }
}
