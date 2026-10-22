package workflow

import (
 "testing"
 "context"
 "encoding/json"
 "errors"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/review"
)

func TestGenerationResourcesCoverFivePublishedArtifactsAndMappedInputs(t *testing.T){
 const width,height int64=6000,4000
 // RGB source, target, two protected float32 masks, including NPY headers.
 inputs:=width*height*24+4*128
 pixels,output,scratch,ram,err:=interactiveGenerationResources(width,height,inputs)
 if err!=nil {t.Fatal(err)}
 // Actual producer output: RGB + coverage + three bounded JSON documents.
 maximumWritten:=pixels*12+16384+pixels*4+8192+3*1048576
 if output<maximumWritten || scratch<inputs+maximumWritten {t.Fatal("coverage/documents or concurrently mapped inputs omitted",output,scratch)}
 requiredFrame:=int64(512*1048576)+pixels*32
 if ram<requiredFrame+pixels*192 {t.Fatal("full frame ROI cannot fit admitted RAM",ram)}
 // The old RGB-only estimate fails this same admitted photograph.
 if pixels*12+2*1048576>=pixels*16 {t.Fatal("fixture does not expose former output quota failure")}
}

func TestGenerationResourcesRetainSmallImageBudgetAndWorkerROILimit(t *testing.T){
 _,_,_,small,err:=interactiveGenerationResources(512,512,4*1048576)
 if err!=nil || small!=4<<30 {t.Fatal("small-image compatibility",small,err)}
 pixels,_,_,ram,err:=interactiveGenerationResources(10000,10000,2000000000)
 if err!=nil || ram!=512*1048576+pixels*32+32000000*192 {t.Fatal("worker ROI ceiling changed",ram,err)}
}

func TestGenerationResourceArithmeticRefusesUnsafeFrozenProfiles(t *testing.T){
 for _,v:=range [][3]int64{{0,1,0},{1,-1,0},{interactiveExactIntegerLimit,2,0},{1,1,-1},{1,1,interactiveExactIntegerLimit}} {
  if _,_,_,_,err:=interactiveGenerationResources(v[0],v[1],v[2]);err==nil {t.Fatal("unsafe estimate admitted",v)}
 }
 if _,err:=interactiveResourceAdd(interactiveExactIntegerLimit,1);err==nil {t.Fatal("sum overflow admitted")}
}

type generationResourceRow struct {ref review.ArtifactRef; bytes int64}
func(r generationResourceRow) Scan(dest ...any)error{
 *dest[0].(*uuid.UUID)=r.ref.ID;*dest[1].(*string)=r.ref.SHA256;*dest[2].(*int64)=r.bytes
 *dest[3].(*string)="bucket";*dest[4].(*string)="key";*dest[5].(*string)="version";*dest[6].(*string)="application/octet-stream"
 return nil
}
type generationResourceDB struct {refs map[uuid.UUID]generationResourceRow; calls int}
func(d *generationResourceDB)QueryRow(_ context.Context,_ string,args ...any)pgx.Row{d.calls++;return d.refs[args[1].(uuid.UUID)]}
func(d *generationResourceDB)Query(context.Context,string,...any)(pgx.Rows,error){return nil,errors.New("unexpected row stream")}
func TestGenerationMappedBytesCountsRepeatedProtectedInputsAndRefusesStaleIdentity(t *testing.T){
 rgb:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)}
 mask:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}
 db:=&generationResourceDB{refs:map[uuid.UUID]generationResourceRow{rgb.ID:{rgb,120128},mask.ID:{mask,40128}}}
 lease:=InteractiveLease{}
 lease.Params,_=json.Marshal(map[string]any{"mask":map[string]any{"artifact":mask},"protected":[]any{map[string]any{"artifact":mask},map[string]any{"artifact":mask}}})
 store:=InteractiveStore{DB:db};image:=review.WorkingImage{Pixels:rgb};inputs:=[]review.ArtifactRef{rgb,mask}
 got,err:=store.generationMappedBytes(context.Background(),lease,image,inputs)
 if err!=nil || got!=120128+3*40128 || db.calls!=4 {t.Fatal("deduplicated mapped occurrences",got,db.calls,err)}
 db.refs[mask.ID]=generationResourceRow{review.ArtifactRef{ID:mask.ID,SHA256:strings.Repeat("c",64)},40128}
 if _,err=store.generationMappedBytes(context.Background(),lease,image,inputs);err==nil {t.Fatal("metadata hash drift admitted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();before:=db.calls
 if _,err=store.generationMappedBytes(ctx,lease,image,inputs);!errors.Is(err,context.Canceled)||db.calls!=before {t.Fatal("cancelled estimate queried metadata",err)}
 if _,err=store.generationMappedBytes(context.Background(),lease,image,[]review.ArtifactRef{rgb});err==nil {t.Fatal("undeclared mask admitted")}
}
