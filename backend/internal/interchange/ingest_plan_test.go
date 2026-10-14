package interchange
import("context";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/v2input";"photoeditor/backend/internal/workflow")
func importedImage(name string)ImportedImageEvidence{return ImportedImageEvidence{EntryID:uuid.New(),ExternalID:uuid.NewString(),Path:name,OriginalSHA:strings.Repeat("a",64),Content:v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},Bytes:1024}}
func TestImportedIngestReusesDecodeDAGAndRetainsOrdinals(t *testing.T){
 job:=uuid.New();images:=[]ImportedImageEvidence{importedImage("one.NEF"),importedImage("unsupported.psd"),importedImage("three.tiff")}
 plan,err:=CompileImportedIngest(job,images);if err!=nil{t.Fatal(err)}
 if len(plan.Spec.Items)!=2||plan.Spec.Items[0].Ordinal!=0||plan.Spec.Items[1].Ordinal!=2||plan.Spec.Items[0].Suffix!=".nef"{t.Fatal("mapping ordinals lost")}
 if len(plan.Unsupported)!=1||plan.Unsupported[0].Ordinal!=1||plan.Unsupported[0].Reason!="unsupported_image_format"{t.Fatal("unsupported file omitted")}
 if len(plan.Graph.Nodes)!=6{t.Fatal("existing three-stage ingest not used")}
 for _,item:=range plan.Spec.Items{if item.AssumeProfile!=nil||item.Analyze||item.Tile!=512{t.Fatal("quality options changed")}}
 retry,err:=CompileImportedIngest(job,images);if err!=nil||retry.Spec.RunID!=plan.Spec.RunID||retry.Graph.Nodes[0].ID!=plan.Graph.Nodes[0].ID{t.Fatal("unstable replay identity")}
}
func TestImportedIngestRejectsSubstitutionAndInvalidPaths(t *testing.T){
 for _,name:=range []string{"../one.jpg","/one.jpg","a//b.jpg","a\\b.jpg","a:b.jpg"}{if _,err:=CompileImportedIngest(uuid.New(),[]ImportedImageEvidence{importedImage(name)});err==nil{t.Fatal("invalid path accepted",name)}}
 image:=importedImage("one.jpg");image.Content.SHA256=strings.Repeat("b",64)
 if _,err:=CompileImportedIngest(uuid.New(),[]ImportedImageEvidence{image});err==nil{t.Fatal("substituted content accepted")}
 first:=importedImage("one.jpg");second:=importedImage("ONE.JPG")
 if _,err:=CompileImportedIngest(uuid.New(),[]ImportedImageEvidence{first,second});err==nil{t.Fatal("path collision accepted")}
 plan,err:=CompileImportedIngest(uuid.New(),[]ImportedImageEvidence{importedImage("one.psd")});if err!=nil||len(plan.Spec.Items)!=0||len(plan.Unsupported)!=1{t.Fatal("all unsupported mapping lost")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=plan.PrepareInspection(ctx,uuid.New(),workflow.IngestInspectionCoordinator{});err!=context.Canceled{t.Fatal("cancelled inspection")}
}
