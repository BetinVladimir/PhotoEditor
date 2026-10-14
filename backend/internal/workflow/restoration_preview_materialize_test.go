package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

func TestRestorationPreviewTaskBodiesRequireExactFourSideBindings(t *testing.T) {
	plan := restorationPreviewFixturePlan(t)
	docs := plan.Documents()
	tasks := make([]PreparedTask, 4)
	for i, doc := range docs {
		var err error
		tasks[i], err = PrepareRestorationPreviewTask(doc,
			review.ArtifactRef{ID: uuid.New(), SHA256: doc.ParamsSHA})
		if err != nil {
			t.Fatal(err)
		}
	}
	bodies, err := restorationPreviewTaskBodies(docs, tasks)
	if err != nil || len(bodies) != 4 {
		t.Fatal(err)
	}
	swapped := append([]PreparedTask(nil), tasks...)
	swapped[0], swapped[2] = swapped[2], swapped[0]
	if _, err = restorationPreviewTaskBodies(docs, swapped); err == nil {
		t.Fatal("before/after task swap accepted")
	}
	mutated := append([]PreparedTask(nil), tasks...)
	mutated[3].NodeKey = "restoration.before:preview2560"
	if _, err = restorationPreviewTaskBodies(docs, mutated); err == nil {
		t.Fatal("node substitution accepted")
	}
	if _, err = restorationPreviewTaskBodies(docs[:3], tasks[:3]); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("three-task graph accepted", err)
	}
	if err = MaterializeRestorationPreviewTasks(context.Background(), nil,
		plan, nil, nil, tasks); !errors.Is(err, objectstore.ErrInvalid) {
		t.Fatal("uninstalled materializer accepted", err)
	}
}
