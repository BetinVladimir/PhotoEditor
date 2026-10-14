package interchange

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/canonical"
)

func TestImportBackgroundIndexScopesPortableIDToExactStyle(t *testing.T){
 first:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 second:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}
 a:=ImportBackgroundSource{Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)}}
 b:=ImportBackgroundSource{Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("d",64)}}
 index:=&ImportBackgroundIndex{sources:map[v2input.Ref]map[string]ImportBackgroundSource{first:{"same":a},second:{"same":b}}}
 for style,want:=range map[v2input.Ref]ImportBackgroundSource{first:a,second:b}{
  got,err:=index.ResolveImportBackground(context.Background(),style,"same");if err!=nil||got!=want{t.Fatalf("wrong style mapping: %v",err)}
 }
 changed:=first;changed.SHA256=second.SHA256
 if _,err:=index.ResolveImportBackground(context.Background(),changed,"same");err==nil{t.Fatal("accepted changed style hash")}
 if _,err:=index.ResolveImportBackground(context.Background(),first,"missing");err==nil{t.Fatal("accepted missing mapping")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=index.ResolveImportBackground(ctx,first,"same");err==nil{t.Fatal("ignored cancellation")}
}
func TestImportBackgroundIndexRejectsInvalidArchive(t *testing.T){
 if _,err:=NewImportBackgroundIndex(context.Background(),[]byte(`{}`));err==nil{t.Fatal("accepted invalid archive")}
 var index *ImportBackgroundIndex
 if _,err:=index.ResolveImportBackground(context.Background(),v2input.Ref{},"same");err==nil{t.Fatal("accepted nil index")}
}

func TestImportBackgroundIndexBuildsOwnedMappingFromValidatedParsedArchive(t *testing.T){
 document:=importParsedValidatorFixture()
 style:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))}
 original:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("background"))}
 staged:=v2input.Ref{ArtifactID:uuid.New(),SHA256:original.SHA256}
 document.Manifest.Inventory=append(document.Manifest.Inventory,
  importBundleInventory{ID:style.ArtifactID,Path:"dependencies/style.json",SHA:style.SHA256,Contract:"StyleV2"},
  importBundleInventory{ID:original.ArtifactID,Path:"dependencies/background.bin",SHA:original.SHA256,Contract:"StyleBackgroundAsset"})
 document.Files=append(document.Files,
  importParsedFile{Path:"dependencies/style.json",Content:v2input.Ref{ArtifactID:uuid.New(),SHA256:style.SHA256},Contract:"StyleV2"},
  importParsedFile{Path:"dependencies/background.bin",Content:staged,Contract:"StyleBackgroundAsset"})
 document.Manifest.Backgrounds=[]bundleBackground{{Style:style,Portable:"bg",Path:"dependencies/background.bin",SHA:original.SHA256}}
 raw,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
 index,err:=NewImportBackgroundIndex(context.Background(),raw);if err!=nil{t.Fatal(err)}
 for i:=range raw{raw[i]='x'}
 got,err:=index.ResolveImportBackground(context.Background(),style,"bg")
 want:=ImportBackgroundSource{Style:style,Portable:"bg",Original:original,Staged:staged}
 if err!=nil||got!=want{t.Fatal("lost owned validated mapping",err)}
 document.Files[len(document.Files)-1].Content.SHA256=rawSHA([]byte("substituted"))
 changed,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)}
 if _,err:=NewImportBackgroundIndex(context.Background(),changed);err==nil{t.Fatal("accepted substituted staged bytes")}
}
