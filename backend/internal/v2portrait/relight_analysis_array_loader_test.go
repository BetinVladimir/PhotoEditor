package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type analysisArrayTestCatalog struct {
	object objectstore.Object
	calls  int
}

func (c *analysisArrayTestCatalog) ResolveAnalysisArray(context.Context, RelightAnalysisPlanRequest, RelightAnalysisArraySpec) (objectstore.Object, error) {
	c.calls++
	return c.object, nil
}
func TestAnalysisArrayLoaderClosesInvalidStream(t *testing.T) {
	request := analysisPlanFixture(t)
	specs, err := AnalysisSourceArrays(request)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &analysisArrayTestCatalog{object: objectstore.Object{Key: "array", VersionID: "v1", ContentType: "application/x-npy", SHA256: specs[0].Ref.SHA256, Bytes: 12}}
	objects := &analysisEvidenceObjects{raw: []byte("invalid npy!")}
	if err := VerifyAnalysisSourceObjects(context.Background(), catalog, objects, request); err == nil {
		t.Fatal("invalid array accepted")
	}
	if objects.opens != 1 || !objects.closed {
		t.Fatal("stream not closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := VerifyAnalysisSourceObjects(ctx, catalog, objects, request); err != context.Canceled {
		t.Fatal(err)
	}
	if objects.opens != 1 {
		t.Fatal("cancelled request opened object")
	}
}
func TestAnalysisArrayCatalogRefusesForeignEdgeBeforeDatabase(t *testing.T) {
	request := analysisPlanFixture(t)
	specs, err := AnalysisSourceArrays(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"ref", "role", "width", "actor", "tenant", "source", "bucket"} {
		r := request
		spec := specs[0]
		db := &noAnalysisDraftDB{}
		catalog := SQLRelightAnalysisArrayCatalog{DB: db, Bucket: "photos"}
		switch fault {
		case "ref":
			spec.Ref.ArtifactID = uuid.New()
		case "role":
			spec.Role = "WorkingAlpha"
		case "width":
			spec.Width++
		case "actor":
			r.Actor = uuid.Nil
		case "tenant":
			r.Tenant = uuid.Nil
		case "source":
			r.SourceRaw = []byte("{}")
		case "bucket":
			catalog.Bucket = ""
		}
		if _, err := catalog.ResolveAnalysisArray(context.Background(), r, spec); err == nil {
			t.Fatal("accepted", fault)
		}
		if db.calls != 0 {
			t.Fatal("invalid edge reached database", fault)
		}
	}
}
