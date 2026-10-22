package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
	"time"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

func TestRetouchPreviewProviderRequiresDependencies(t *testing.T) {
	if _, err := NewRetouchPreviewProvider(nil, nil, "", nil, nil, "a", "b"); err == nil {
		t.Fatal("incomplete provider installed")
	}
	var provider *RetouchPreviewProvider
	got, err := provider.ReadPreview(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), PreviewRequest{})
	if err != ErrUnavailable || got.SchemaVersion != 0 {
		t.Fatal("nil provider returned preview", err)
	}
}

func TestRetouchPreviewProviderCancellationBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var provider *RetouchPreviewProvider
	if _, err := provider.ReadPreview(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), PreviewRequest{}); err != context.Canceled {
		t.Fatal("cancel ignored", err)
	}
}

func TestRetouchProviderInvalidScopeStopsBeforeResolverAndStorage(t *testing.T) {
	for _, mode := range []string{"tenant", "intent", "target", "actor", "session", "revision", "plan", "candidate"} {
		t.Run(mode,func(t *testing.T) {
			params := retouchParamsFixture()
			who := auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
			tenant,intent,target := params.TenantID,params.IntentID,params.TargetID
			request := PreviewRequest{ExpectedRevision:params.WorkflowRevision,TargetRevision:params.TargetRevision,PlanSHA:params.Plan.SHA256,CandidateSHA:params.Candidate.SHA256}
			switch mode {
			case "tenant": tenant=uuid.Nil
			case "intent": intent=uuid.Nil
			case "target": target=uuid.Nil
			case "actor": who.UserID=uuid.Nil
			case "session": who.SessionID=uuid.Nil
			case "revision": request.TargetRevision="01"
			case "plan": request.PlanSHA="invalid"
			case "candidate": request.CandidateSHA="invalid"
			}
			store := &retouchPipelineReadStore{t:t}
			calls := 0
			provider := &RetouchPreviewProvider{db:&pgxpool.Pool{},store:store,bucket:"fixture",
				resolve:func(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,PreviewRequest)(RetouchPreviewLookup,error) { calls++; return RetouchPreviewLookup{},nil },
				sign:func(context.Context,objectstore.Object,time.Time)(string,error) { calls++; return "",nil }}
			got,err := provider.ReadPreview(context.Background(),who,tenant,intent,target,request)
			if err!=ErrInvalid || got.SchemaVersion!=0 || calls!=0 || store.opened!=0 { t.Fatal("invalid scope reached IO",mode,err,calls,store.opened) }
		})
	}
}
