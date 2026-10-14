package review

import (
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestNegativeReviewCandidatePreservesAllNonRGBMetadata(t *testing.T){
 baseRaw:=imageFixtures(t)[0];base,err:=ParseWorkingImage(baseRaw);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","alpha_added","alpha_removed","alpha_changed","canvas","color","dtype","invalid"}{
  t.Run(mode,func(t *testing.T){
   before:=base;candidate:=base;candidate.Pixels.ID=uuid.New()
   alpha:=ArtifactRef{ID:uuid.New(),SHA256:base.Pixels.SHA256}
   switch mode{
   case "alpha_added":before.Alpha=nil;candidate.Alpha=&alpha
   case "alpha_removed":before.Alpha=&alpha;candidate.Alpha=nil
   case "alpha_changed":before.Alpha=&alpha;changed:=alpha;changed.ID=uuid.New();candidate.Alpha=&changed
   case "canvas":candidate.Canvas.Width++
   case "color":candidate.Color.ICCSHA256="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";if candidate.Color.ICCSHA256==before.Color.ICCSHA256{candidate.Color.ICCSHA256="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
   case "dtype":candidate.DType="uint8"
   }
   raw,err:=canonical.Marshal(candidate);if err!=nil{t.Fatal(err)};beforeRaw,err:=canonical.Marshal(before);if err!=nil{t.Fatal(err)}
   if mode=="invalid"{raw=[]byte(`{}`)}
   err=validateCandidatePair("optical.negative",raw,beforeRaw)
   if mode=="valid"{if err!=nil{t.Fatal("valid negative pair rejected",err)};return}
   if err==nil{t.Fatal("negative metadata mutation accepted",mode)}
  })
 }
}
