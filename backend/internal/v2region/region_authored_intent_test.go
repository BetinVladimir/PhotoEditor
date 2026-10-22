package v2region

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func authoredRegionIntentFixture() AuthoredRegionIntent {
	return AuthoredRegionIntent{Tenant: uuid.New(), Project: uuid.New(), Asset: uuid.New(), Actor: uuid.New(), Session: uuid.New(), DraftDocument: uuid.New(), DraftRevision: 1, FrozenDocument: uuid.New(), Binding: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: regionSHA}, CanvasHash: regionSHA}, Recipe: v2input.Ref{ArtifactID: uuid.New(), SHA256: regionSHA}, Rect: RegionRect{0, 0, 1, 1}, MaxEdge: 1024, TargetICCSHA: regionSHA, PresentationRuntimeSHA:regionSHA, PresentationICCLockSHA:regionSHA, RenderPolicySHA: regionSHA, Qualification: v2render.RenderQualificationPin{RenderQualification: v2render.RenderQualification{ModelSHA: regionSHA, RuntimeSHA: regionSHA, Backend: "mps", DeviceSHA: regionSHA, DatasetSHA: regionSHA}, CalibrationSHA: regionSHA}}
}

func TestAuthoredRegionCacheCannotReuseAnotherDraftSourceOrQualification(t *testing.T) {
	original := authoredRegionIntentFixture()
	base, err := original.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*AuthoredRegionIntent){
		"tenant":   func(i *AuthoredRegionIntent) { i.Tenant = uuid.New() },
		"project":  func(i *AuthoredRegionIntent) { i.Project = uuid.New() },
		"session":  func(i *AuthoredRegionIntent) { i.Session = uuid.New() },
		"actor":    func(i *AuthoredRegionIntent) { i.Actor = uuid.New() },
		"revision": func(i *AuthoredRegionIntent) { i.DraftRevision++ },
		"source":   func(i *AuthoredRegionIntent) { i.Binding.Source.ArtifactID = uuid.New() },
		"version":  func(i *AuthoredRegionIntent) { i.Binding.AssetVersionID = uuid.New() },
		"recipe":   func(i *AuthoredRegionIntent) { i.Recipe.ArtifactID = uuid.New() },
		"ROI":      func(i *AuthoredRegionIntent) { i.Rect.X0 = .25 },
		"scale":    func(i *AuthoredRegionIntent) { i.MaxEdge = 512 },
		"ICC": func(i *AuthoredRegionIntent) {
			i.TargetICCSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		},
		"presentation-runtime": func(i *AuthoredRegionIntent) { i.PresentationRuntimeSHA="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
        "presentation-icc-lock": func(i *AuthoredRegionIntent) { i.PresentationICCLockSHA="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" },
        "backend": func(i *AuthoredRegionIntent) { i.Qualification.Backend = "cuda" },
		"calibration": func(i *AuthoredRegionIntent) {
			i.Qualification.CalibrationSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := original
			mutate(&copy)
			hash, err := copy.SHA256()
			if err != nil || hash == base {
				t.Fatal("distinct immutable intent reused cache", err)
			}
		})
	}
	render, err := original.ChildKey("render")
	if err != nil {
		t.Fatal(err)
	}
	crop, err := original.ChildKey("crop")
	if err != nil || crop == render {
		t.Fatal("child phase collision", err)
	}
	if _, err = original.ChildKey("apply"); err == nil {
		t.Fatal("unexpected Apply phase accepted")
	}
}

func TestAuthoredRegionIntentRejectsUnboundedOrUnqualifiedShape(t *testing.T) {
	for name, mutate := range map[string]func(*AuthoredRegionIntent){
		"zero_revision":       func(i *AuthoredRegionIntent) { i.DraftRevision = 0 },
		"large_output":        func(i *AuthoredRegionIntent) { i.MaxEdge = 1025 },
		"empty_ROI":           func(i *AuthoredRegionIntent) { i.Rect.X1 = 0 },
		"unknown_backend":     func(i *AuthoredRegionIntent) { i.Qualification.Backend = "unknown" },
		"missing_calibration": func(i *AuthoredRegionIntent) { i.Qualification.CalibrationSHA = "" },
		"source_alias_recipe": func(i *AuthoredRegionIntent) { i.Recipe = i.Binding.Source },
	} {
		t.Run(name, func(t *testing.T) {
			intent := authoredRegionIntentFixture()
			mutate(&intent)
			if _, err := intent.SHA256(); err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}

func TestAuthoredRegionSeparateRequestsDoNotShareCancellationChildren(t *testing.T){
 i:=authoredRegionIntentFixture()
 a,ar,ac,err:=authoredRegionIDs(i,"request-a");if err!=nil{t.Fatal(err)}
 retry,rr,rc,err:=authoredRegionIDs(i,"request-a");if err!=nil||a!=retry||ar!=rr||ac!=rc{t.Fatal("retry changed immutable child identity",err)}
 other,br,bc,err:=authoredRegionIDs(i,"request-b");if err!=nil||a==other||ar==br||ac==bc{t.Fatal("separate request shares cancellation ownership",err)}
 first,err:=i.RequestChildKey(a,"render");if err!=nil||len(first)>128{t.Fatal("child key exceeds existing idempotency contract",err)}
 second,err:=i.RequestChildKey(other,"render");if err!=nil||first==second{t.Fatal("request domain missing",err)}
}
