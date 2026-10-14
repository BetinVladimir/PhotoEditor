package workflow

import (
 "testing"
 "crypto/sha256"
 "encoding/hex"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
)

func TestEncodeRAMMatchesProductionForLargeImagesAndAlpha(t *testing.T) {
 for _, alpha := range []bool{false,true} {
  source := int64(6000*4000)
  multiplier := int64(24)
  if alpha { multiplier=96 }
  want := int64(128<<20)+source*multiplier+source*96
  if got:=encodeRAMBytes(source,source,alpha);got!=want || got<=2<<30 { t.Fatalf("alpha=%v RAM=%d want=%d",alpha,got,want) }
 }
}

func TestDownloadTaskReservesComputedEncodeRAM(t *testing.T) {
 spec:=downloadExportFixture()
 spec.Items[0].Image.Canvas.Width=6000
 spec.Items[0].Image.Canvas.Height=4000
 registry,err:=LoadRegistry()
 if err!=nil { t.Fatal(err) }
 nodes,err:=CompileDownloadExports(spec,registry)
 if err!=nil { t.Fatal(err) }
 sum:=sha256.Sum256(nodes[0].Params)
 entry:=PublishedDownloadExport{Node:nodes[0],Params:review.ArtifactRef{ID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
 sizes:=map[uuid.UUID]int64{}
 for _,ref:=range nodes[0].Inputs { sizes[ref.ID]=4096 }
 task,err:=PrepareDownloadExportTask(entry,sizes,spec.Items[0])
 if err!=nil { t.Fatal(err) }
 estimate,err:=ParseResourceEstimate(task.ExecutionProfile)
 if err!=nil { t.Fatal(err) }
 if estimate.RAMBytes<=2<<30 { t.Fatalf("large export retained fixed budget: %d",estimate.RAMBytes) }
}

func TestEncodeDimensionsMatchesPythonRoundingAndUpscale(t *testing.T) {
 edge:=int64(5)
 for _, c:=range []struct{w,h int64; size *ExportSize; wantW,wantH int64}{
  {4,2,&ExportSize{LongEdge:&edge,Fit:"contain",AllowUpscale:true},5,2},
  {4,2,&ExportSize{LongEdge:&edge,Fit:"contain",AllowUpscale:false},4,2},
  {4,2,nil,4,2},
 } {
  w,h:=encodeDimensions(c.w,c.h,c.size)
  if w!=c.wantW || h!=c.wantH { t.Fatalf("dimensions %dx%d want %dx%d",w,h,c.wantW,c.wantH) }
 }
}

func TestPrepareDownloadExportRejectsSourceAndOperationSubstitution(t *testing.T) {
 for _, mode:=range []string{"version","manifest","pixels","alpha","negative"} {
  t.Run(mode,func(t *testing.T){
   spec:=downloadExportFixture()
   registry,err:=LoadRegistry()
   if err!=nil { t.Fatal(err) }
   nodes,err:=CompileDownloadExports(spec,registry)
   if err!=nil { t.Fatal(err) }
   sum:=sha256.Sum256(nodes[0].Params)
   entry:=PublishedDownloadExport{Node:nodes[0],Params:review.ArtifactRef{ID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
   sizes:=map[uuid.UUID]int64{}
   for _,ref:=range nodes[0].Inputs { sizes[ref.ID]=4096 }
   item:=spec.Items[0]
   switch mode {
   case "version":entry.Node.Version="2"
   case "manifest":item.Manifest.ID=uuid.New()
   case "pixels":item.Image.Pixels.ID=uuid.New()
   case "alpha":ref:=review.ArtifactRef{ID:uuid.New(),SHA256:item.Image.Pixels.SHA256};item.Image.Alpha=&ref
   case "negative":item.Image.Canvas.Width=-400;item.Image.Canvas.Height=-200
   }
   if _,err:=PrepareDownloadExportTask(entry,sizes,item);err==nil { t.Fatal("substitution accepted") }
  })
 }
}

func TestDownloadExportUpscaleAccountsTargetPixelsAndBytes(t *testing.T) {
 spec:=downloadExportFixture()
 edge:=int64(800)
 spec.Preset.Size=&ExportSize{LongEdge:&edge,Fit:"contain",AllowUpscale:true}
 registry,err:=LoadRegistry()
 if err!=nil { t.Fatal(err) }
 nodes,err:=CompileDownloadExports(spec,registry)
 if err!=nil { t.Fatal(err) }
 sum:=sha256.Sum256(nodes[0].Params)
 entry:=PublishedDownloadExport{Node:nodes[0],Params:review.ArtifactRef{ID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
 sizes:=map[uuid.UUID]int64{}
 for _,ref:=range nodes[0].Inputs { sizes[ref.ID]=4096 }
 task,err:=PrepareDownloadExportTask(entry,sizes,spec.Items[0])
 if err!=nil { t.Fatal(err) }
 estimate,err:=ParseResourceEstimate(task.ExecutionProfile)
 if err!=nil { t.Fatal(err) }
 if estimate.PixelCount!=800*400 || estimate.OutputBytes!=800*400*8+2*1048576 || estimate.RAMBytes!=encodeRAMBytes(400*200,800*400,false) {
  t.Fatalf("incorrect upscale admission: %+v",estimate)
 }
}
