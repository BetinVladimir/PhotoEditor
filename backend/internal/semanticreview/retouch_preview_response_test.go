package semanticreview

import (
	"context"
	"testing"
	"fmt"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/objectstore"
	"time"
)

func TestRetouchResponseRejectsUnverifiedMediaBeforeSigning(t *testing.T) {
	got, err := BuildRetouchPreviewResponse(context.Background(), PhysicalRetouchPreview{}, PreviewRequest{}, time.Now(), nil, nil)
	if err != ErrInvalid || got.SchemaVersion != 0 {
		t.Fatal("unverified media exposed", err)
	}
}

func TestRetouchResponseCancellationBeforeValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := BuildRetouchPreviewResponse(ctx, PhysicalRetouchPreview{}, PreviewRequest{}, time.Now(), nil, nil)
	if err != context.Canceled || got.SchemaVersion != 0 {
		t.Fatal("cancelled media exposed", err)
	}
}

func TestRetouchResponseRejectsStaleBindingsBeforeAuthorityOrSigning(t *testing.T) {
	for _, field := range []string{"workflow", "target", "plan", "candidate"} {
		t.Run(field, func(t *testing.T) {
			params := retouchParamsFixture()
			request := PreviewRequest{ExpectedRevision: params.WorkflowRevision, TargetRevision: params.TargetRevision, PlanSHA: params.Plan.SHA256, CandidateSHA: params.Candidate.SHA256}
			switch field {
			case "workflow": request.ExpectedRevision = "1"
			case "target": request.TargetRevision = "1"
			case "plan": request.PlanSHA = "0000000000000000000000000000000000000000000000000000000000000000"
			case "candidate": request.CandidateSHA = "0000000000000000000000000000000000000000000000000000000000000000"
			}
			physical := PhysicalRetouchPreview{verified: true, linkage: RetouchPreviewPresentationLinkage{Presentation: RetouchPreviewPresentation{Params: params}}}
			calls := 0
			got, err := BuildRetouchPreviewResponse(context.Background(), physical, request, time.Now(),
				func(context.Context, objectstore.Object, time.Time) (string, error) { calls++; return "", nil },
				func(context.Context) error { calls++; return nil })
			if err != ErrConflict || got.SchemaVersion != 0 || calls != 0 { t.Fatal("stale request exposed media", field, err, calls) }
		})
	}
}

func TestRetouchSignedResponseFailuresNeverExposePartialMedia(t *testing.T) {
	for _, mode := range []string{"valid", "authority_before", "authority_after", "cancel_authority_before", "cancel_authority_after", "sign_error", "sign_cancel", "invalid_url", "missing_version", "wrong_version", "duplicate_version"} {
		for stage := 1; stage <= 4; stage++ {
			if stage > 1 && mode != "sign_error" && mode != "sign_cancel" && mode != "invalid_url" && mode != "missing_version" && mode != "wrong_version" && mode != "duplicate_version" { continue }
			t.Run(fmt.Sprintf("%s_%d",mode,stage),func(t *testing.T) {
				docs := buildRetouchPhysicalDocuments(t)
				run := uuid.New()
				query := &retouchPipelineReadQuery{t:t, docs:docs, task:uuid.NewSHA1(run,[]byte("semantic.preview_retouch_region:"+docs.params.TargetID.String())), result:uuid.New(), adapter:v2input.Ref{ArtifactID:uuid.New(),SHA256:docs.params.Plan.SHA256}}
				store := &retouchPipelineReadStore{t:t,query:query}
				physical, err := ReadPhysicalRetouchPreview(context.Background(),query,store,"fixture",auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},run,docs.params,query.adapter,docs.source,docs.params.Plan.SHA256,docs.srgbSHA)
				if err != nil { t.Fatal(err) }
				ctx,cancel := context.WithCancel(context.Background())
				defer cancel()
				now := time.Now()
				request := PreviewRequest{ExpectedRevision:docs.params.WorkflowRevision,TargetRevision:docs.params.TargetRevision,PlanSHA:docs.params.Plan.SHA256,CandidateSHA:docs.params.Candidate.SHA256}
				calls, checks := 0,0
				storageErr := errors.New("signer unavailable")
				got,err := BuildRetouchPreviewResponse(ctx,physical,request,now,
					func(_ context.Context,obj objectstore.Object,expiry time.Time)(string,error) {
						calls++
						if !expiry.Equal(now.Add(time.Minute)) || obj.VersionID != "immutable-v1" || obj.SHA256 != retouchPhysicalFixtureSHA(docs.bodies[obj.Key]) { t.Fatal("signed wrong version/expiry") }
						if calls == stage {
							switch mode {
							case "sign_error": return "",storageErr
							case "sign_cancel": cancel()
							case "invalid_url": return "http://unsafe.example.test/image",nil
							case "missing_version": return "https://media.example.test/"+obj.Key,nil
							case "wrong_version": return "https://media.example.test/"+obj.Key+"?versionId=other",nil
							case "duplicate_version": return "https://media.example.test/"+obj.Key+"?versionId="+obj.VersionID+"&versionId=other",nil
							}
						}
						return "https://media.example.test/"+obj.Key+"?versionId="+obj.VersionID,nil
					},func(context.Context)error {
						checks++
						if checks==1 && mode=="authority_before" || checks==2 && mode=="authority_after" { return ErrConflict }
						if checks==1 && mode=="cancel_authority_before" || checks==2 && mode=="cancel_authority_after" { cancel() }
						return nil
					})
				if mode=="valid" {
					if err!=nil || got.SchemaVersion!=1 || calls!=4 || checks!=2 { t.Fatal("valid signed response refused",err) }
					return
				}
				if err==nil || got.SchemaVersion!=0 || got.Before.URL!="" || got.After.URL!="" { t.Fatal("partial signed response escaped",mode,err) }
				if mode=="sign_error" && (!errors.Is(err,storageErr) || calls!=stage) { t.Fatal("signer error replaced or continued",err,calls) }
				if mode=="sign_cancel" && (err!=context.Canceled || calls!=stage) { t.Fatal("sign cancellation replaced",err,calls) }
				if mode=="authority_before" && calls!=0 { t.Fatal("signed before authority") }
				if store.opened!=store.closed { t.Fatal("physical fixture leaked") }
			})
		}
	}
}
