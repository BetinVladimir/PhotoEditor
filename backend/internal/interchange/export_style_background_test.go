package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type exportBackgroundAuthorityStub struct{calls int;err error}
func(s *exportBackgroundAuthorityStub)QualifyExportStyleBackground(context.Context,v2draft.FrozenBackgroundAsset)error{s.calls++;return s.err}
func TestExportStyleBackgroundExactFrozenPinAndMandatoryAuthority(t *testing.T){
 for _,mode:=range []string{"valid","missing_pin","portable","style","icc","missing_authority","revoked","keep"}{t.Run(mode,func(t *testing.T){
  style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))};icc:=rawSHA([]byte("icc"))
  pin:=v2draft.FrozenBackgroundAsset{PortableAssetID:"studio",Style:style,WorkICCSHA:icc}
  recipe:=v2draft.FrozenRenderRecipe{BackgroundAsset:&pin};recipe.Scalars.ICCLockSHA=icc
  raw:=[]byte(`{"extensions":{"background":{"mode":"asset","asset_id":"studio"}}}`)
  stub:=&exportBackgroundAuthorityStub{};var authority ExportStyleBackgroundAuthority=stub;cause:=errors.New("background revoked")
  switch mode{case "missing_pin":recipe.BackgroundAsset=nil;case "portable":pin.PortableAssetID="foreign";case "style":pin.Style.ArtifactID=uuid.New();case "icc":pin.WorkICCSHA=rawSHA([]byte("foreign"));case "missing_authority":authority=nil;case "revoked":stub.err=cause;case "keep":raw=[]byte(`{"extensions":{"background":{"mode":"keep","asset_id":null}}}`)}
  err:=qualifyExportStyleBackground(context.Background(),raw,style,recipe,authority)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected background admission: %v",err)}
  if mode=="revoked"&&!errors.Is(err,cause){t.Fatal("revocation cause lost")}
  if mode!="valid"&&mode!="revoked"&&stub.calls!=0{t.Fatal("unbound pin reached authority")}
 })}
 if err:=qualifyExportStyleBackground(context.Background(),[]byte(`{"extensions":{}}`),v2input.Ref{},v2draft.FrozenRenderRecipe{},nil);err!=nil{t.Fatal("no background refused",err)}
}
