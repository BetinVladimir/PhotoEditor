package semanticreview

import (
	"context"
 "fmt"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"testing"
	"time"
)

func TestRemovalPreviewResponseRejectsUnverifiedBeforeSigning(t *testing.T) {
	called := false
	sign := func(context.Context, objectstore.Object, time.Time) (string, error) { called = true; return "", nil }
	_, err := BuildRemovalPreviewResponse(context.Background(), PhysicalRemovalPreview{}, PreviewRequest{}, time.Now(), sign,
		func(context.Context) error { called = true; return nil })
	if err == nil || called {
		t.Fatal("unverified preview reached signing")
	}
}

func TestRemovalPreviewResponseSigningAndLateAuthority(t *testing.T) {
 for _,mode:=range []string{"valid","before_authority","after_authority","sign_1","sign_2","sign_3","sign_4","invalid_url","cancel","request_revision","live_valid","late_result","late_revoked"} {
  t.Run(mode,func(t *testing.T) {
   d:=buildRemovalPhysicalDocuments(t)
   run:=uuid.New()
   q:=&removalLinkageQuery{t:t,presentation:d.presentation,ref:d.presentationRef,bodies:d.bodies,task:uuid.NewSHA1(run,[]byte("semantic.preview_remove_object:"+d.params.TargetID.String())),result:uuid.New(),replacement:uuid.New()}
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   store:=&removalPhysicalStore{q:q,cancel:cancel}
   who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
   physical,err:=ReadPhysicalRemovalPreview(ctx,q,store,"fixture",who,run,d.params,d.source,d.params.Plan.SHA256,d.srgbSHA)
   if err!=nil {t.Fatal("physical fixture",err)}
   request:=PreviewRequest{ExpectedRevision:d.params.WorkflowRevision,TargetRevision:d.params.TargetRevision,PlanSHA:d.params.Plan.SHA256,CandidateSHA:d.params.Candidate.SHA256}
   if mode=="request_revision" {request.TargetRevision="999"}
   now:=time.Now();signed,checks:=0,0
   got,err:=BuildRemovalPreviewResponse(ctx,physical,request,now,func(_ context.Context,o objectstore.Object,expiry time.Time)(string,error) {
    signed++
    expected:=physical.linkage.Sidecars[signed+2]
    if o.Key!=expected.Key || o.VersionID!=expected.Version || o.SHA256!=expected.Ref.SHA256 || o.Bytes!=expected.Bytes || o.ContentType!="image/png" || !expiry.Equal(now.Add(time.Minute)) {t.Fatal("unbound signer",o,expiry)}
    if mode==fmt.Sprintf("sign_%d",signed) {return "",ErrUnavailable}
    if mode=="cancel" {cancel()}
    if signed==4 && mode=="late_result" {q.result=q.replacement}
    if signed==4 && mode=="late_revoked" {q.mode="revoked"}
    if mode=="invalid_url" {return "https://user:secret@media.example.test/image",nil}
    return "https://media.example.test/"+o.Key,nil
   },func(context.Context)error {
    checks++
    if mode=="before_authority" && checks==1 || mode=="after_authority" && checks==2 {return ErrConflict}
    if mode=="live_valid" || mode=="late_result" || mode=="late_revoked" {return recheckRemovalIssuedPreview(ctx,q,store,"fixture",who,run,physical,d.params.Plan.SHA256,d.srgbSHA)}
    return nil
   })
   if mode=="valid" || mode=="live_valid" {
    if err!=nil || signed!=4 || checks!=2 || got.QualityStatus!="review" || got.Evidence.ArtifactID!=d.presentationRef.ArtifactID || !got.Before.ExpiresAt.Equal(now.Add(time.Minute)) {t.Fatal("invalid issuance",got,err,signed,checks)}
   } else if err==nil || got.SchemaVersion!=0 || got.Before.URL!="" || got.After.URL!="" {t.Fatal("partial signed response escaped",mode,got,err)}
   if (mode=="late_result" || mode=="late_revoked") && (signed!=4 || checks!=2) {t.Fatal("late producer not exercised",signed,checks)}
   if store.opened!=store.closed {t.Fatal("issuance stream leaked",store.opened,store.closed)}
   if mode=="live_valid" && (q.rows!=20 || q.queries!=10) {t.Fatal("missing live issuance reread",q.rows,q.queries)}
   if mode=="before_authority" && signed!=0 || mode=="request_revision" && (signed!=0 || checks!=0) || mode=="after_authority" && (signed!=4 || checks!=2) {t.Fatal("issuance order",mode,signed,checks)}
  })
 }
}
