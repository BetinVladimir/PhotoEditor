package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type noAnalysisPublicationObjects struct{}

func (noAnalysisPublicationObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	panic("unexpected read")
}
func (noAnalysisPublicationObjects) PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error) {
	panic("unexpected write")
}
func (noAnalysisPublicationObjects) RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error) {
	panic("unexpected recovery")
}

func TestAnalysisCoordinatorRejectsBeforeAnyIO(t *testing.T) {
	for _, fault := range []string{"actor", "session", "source", "expected_operation", "expected_backend", "license", "reader", "writer", "db", "bucket", "cancel"} {
		request, _, expected := analysisQualificationFixture(t)
		db := &noAnalysisDraftDB{}
		c := RelightAnalysisCoordinator{DB: db, Reader: noAnalysisPublicationObjects{}, Writer: noAnalysisPublicationObjects{}, Bucket: "photos", Expected: expected}
		who := auth.Identity{UserID: request.Actor, SessionID: request.Session}
		ctx, cancel := context.WithCancel(context.Background())
		switch fault {
		case "actor":
			who.UserID = uuid.New()
		case "session":
			who.SessionID = uuid.New()
		case "source":
			request.SourceRaw = []byte("{}")
		case "expected_operation":
			c.Expected.Operation = "image.relight"
		case "expected_backend":
			c.Expected.Backend = "mps"
		case "license":
			c.Expected.LicenseCleared = false
		case "reader":
			c.Reader = nil
		case "writer":
			c.Writer = nil
		case "db":
			c.DB = nil
		case "bucket":
			c.Bucket = ""
		case "cancel":
			cancel()
		}
		_, err := c.PrepareParams(ctx, who, request)
		cancel()
		if err == nil || db.calls != 0 {
			t.Fatal("invalid preparation reached IO", fault, err)
		}
	}
}
func TestPreparedAnalysisOwnsReturnedPlan(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	prepared := PreparedRelightAnalysis{plan: plan}
	returned := prepared.Plan()
	returned.snapshot.Params[0] = 'x'
	returned.snapshot.Inputs[0].Contract = "altered"
	actual := prepared.Plan()
	if actual.snapshot.Params[0] == 'x' || actual.snapshot.Inputs[0].Contract == "altered" {
		t.Fatal("prepared result leaked owned plan")
	}
}
