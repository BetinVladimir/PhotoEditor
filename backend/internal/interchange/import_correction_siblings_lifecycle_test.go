package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type correctionSiblingArchive struct{bodies map[v2input.Ref][]byte;plan v2input.Ref;planReads int;changed bool}
func(r *correctionSiblingArchive)ReadImportArchiveDocument(_ context.Context,ref v2input.Ref,limit int64)([]byte,error){
 body,ok:=r.bodies[ref];if !ok||int64(len(body))>limit{return nil,v2input.ErrInvalid}
 if ref==r.plan{r.planReads++;if r.changed&&r.planReads>1{return []byte(`{}`),nil}}
 return append([]byte(nil),body...),nil
}
func TestCorrectionSiblingArchivedPlanLifecycle(t *testing.T){
 for _,change:=range []string{"valid","plan_changed","plan_hash","master_changed","ambiguous","foreign_root"}{t.Run(change,func(t *testing.T){
  _,master,masterRaw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(masterRaw);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  root:=correctionBindingFixture()
  sibling:=correctionBindingFixture();sibling.Old.Source.SHA256=master.Ref.SHA256;sibling.Old.CanvasHash=canvas
  sibling.New.Source=master.Ref;sibling.New.CanvasHash=canvas
  parsed:=importParsedValidatorFixture()
  first:=parsed.Manifest.Entries[0];first.ID=root.Entry;first.ImageSHA=rawSHA([]byte("root original"))
  second:=first;second.ID=sibling.Entry;second.ImageSHA=image.Canvas.SourceSHA256
  parsed.Manifest.Entries=append(parsed.Manifest.Entries[:0],first,second)
  sources:=ImportSourceInventory{document:importSourcesDocument{Entries:[]importSourceEntry{{ID:root.Entry,State:"adopted",Version:&root.New.AssetVersionID,Master:&root.New.Source},{ID:sibling.Entry,State:"adopted",Version:&sibling.New.AssetVersionID,Master:&sibling.New.Source}}}}
  rows:=[]any{}
  for i,b:=range []v2input.SessionVersionBinding{root.Old,sibling.Old}{rows=append(rows,map[string]any{"binding":b,"ordinal":i,"delta":v2draft.CorrectionVector{},"disposition":"no_change","reason":nil})}
  raw,err:=canonical.Marshal(map[string]any{"schema_version":1,"snapshot_digest":rawSHA([]byte("snapshot")),"items":rows});if err!=nil{t.Fatal(err)}
  plan:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
  stagedPlan:=v2input.Ref{ArtifactID:uuid.New(),SHA256:plan.SHA256}
  stagedSource:=v2input.Ref{ArtifactID:uuid.New(),SHA256:master.Ref.SHA256}
  archive:=&correctionSiblingArchive{bodies:map[v2input.Ref][]byte{stagedPlan:raw,stagedSource:masterRaw},plan:stagedPlan}
  current:=&recipeSourceMasterReader{body:masterRaw}
  q:=&ParsedImportRecipeSourceQualifier{originals:map[uuid.UUID]string{sibling.Entry:image.Canvas.SourceSHA256},archive:map[uuid.UUID]importArchivedSource{plan.ArtifactID:{staged:stagedPlan,sha:plan.SHA256,contract:"CorrectionPlan"},sibling.Old.Source.ArtifactID:{staged:stagedSource,sha:master.Ref.SHA256,contract:"WorkingImage"}},reader:archive,masters:current}
  planRoot:=importArchivedCorrectionRoot{Ref:plan,Binding:root.Old}
  switch change{case "plan_changed":archive.changed=true;case "plan_hash":archive.bodies[stagedPlan]=[]byte(`{}`);case "master_changed":current.second=[]byte(`{}`);case "ambiguous":parsed.Manifest.Entries[0].ImageSHA=image.Canvas.SourceSHA256;case "foreign_root":planRoot.Binding.AssetVersionID=uuid.New()}
  result,err:=extendImportCorrectionBindings(context.Background(),sources,parsed,q,[]ImportCorrectionBinding{root},[]importArchivedCorrectionRoot{planRoot})
  if change=="valid"{if err!=nil||len(result)!=2||result[0]!=root||result[1]!=sibling||archive.planReads!=3{t.Fatalf("complete sibling lifecycle: %v",err)}}else if err==nil||result!=nil{t.Fatal("changed authority accepted")}
 })}
}
