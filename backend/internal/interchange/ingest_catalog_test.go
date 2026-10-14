package interchange
import("context";"encoding/json";"strings";"testing";"github.com/google/uuid";"photoeditor/backend/internal/canonical";"photoeditor/backend/internal/review";"photoeditor/backend/internal/workflow")
func TestImportedCatalogRevalidatesWorkingImageAndPreservesScope(t *testing.T){
 tenant,who,_,_,_:=pendingFixture();run:=uuid.New();source:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)};pixels:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)};icc:=strings.Repeat("c",64)
 img:=review.WorkingImage{Pixels:pixels,Canvas:review.Canvas{Width:400,Height:300,Orientation:1,SpaceID:"oriented:"+source.SHA256,SourceSHA256:source.SHA256},Color:review.ColorSpace{ProfileID:"work_rec2020_gamma24_v1",Encoding:"gamma24",ICCSHA256:icc},DType:"float32",TransformChain:json.RawMessage(`{"hash":"4430e7786edc0f8419f02e909c15422ebf572287a58132d8f6f33250ce053121","steps":[]}`)}
 raw,err:=canonical.Marshal(img);if err!=nil{t.Fatal(err)}
 image:=workflow.CatalogImage{Ordinal:7,Source:source,DecodeTaskID:uuid.New(),MetadataRef:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("d",64)},Metadata:workflow.IngestMetadata{Source:source,Width:400,Height:300,Orientation:1,Format:"jpeg"},Outputs:workflow.DecodeOutputBinding{Manifest:review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA(raw)},Pixels:pixels},Image:img}
 tx:=&proofTx{accepted:true};adopter:=ImportedCatalogAdopter{DB:tx,Who:who,WorkICCSHA:icc}
 ok,err:=adopter.AdoptImage(context.Background(),tenant,run,image);if err!=nil||!ok{t.Fatal("valid catalog DTO refused",err)}
 if len(tx.args)!=11||tx.args[0]!=tenant||tx.args[1]!=who.UserID||tx.args[2]!=who.SessionID||tx.args[3]!=run||tx.args[9]!=image.MetadataRef.ID||tx.args[10]!=image.MetadataRef.SHA256{t.Fatal("catalog scope changed")}
 tx.args=nil;image.Image.Canvas.Width++
 if ok,err:=adopter.AdoptImage(context.Background(),tenant,run,image);err==nil||ok||tx.args!=nil{t.Fatal("mutated image reached SQL")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=adopter.AdoptImage(ctx,tenant,run,image);err!=context.Canceled||tx.args!=nil{t.Fatal("cancelled commit")}
}
