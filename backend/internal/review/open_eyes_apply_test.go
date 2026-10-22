package review

import (
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestOpenEyesApplyBindsSelectedBytesAndPreservesGeometry(t *testing.T){
 baseRaw:=imageFixtures(t)[0]
 base,err:=ParseWorkingImage(baseRaw);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"candidate","base","skip","hash","geometry","same_pixels","foreign_base","invalid_image"}{
  t.Run(mode,func(t *testing.T){
   image:=base;image.Pixels.ID=uuid.New();image.Pixels.SHA256=strings.Repeat("c",64)
   route:="candidate"
   if mode=="base"{route="base";image=base}
   if mode=="geometry"{image.Canvas.Width++}
   if mode=="same_pixels"{image=base}
   raw,e:=canonical.Marshal(image);if e!=nil{t.Fatal(e)}
   if mode=="base"{raw=baseRaw}
   if mode=="invalid_image"{raw=[]byte(`{}`)}
   sum:=sha256.Sum256(raw)
   selected:=RestorationResultSelection{SchemaVersion:1,ReviewID:uuid.New(),DecisionHash:strings.Repeat("a",64),Route:route,
    Image:&ArtifactRef{ID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
   if mode=="skip"{selected.Route="skip";selected.Image=nil}
   if mode=="hash"{selected.Image.SHA256=strings.Repeat("b",64)}
   source:=baseRaw;if mode=="foreign_base"{source=[]byte(`{}`)}
   e=validateOpenEyesApplyImages(selected,source,raw)
   if mode=="candidate"||mode=="base"{if e!=nil{t.Fatal(e)}}else if e==nil{t.Fatal("invalid apply accepted")}
  })
 }
}
