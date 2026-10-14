package review

import (
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestNegativeReviewBaseRequiresPhysicalParamsSourceAndCanvas(t *testing.T){
 imageRaw:=imageFixtures(t)[0];image,err:=ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 sha,err:=canonical.Hash(image);if err!=nil{t.Fatal(err)}
 base:=ArtifactRef{ID:uuid.New(),SHA256:sha};canvasHash,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","source_id","source_sha","canvas","schema","noncanonical","invalid_image"}{
  t.Run(mode,func(t *testing.T){
   source:=base;hash:=canvasHash;schema:=1;body:=imageRaw
   switch mode{case "source_id":source.ID=uuid.New();case "source_sha":source.SHA256="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";if source.SHA256==base.SHA256{source.SHA256="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"};case "canvas":hash=base.SHA256;case "schema":schema=2;case "invalid_image":body=[]byte(`{}`)}
   params,err:=canonical.Marshal(map[string]any{"schema_version":schema,"base":map[string]any{"source":source,"canvas_hash":hash}});if err!=nil{t.Fatal(err)}
   if mode=="noncanonical"{params=append(params,'\n')}
   err=verifyNegativeReviewBase(params,body,base)
   if mode=="valid"{if err!=nil{t.Fatal(err)};return};if err==nil{t.Fatal("foreign negative base accepted",mode)}
  })
 }
}

func TestNegativeParamsObjectIsPartOfReviewContext(t *testing.T){
 original:=decisionContext{Kind:"optical.negative"};changed:=original
 changed.NegativeParams.ID=uuid.New();if sameContext(original,changed){t.Fatal("Params identity omitted")}
 changed=original;changed.NegativeParamsObject.VersionID="v2";if sameContext(original,changed){t.Fatal("Params object version omitted")}
}
