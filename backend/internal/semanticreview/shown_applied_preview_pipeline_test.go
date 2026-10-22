package semanticreview
import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "fmt"
 "io"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
type appliedPreviewFixture struct {p ShownExposureAdoptionParams;shown PlanSource;sources map[string]PlanSource;bodies map[string][]byte;revoked bool;opens,signs int;afterSign func();afterOpen func()}
func(q *appliedPreviewFixture)QueryRow(_ context.Context,query string,args ...any)pgx.Row{
 if q.revoked{return cleanupReadRow{err:pgx.ErrNoRows}}
 if strings.Contains(query,"semantic_shown_applied_preview_object"){
  if args[0]!=q.p.TenantID||args[1]!=q.p.TargetID{return cleanupReadRow{err:pgx.ErrNoRows}}
  a:=q.sources[args[4].(string)];return shownDocumentRow{source:a}
 }
 if strings.Contains(query,"semantic_shown_applied_object"){return shownDocumentRow{source:q.shown}}
 if strings.Contains(query,"semantic_shown_applied_image"){return cleanupReadRow{values:[]any{q.p.ChildRunID,uuid.NewSHA1(q.p.ChildRunID,[]byte("semantic.adopt_shown_background_cleanup:"+q.p.TargetID.String())),q.p.PreviewResultID,q.shown.Ref.ArtifactID,q.shown.Ref.SHA256}}}
 return cleanupReadRow{err:fmt.Errorf("unexpected query")}
}
func(q *appliedPreviewFixture)Open(_ context.Context,a objectstore.Object)(io.ReadCloser,error){q.opens++;if q.afterOpen!=nil{q.afterOpen()};raw,ok:=q.bodies[a.Key];if !ok{return nil,ErrInvalid};return io.NopCloser(bytes.NewReader(raw)),nil}
func(q *appliedPreviewFixture)SignRead(_ context.Context,_ objectstore.Object,_ time.Duration)(objectstore.ReadURL,error){q.signs++;if q.afterSign!=nil{q.afterSign()};return objectstore.ReadURL{URL:"https://objects.example/"+q.p.TargetID.String(),ExpiresAt:time.Now().Add(time.Minute)},nil}
func appliedPreviewPipelineFixture(t *testing.T)(*appliedPreviewFixture,string){
 d:=buildBackgroundCleanupPhysicalDocuments(t);p:=shownAdoptionFixture();p.TenantID=d.params.TenantID;p.TargetID=d.params.TargetID;p.PreviewTaskID=uuid.NewSHA1(p.PreviewRunID,[]byte("semantic.preview_background_cleanup:"+p.TargetID.String()));p.Presentation=d.presentationRef;p.NumericalEvidence=d.presentation.NumericalEvidence;p.WorkingImage=d.presentation.WorkingImage;p.Pixels=d.presentation.Pixels
 q:=&appliedPreviewFixture{p:p,sources:map[string]PlanSource{},bodies:map[string][]byte{}}
 put:=func(kind,contract,media string,ref v2input.Ref,raw []byte)PlanSource{a:=PlanSource{Contract:contract,Ref:ref,Bytes:int64(len(raw)),Bucket:"fixture",Key:ref.ArtifactID.String(),Version:"immutable",MediaType:media};q.sources[kind]=a;q.bodies[a.Key]=raw;return a}
 ref:=func(raw []byte)v2input.Ref{sum:=sha256.Sum256(raw);return v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
 params,_:=canonical.Marshal(p);put("params","Params","application/json",ref(params),params)
 put("presentation","Presentation","application/json",p.Presentation,d.bodies[p.Presentation.ArtifactID.String()])
 original:=d.bodies[p.WorkingImage.ArtifactID.String()];put("source_image","WorkingImage","application/json",p.WorkingImage,original)
 image,err:=review.ParseWorkingImage(original);if err!=nil{t.Fatal(err)};image.Pixels.ID=uuid.New();copied,_:=canonical.Marshal(image);q.shown=put("shown","WorkingImage","application/json",ref(copied),copied)
 put("after","PNG","image/png",d.presentation.After,d.bodies[d.presentation.After.ArtifactID.String()])
 return q,d.srgbSHA
}
func TestAppliedTargetPreviewPhysicalChainAndCurrentAuthority(t *testing.T){
 for _,mode:=range []string{"success","revoked_after_sign","cancel_after_sign","cancel_during_read","wrong_copied_pixels"}{t.Run(mode,func(t *testing.T){
  q,srgb:=appliedPreviewPipelineFixture(t);ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "revoked_after_sign":q.afterSign=func(){q.revoked=true};case "cancel_after_sign":q.afterSign=cancel;case "cancel_during_read":q.afterOpen=cancel;case "wrong_copied_pixels":image,_:=review.ParseWorkingImage(q.bodies[q.shown.Key]);image.Pixels.SHA256=strings.Repeat("f",64);raw,_:=canonical.Marshal(image);sum:=sha256.Sum256(raw);q.shown.Ref.SHA256=hex.EncodeToString(sum[:]);q.shown.Bytes=int64(len(raw));q.bodies[q.shown.Key]=raw}
  service,err:=NewShownAppliedPreviewService(q,q,q,"fixture",srgb);if err!=nil{t.Fatal(err)}
  got,err:=service.ReadAppliedPreview(ctx,auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},q.p.TenantID,q.p.TargetID)
  if mode=="success"{if err!=nil||got.TargetID!=q.p.TargetID||got.ChildRunID!=q.p.ChildRunID||got.After.ArtifactID!=q.sources["after"].Ref.ArtifactID||q.signs!=1{t.Fatalf("result=%+v error=%v signs=%d",got,err,q.signs)}}else if err==nil||got.After.URL!=""{t.Fatal("stale physical authority exposed URL")}
  if (mode=="cancel_during_read"||mode=="wrong_copied_pixels")&&q.signs!=0{t.Fatal("signed unverified media")}
 })}
}

// Even a faulty metadata adapter must not substitute another immutable object.
func TestAppliedPreviewRejectsSubstitutedObjectIdentityBeforeRead(t *testing.T){
 for _,kind:=range []string{"presentation","source_image","after"}{t.Run(kind,func(t *testing.T){
  q,srgb:=appliedPreviewPipelineFixture(t);a:=q.sources[kind];a.Ref.ArtifactID=uuid.New();q.sources[kind]=a
  service,err:=NewShownAppliedPreviewService(q,q,q,"fixture",srgb);if err!=nil{t.Fatal(err)}
  got,err:=service.ReadAppliedPreview(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},q.p.TenantID,q.p.TargetID)
  if err==nil||got.After.URL!=""||q.signs!=0{t.Fatal("substituted media identity accepted")}
 })}
}
