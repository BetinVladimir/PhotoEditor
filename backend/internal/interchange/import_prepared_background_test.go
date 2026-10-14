package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestPreparedBackgroundCertificateRequiresExactOccurrenceAndMedia(t *testing.T){
 for _,mode:=range []string{"valid","media","target","sha","missing","unknown"}{t.Run(mode,func(t *testing.T){
  scope:=preparedPublisherScope()
  source:=ImportBackgroundSource{Style:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))},Portable:"background",Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))},Staged:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}}
  entry:=v2input.Entry{Contract:"StyleBackgroundAsset",Ref:v2input.Ref{ArtifactID:importBackgroundID(scope.Workflow,source,scope.Binding),SHA256:source.Original.SHA256}}
  receipt:=map[string]any{"background_occurrence":source,"media_type":"image/png"}
  switch mode{case "media":receipt["media_type"]="application/octet-stream";case "target":scope.Binding.AssetVersionID=uuid.New();case "sha":entry.Ref.SHA256=rawSHA([]byte("other"));case "missing":delete(receipt,"background_occurrence");case "unknown":receipt["trusted"]=true}
  raw,err:=canonical.Marshal(receipt);if err!=nil{t.Fatal(err)}
  got,media,err:=decodePreparedImportBackgroundCertificate(scope,entry,raw)
  if mode=="valid"{if err!=nil||got!=source||media!="image/png"{t.Fatal("certificate",err)}}else if err==nil{t.Fatal("invalid certificate accepted")}
 })}
}
func TestPreparedBackgroundResolverCancelsBeforeIO(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(ImportPreparedBackgroundNodeResolver{}).ResolveImportNode(ctx,v2input.Entry{});!errors.Is(err,context.Canceled){t.Fatal(err)}
 if _,err:=(SQLPreparedImportBackgroundCatalog{}).LookupImportDocument(ctx,v2input.Entry{});!errors.Is(err,context.Canceled){t.Fatal(err)}
}
