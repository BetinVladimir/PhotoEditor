package v2render

import (
 "photoeditor/backend/internal/canonical"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestPerspectivePreviewReplacementRepeatedReadUsesValueEquality(t *testing.T){
 identity:=PerspectivePreviewReplacementIdentity{Review:uuid.New(),OriginalTask:uuid.New(),OriginalRun:uuid.New(),DecisionHash:strings.Repeat("d",64)}
 raw,err:=canonical.Marshal(identity);if err!=nil{t.Fatal(err)}
 first,err:=decodePerspectivePreviewReplacementIdentity(raw);if err!=nil{t.Fatal(err)}
 second,err:=decodePerspectivePreviewReplacementIdentity(append([]byte(nil),raw...));if err!=nil{t.Fatal(err)}
 before:=perspectivePreviewSourceFixture(t);before.Replacement=first
 after:=before;after.Replacement=second
 if before!=after{t.Fatal("identical repeated observations differ")}
 exposed:=before.Replacement;exposed.OriginalTask=uuid.New()
 if before.Replacement!=identity{t.Fatal("caller mutation changed retained source")}
 after.Replacement.DecisionHash=strings.Repeat("e",64)
 if before==after{t.Fatal("changed identity observation accepted")}
}

func perspectivePreviewSourceFixture(t *testing.T) PerspectivePreviewSource {
	t.Helper()
	run := uuid.New()
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := v2input.TaskID(workflow, "optical.perspective", 0)
	if err != nil {
		t.Fatal(err)
	}
	return PerspectivePreviewSource{Tenant: uuid.New(), Project: uuid.New(), Run: run, Workflow: workflow, Producer: producer, ResultID: uuid.New(), Result: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Attempt: 1, Fence: 2, Principal: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, CanvasHash: strings.Repeat("c", 64)}}
}

func TestPerspectivePreviewReplacementIdentity(t *testing.T){
 value:=perspectivePreviewSourceFixture(t)
 id:=PerspectivePreviewReplacementIdentity{Review:uuid.New(),OriginalTask:uuid.New(),OriginalRun:uuid.New(),DecisionHash:strings.Repeat("d",64)}
 namespace:=uuid.NewSHA1(value.Tenant,[]byte("V29.NegativeReplacement/v2:"+value.Project.String()+":"+id.Review.String()+":"+id.DecisionHash))
 value.Run=uuid.NewSHA1(namespace,[]byte("run:"+id.OriginalRun.String()+":"+id.OriginalTask.String()))
 value.Workflow,_=v2input.WorkflowID(value.Run)
 value.Producer=uuid.NewSHA1(namespace,[]byte("task:"+id.OriginalTask.String()))
 value.Replacement=id
 if err:=validatePerspectivePreviewSource(value);err!=nil{t.Fatal(err)}
 for _,field:=range []string{"review","original_task","original_run","hash","tenant","project","missing"}{
  bad:=value;changed:=id;bad.Replacement=changed
  switch field{case "review":changed.Review=uuid.New();case "original_task":changed.OriginalTask=uuid.New();case "original_run":changed.OriginalRun=uuid.New();case "hash":changed.DecisionHash=strings.Repeat("e",64);case "tenant":bad.Tenant=uuid.New();case "project":bad.Project=uuid.New();case "missing":bad.Replacement=PerspectivePreviewReplacementIdentity{}}
  if field!="missing"{bad.Replacement=changed}
  if err:=validatePerspectivePreviewSource(bad);err==nil{t.Fatal("replacement identity tampering accepted",field)}
 }
 for _,raw:=range []string{`{}`,`{"extra":true}`,`[]`,`{"review_id":"bad"}`} {
  if _,err:=decodePerspectivePreviewReplacementIdentity([]byte(raw));err==nil{t.Fatal("unscoped identity accepted",raw)}
 }
}

func TestPerspectivePreviewSourceExactIdentity(t *testing.T) {
	value := perspectivePreviewSourceFixture(t)
	if err := validatePerspectivePreviewSource(value); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*PerspectivePreviewSource){
		func(v *PerspectivePreviewSource) { v.Workflow = uuid.New() }, func(v *PerspectivePreviewSource) { v.Producer = uuid.New() },
		func(v *PerspectivePreviewSource) { v.Attempt = 0 }, func(v *PerspectivePreviewSource) { v.Fence = 0 },
		func(v *PerspectivePreviewSource) { v.Principal.SessionID = uuid.Nil }, func(v *PerspectivePreviewSource) { v.Base.CanvasHash = "invalid" },
		func(v *PerspectivePreviewSource) { v.Result = v.Base.Source }, func(v *PerspectivePreviewSource) { v.Project = uuid.Nil },
	}
	for i, mutate := range mutations {
		bad := value
		mutate(&bad)
		if err := validatePerspectivePreviewSource(bad); err == nil {
			t.Fatal("invalid source accepted", i)
		}
	}
}

func TestPerspectivePreviewSourceRefusesBeforeDatabase(t *testing.T) {
	source := SQLPerspectivePreviewSource{}
	if _, err := source.Resolve(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("missing database accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Resolve(ctx, uuid.New(), uuid.New()); err != context.Canceled {
		t.Fatal("cancellation not retained", err)
	}
}
