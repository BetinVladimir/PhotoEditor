package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestAnalysisStageRefusesChangedPreparationBeforeIO(t *testing.T) {
	for _, fault := range []string{"params_id", "params_sha", "actor", "session", "key", "snapshot", "source", "runtime", "empty", "cancel"} {
		request, _, expected := analysisQualificationFixture(t)
		plan, err := CompileRelightAnalysisPlan(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		prepared := PreparedRelightAnalysis{plan: plan, params: artifactjson.Ref{ID: plan.snapshot.ParamsDocument, SHA256: plan.snapshot.ParamsSHA}}
		db := &noAnalysisDraftDB{}
		c := RelightAnalysisCoordinator{DB: db, Reader: noAnalysisPublicationObjects{}, Bucket: "photos", Expected: expected}
		who := auth.Identity{UserID: request.Actor, SessionID: request.Session}
		ctx, cancel := context.WithCancel(context.Background())
		switch fault {
		case "params_id":
			prepared.params.ID = uuid.New()
		case "params_sha":
			prepared.params.SHA256 = "invalid"
		case "actor":
			who.UserID = uuid.New()
		case "session":
			who.SessionID = uuid.New()
		case "key":
			request.Key = "different-key"
		case "snapshot":
			request.Snapshot = uuid.New()
		case "source":
			request.SourceRaw = []byte("{}")
		case "runtime":
			request.Runtime.RuntimeLockSHA = request.Runtime.ModelLockSHA
		case "empty":
			prepared = PreparedRelightAnalysis{}
		case "cancel":
			cancel()
		}
		err = c.Stage(ctx, who, request, prepared)
		cancel()
		if err == nil || db.calls != 0 {
			t.Fatal("invalid staging reached IO", fault, err)
		}
	}
}
