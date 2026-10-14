package interchange
import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
)
func TestImportEntrySourcesQualifiesCorrectionSiblings(t *testing.T){
 for _,change:=range []string{"valid","missing_root","root_binding","unknown_entry","duplicate_entry","duplicate_master","duplicate_version","duplicate_old_version","source_refusal","cancel"}{t.Run(change,func(t *testing.T){
  bindings:=[]ImportCorrectionBinding{correctionBindingFixture(),correctionBindingFixture()}
  scope:=ImportPreparedScope{Entry:bindings[0].Entry,Binding:bindings[0].New}
  document:=importParsedValidatorFixture()
  first:=document.Manifest.Entries[0];first.ID=bindings[0].Entry;first.ImageSHA=rawSHA([]byte("first"))
  second:=first;second.ID=bindings[1].Entry;second.ImageSHA=rawSHA([]byte("second"))
  document.Manifest.Entries=append(document.Manifest.Entries[:0],first,second)
  sentinel:=errors.New("source revoked")
  source:=&correctionSourceStub{pairs:append([]ImportCorrectionBinding(nil),bindings...)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch change{
  case "missing_root":scope.Entry=uuid.New()
  case "root_binding":scope.Binding.AssetVersionID=uuid.New()
  case "unknown_entry":bindings[1].Entry=uuid.New()
  case "duplicate_entry":bindings[1].Entry=bindings[0].Entry
  case "duplicate_master":bindings[1].New.Source=bindings[0].New.Source
  case "duplicate_version":bindings[1].New.AssetVersionID=bindings[0].New.AssetVersionID
  case "duplicate_old_version":bindings[1].Old.AssetVersionID=bindings[0].Old.AssetVersionID
  case "source_refusal":source.fail=sentinel
  case "cancel":cancel()
  }
  evidence,err:=qualifyImportEntrySources(ctx,scope,document,bindings,source)
  if change=="valid"{
   if err!=nil||len(evidence)!=2||source.calls!=2{t.Fatalf("siblings missing: %v",err)}
   for i,item:=range bindings{got:=evidence[item.New.Source.ArtifactID];if got.ref!=item.New.Source||got.canvas!=item.New.CanvasHash||got.original!=document.Manifest.Entries[i].ImageSHA{t.Fatal("source binding lost")}}
   return
  }
  if err==nil||evidence!=nil{t.Fatal("invalid source set accepted")}
  if change=="source_refusal"{if !errors.Is(err,sentinel)||source.calls!=1{t.Fatal("source refusal lost")}}else if source.calls!=0{t.Fatal("invalid mapping invoked IO")}
  if change=="cancel"&&!errors.Is(err,context.Canceled){t.Fatal(err)}
 })}
}
