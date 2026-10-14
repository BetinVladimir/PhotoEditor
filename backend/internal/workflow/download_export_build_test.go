package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
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

func TestDownloadExportRegisterRefusesOversizedGeometry(t *testing.T) {
 spec:=downloadExportFixture()
 edge:=int64(100000)
 spec.Preset.Size=&ExportSize{LongEdge:&edge,Fit:"contain",AllowUpscale:true}
 q:=&ingestQueries{}
 batch:=DownloadExportBatch{Spec:spec}
 // Register compiles before any SQL insert or parameter publication.
 if nodes,err:=(DownloadExportPlanRepository{DB:q}).Register(context.Background(),uuid.New(),batch);err==nil || len(nodes)!=0 {
  t.Fatal("oversize export registration accepted")
 }
}

func TestPrepareDownloadExportRejectsNoncanonicalOrUnsupportedParams(t *testing.T) {
 for _,mode:=range []string{"unknown","duplicate","icc","whitespace","color","alias"} {
  t.Run(mode,func(t *testing.T){
   spec:=downloadExportFixture()
   registry,err:=LoadRegistry()
   if err!=nil { t.Fatal(err) }
   nodes,err:=CompileDownloadExports(spec,registry)
   if err!=nil { t.Fatal(err) }
   n:=nodes[0]
   item:=spec.Items[0]
   switch mode {
   case "unknown":n.Params=append(append([]byte{},n.Params[:len(n.Params)-1]...),[]byte(",\"unsupported\":true}")...)
   case "duplicate":n.Params=append(append([]byte{},n.Params[:len(n.Params)-1]...),[]byte(",\"format\":\"jpeg\"}")...)
   case "icc":
    var p encodeParams
    if err:=json.Unmarshal(n.Params,&p);err!=nil { t.Fatal(err) }
    p.ICCSHA256="bad"
    n.Params,err=canonical.Marshal(p)
    if err!=nil { t.Fatal(err) }
   case "whitespace":n.Params=append(n.Params,'\n')
   case "color":item.Image.Color.Encoding="linear"
   case "alias":item.Image.Pixels=item.Manifest;n.Inputs[1]=item.Manifest
   }
   sum:=sha256.Sum256(n.Params)
   entry:=PublishedDownloadExport{Node:n,Params:review.ArtifactRef{ID:uuid.New(),SHA256:hex.EncodeToString(sum[:])}}
   sizes:=map[uuid.UUID]int64{}
   for _,ref:=range n.Inputs { sizes[ref.ID]=4096 }
   if _,err:=PrepareDownloadExportTask(entry,sizes,item);err==nil { t.Fatal("invalid params/source accepted") }
  })
 }
}
