package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func TestDownloadExportRegisterPrepareMaterialize(t *testing.T) {
	spec := downloadExportFixture()
	registry, _ := LoadRegistry()
	nodes, _ := CompileDownloadExports(spec, registry)
	n := nodes[0]
	sum := sha256.Sum256(n.Params)
	entry := PublishedDownloadExport{Node: n, Params: review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}}
	sizes := map[uuid.UUID]int64{}
	for _, ref := range n.Inputs {
		sizes[ref.ID] = 4096
	}
	task, err := PrepareDownloadExportTask(entry, sizes, spec.Items[0])
	if err != nil || task.Operation != "export.encode" || task.InputDigest == "" {
		t.Fatal(task, err)
	}
	q := &expansionQueries{row: expansionRow{values: []any{true}}}
	repo := DownloadExportPlanRepository{DB: q}
	if ok, e := repo.Materialize(context.Background(), uuid.New(), spec.RunID, task); e != nil || !ok {
		t.Fatal(e)
	}
	task.InputDigest = "bad"
	if _, e := repo.Materialize(context.Background(), uuid.New(), spec.RunID, task); e == nil {
		t.Fatal("bad digest accepted")
	}
}

func TestDownloadExportRegisterRejectsPartialReplay(t *testing.T) {
	spec := downloadExportFixture()
	batch := DownloadExportBatch{Spec: spec}
	q := &ingestQueries{failAt: 1}
	if _, err := (DownloadExportPlanRepository{DB: q}).Register(context.Background(), uuid.New(), batch); err == nil {
		t.Fatal("conflicting row accepted")
	}
}

func TestPrepareDownloadExportRequiresEveryExactInputSize(t *testing.T) {
	spec := downloadExportFixture()
	registry, _ := LoadRegistry()
	nodes, _ := CompileDownloadExports(spec, registry)
	sum := sha256.Sum256(nodes[0].Params)
	entry := PublishedDownloadExport{Node: nodes[0], Params: review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}}
	if _, err := PrepareDownloadExportTask(entry, map[uuid.UUID]int64{}, spec.Items[0]); err == nil {
		t.Fatal("missing sizes accepted")
	}
}
