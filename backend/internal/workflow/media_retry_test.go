package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
 "bytes"
 "time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/media"
	"photoeditor/backend/internal/review"
)

type mediaRetryPorts struct {
	lease                    *MediaRetryLease
	begun, sealed, released  int
	registered, materialized []uuid.UUID
	failPublish              bool
 failPublishAt int
 published [][]byte
 documents []MediaRetryDocument
 configs []json.RawMessage
 releaseDeadline time.Time
 afterStage func(string)
	tasks                    []PreparedTask
}

func (p *mediaRetryPorts) Claim(context.Context) (*MediaRetryLease, error) { return p.lease, nil }
func (p *mediaRetryPorts) Release(ctx context.Context, _ MediaRetryLease) (bool, error) {
 p.releaseDeadline,_=ctx.Deadline()
	p.released++
	return true, nil
}
func (p *mediaRetryPorts) Begin(_ context.Context, _ MediaRetryLease, _ uuid.UUID, _ string, config json.RawMessage) (bool, error) {
 p.configs=append(p.configs,append(json.RawMessage(nil),config...))
	p.begun++
 if p.afterStage!=nil{p.afterStage("begin")}
	return true, nil
}
func (p *mediaRetryPorts) Register(_ context.Context, _ MediaRetryLease, _ uuid.UUID, n MediaRetryDocument, _ string) (bool, error) {
	p.registered = append(p.registered, n.ID)
 p.documents=append(p.documents,n)
 if p.afterStage!=nil{p.afterStage("register")}
	return true, nil
}
func (p *mediaRetryPorts) Materialize(_ context.Context, _ MediaRetryLease, _ uuid.UUID, n PreparedTask) (bool, error) {
	p.materialized = append(p.materialized, n.ID)
	p.tasks = append(p.tasks, n)
 if p.afterStage!=nil{p.afterStage("materialize")}
	return true, nil
}
func (p *mediaRetryPorts) Seal(context.Context, MediaRetryLease, uuid.UUID) (bool, error) {
	p.sealed++
 if p.afterStage!=nil{p.afterStage("seal")}
	return true, nil
}
func (p *mediaRetryPorts) Publish(_ context.Context, s artifactjson.Scope, raw []byte) (artifactjson.Ref, error) {
	p.published=append(p.published,append([]byte(nil),raw...))
 if p.failPublish || p.failPublishAt>0 && len(p.published)==p.failPublishAt {
		return artifactjson.Ref{}, errors.New("store down")
	}
	sum := sha256.Sum256(raw)
 if p.afterStage!=nil{p.afterStage("publish")}
	return artifactjson.Ref{ID: uuid.NewSHA1(s.Owner, []byte("params")), SHA256: hex.EncodeToString(sum[:])}, nil
}

func TestBuildMediaRetryDocumentsAreDeterministicAndComplete(t *testing.T) {
	event, version := uuid.New(), uuid.New()
	master := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1000, Width: 6000, Height: 4000, MediaType: "image/jpeg"}
	first, err := BuildMediaRetryDocuments(event, version, master, []string{"preview", "thumbnail"}, strings.Repeat("b", 64))
	if err != nil || len(first) != 4 {
		t.Fatal(first, err)
	}
	second, err := BuildMediaRetryDocuments(event, version, master, []string{"thumbnail", "preview"}, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range first {
		if item.ID != second[i].ID || string(item.Params) != string(second[i].Params) || len(item.Inputs) != 1 || item.Inputs[0] != master.Artifact {
			t.Fatal("nondeterministic plan", i)
		}
		var params map[string]any
		if json.Unmarshal(item.Params, &params) != nil || len(params) != 5 || params["source_kind"] != "master" || params["suffix"] != ".jpg" || params["profile_hash"] == nil {
			t.Fatal(string(item.Params))
		}
	}
}

func TestPrepareMediaRetryTaskBindsParamsAndBudget(t *testing.T) {
	master := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1000, Width: 6000, Height: 4000, MediaType: "image/jpeg"}
	docs, err := BuildMediaRetryDocuments(uuid.New(), uuid.New(), master, []string{"thumbnail"}, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	task, err := PrepareMediaRetryTask(PublishedMediaRetry{Document: docs[0], Params: params}, master)
	if err != nil || task.ResourceClass != "media" || task.Params != params || task.InputDigest == "" {
		t.Fatal(task, err)
	}
	if _, err = ParseResourceEstimate(task.ExecutionProfile); err != nil {
		t.Fatal(err)
	}
}

func TestBuildMediaRetryRejectsUnsafeSources(t *testing.T) {
	valid := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1, Width: 1, Height: 1, MediaType: "image/jpeg"}
	for _, change := range []func(*MediaMaster){func(m *MediaMaster) { m.MediaType = "application/octet-stream" }, func(m *MediaMaster) { m.Width = 200000000 }, func(m *MediaMaster) { m.Artifact.SHA256 = "bad" }} {
		m := valid
		change(&m)
		if _, err := BuildMediaRetryDocuments(uuid.New(), uuid.New(), m, []string{"thumbnail"}, strings.Repeat("b", 64)); err == nil {
			t.Fatal("accepted", m)
		}
	}
	if _, err := BuildMediaRetryDocuments(uuid.New(), uuid.New(), valid, []string{"thumbnail", "thumbnail"}, strings.Repeat("b", 64)); err == nil {
		t.Fatal("accepted duplicates")
	}
}

func TestBuildMediaRetryFromWorkingMasterDeclaresNestedInputs(t *testing.T) {
	manifest, pixels, alpha := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}, review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	working := review.WorkingImage{Pixels: pixels, Alpha: &alpha, DType: "float32", Canvas: review.Canvas{Width: 120, Height: 80}, Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", Encoding: "gamma24", ICCSHA256: strings.Repeat("d", 64)}}
	docs, err := BuildMediaRetryDocuments(uuid.New(), uuid.New(), MediaMaster{Artifact: manifest, Bytes: 500, Working: &working}, []string{"preview"}, strings.Repeat("e", 64))
	if err != nil || len(docs) != 2 {
		t.Fatal(docs, err)
	}
	for _, doc := range docs {
		if len(doc.Inputs) != 3 || doc.Inputs[0] != manifest || doc.Inputs[1] != pixels || doc.Inputs[2] != alpha {
			t.Fatal(doc.Inputs)
		}
		var p map[string]any
		_ = json.Unmarshal(doc.Params, &p)
		if p["source_kind"] != "working" || p["suffix"] != nil || len(p) != 4 {
			t.Fatal(string(doc.Params))
		}
	}
}

func TestMediaRetryCoordinatorSealsDeterministicPlan(t *testing.T) {
	manifest, pixels := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	working := review.WorkingImage{Pixels: pixels, DType: "float32", Canvas: review.Canvas{Width: 120, Height: 80}, Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", Encoding: "gamma24", ICCSHA256: strings.Repeat("c", 64)}}
	p := &mediaRetryPorts{lease: &MediaRetryLease{RendererPolicy:"declared_encoding_alpha8_icc_v2",EventID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), AssetVersionID: uuid.New(), Variants: []string{"thumbnail"}, Master: MediaMaster{Artifact: manifest, Bytes: 100, Working: &working}}}
	done, err := (MediaRetryCoordinator{Store: p, Publisher: p, SRGBSHA: strings.Repeat("d", 64)}).Process(context.Background())
	if err != nil || !done || p.begun != 1 || p.sealed != 1 || p.released != 0 || len(p.registered) != 2 || len(p.materialized) != 2 {
		t.Fatal(done, err, p)
	}
	for i := range p.registered {
		if p.registered[i] != p.materialized[i] {
			t.Fatal("node/task identity mismatch")
		}
	}
}

func TestMediaRetryCoordinatorReleasesLeaseOnObjectFailure(t *testing.T) {
	master := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1, Width: 1, Height: 1, MediaType: "image/jpeg"}
	p := &mediaRetryPorts{failPublish: true, lease: &MediaRetryLease{RendererPolicy:"declared_encoding_alpha8_icc_v2",EventID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), AssetVersionID: uuid.New(), Variants: []string{"preview"}, Master: master}}
	done, err := (MediaRetryCoordinator{Store: p, Publisher: p, SRGBSHA: strings.Repeat("b", 64)}).Process(context.Background())
	if err == nil || done || p.released != 1 || p.sealed != 0 {
		t.Fatal(done, err, p.released, p.sealed)
	}
}

func TestMediaRetryPresentationResourcesCoverWorkerAdmission(t *testing.T) {
	for _, alpha := range []bool{false, true} {
		image := review.WorkingImage{Pixels: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, DType: "float32", Canvas: review.Canvas{Width: 64, Height: 32}}
		if alpha {
			ref := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
			image.Alpha = &ref
		}
		master := MediaMaster{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}, Working: &image}
		doc := MediaRetryDocument{ID: uuid.New(), EventID: uuid.New(), AssetVersionID: uuid.New(), Variant: "preview1600", Operation: "media.preview", Inputs: workingInputs(master.Artifact, image)}
		task, err := PrepareMediaRetryTask(PublishedMediaRetry{Document: doc, Params: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("d", 64)}}, master)
		if err != nil {
			t.Fatal(err)
		}
		var profile struct {
			Resources ResourceEstimate `json:"resource_estimate"`
		}
		if err = json.Unmarshal(task.ExecutionProfile, &profile); err != nil {
			t.Fatal(err)
		}
		// Independent small-canvas worker formula: source transfer + target buffers +128MiB.
		sourceBytes := int64(64 * 32 * 24)
		if alpha {
			sourceBytes = 64 * 32 * 96
		}
		required := sourceBytes + 64*32*96 + 128*1024*1024
		minimumScratch := int64(64*1024*1024 + 64*32*12)
		if alpha {
			minimumScratch += 64 * 32 * 4
		}
		if profile.Resources.ScratchBytes < minimumScratch {
			t.Fatal("scratch omits downloaded source arrays")
		}
		if profile.Resources.RAMBytes < required {
			t.Fatal("profile cannot pass worker memory admission", profile.Resources.RAMBytes, required)
		}
	}
}

func TestShownCopyMediaPlanKeepsNewPixelsAndRetainedAlphaThroughSeal(t *testing.T) {
	root := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	copied := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	retainedAlpha := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	image := review.WorkingImage{Pixels: copied, Alpha: &retainedAlpha, DType: "float32", Canvas: review.Canvas{Width: 120, Height: 80}, Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", Encoding: "gamma24", ICCSHA256: strings.Repeat("d", 64)}}
	master := MediaMaster{Artifact: root, Bytes: 500, Working: &image}
	event, version := uuid.New(), uuid.New()
	ports := &mediaRetryPorts{lease: &MediaRetryLease{RendererPolicy:"declared_encoding_alpha8_icc_v2",EventID: event, Token: uuid.New(), TenantID: uuid.New(), AssetVersionID: version, Variants: []string{"thumbnail", "preview"}, Master: master}}
	done, err := (MediaRetryCoordinator{Store: ports, Publisher: ports, SRGBSHA: strings.Repeat("e", 64)}).Process(context.Background())
	if err != nil || !done || ports.sealed != 1 || len(ports.tasks) != 4 {
		t.Fatalf("done=%v seal=%d tasks=%d error=%v", done, ports.sealed, len(ports.tasks), err)
	}
	for _, task := range ports.tasks {
		if len(task.Inputs) != 3 || task.Inputs[0] != root || task.Inputs[1] != copied || task.Inputs[2] != retainedAlpha {
			t.Fatalf("changed shown source: %v", task.Inputs)
		}
	}
	docs, err := BuildMediaRetryDocuments(event, version, master, []string{"preview"}, strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 1, 2} {
		altered := docs[0]
		altered.Inputs = append([]review.ArtifactRef(nil), altered.Inputs...)
		altered.Inputs[index].ID = uuid.New()
		if _, err := PrepareMediaRetryTask(PublishedMediaRetry{Document: altered, Params: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("f", 64)}}, master); err == nil {
			t.Fatalf("accepted changed input %d", index)
		}
	}
}

func TestMediaRetryFrozenPolicyKeepsLegacyParamsAndNewCacheIsSeparate(t *testing.T){
 event,version:=uuid.New(),uuid.New()
 master:=MediaMaster{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},Bytes:100,Width:32,Height:24,MediaType:"image/jpeg"}
 srgb:=strings.Repeat("b",64)
 old,err:=BuildMediaRetryDocumentsForPolicy(event,version,master,[]string{"thumbnail"},srgb,"legacy_v1");if err!=nil{t.Fatal(err)}
 fresh,err:=BuildMediaRetryDocuments(event,version,master,[]string{"thumbnail"},srgb);if err!=nil{t.Fatal(err)}
 again,err:=BuildMediaRetryDocumentsForPolicy(event,version,master,[]string{"thumbnail"},srgb,"legacy_v1");if err!=nil{t.Fatal(err)}
 for i,n:=range old{
  expected,_:=media.LegacyProfileHash(n.Variant,srgb)
  if n.ProfileHash!=expected||n.ID!=fresh[i].ID||string(n.Params)!=string(again[i].Params)||n.ProfileHash==fresh[i].ProfileHash{t.Fatal("policy changed frozen identity or reused cache",i)}
 }
}
func TestMediaRetryUnknownPolicyRefusesPlanBeforeBegin(t *testing.T){
 for _,policy:=range []string{"","future_policy"}{
  p:=&mediaRetryPorts{lease:&MediaRetryLease{RendererPolicy:policy,EventID:uuid.New(),Token:uuid.New(),TenantID:uuid.New(),AssetVersionID:uuid.New()}}
  done,err:=(MediaRetryCoordinator{Store:p,Publisher:p,SRGBSHA:strings.Repeat("a",64)}).Process(context.Background())
  if err==nil||done||p.begun!=0||len(p.registered)!=0||p.released!=1{t.Fatal("unknown policy wrote plan",policy,p)}
 }
}

func TestMediaRetryInterruptedLegacyCompilationResumesExactPlan(t *testing.T){
 master:=MediaMaster{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},Bytes:100,Width:32,Height:24,MediaType:"image/jpeg"}
 p:=&mediaRetryPorts{failPublishAt:2,lease:&MediaRetryLease{RendererPolicy:media.LegacyRendererPolicy,EventID:uuid.New(),Token:uuid.New(),TenantID:uuid.New(),AssetVersionID:uuid.New(),Variants:[]string{"thumbnail"},Master:master}}
 coordinator:=MediaRetryCoordinator{Store:p,Publisher:p,SRGBSHA:strings.Repeat("b",64)}
 done,err:=coordinator.Process(context.Background())
 if err==nil||done||len(p.tasks)!=1||p.sealed!=0||p.released!=1{t.Fatal("did not interrupt after first materialized node",done,err,p)}
 frozenTask:=p.tasks[0]
 if p.releaseDeadline.IsZero()||time.Until(p.releaseDeadline)>5*time.Second{t.Fatal("cleanup lacks bounded deadline")}
 done,err=coordinator.Process(context.Background())
 if err!=nil||!done||p.sealed!=1||len(p.tasks)!=3{t.Fatal("legacy recovery failed",done,err,p)}
 if !bytes.Equal(p.configs[0],p.configs[1])||!bytes.Equal(p.published[0],p.published[2])||!bytes.Equal(p.published[1],p.published[3]){t.Fatal("immutable bytes changed on recovery")}
 if p.tasks[1].ID!=frozenTask.ID||p.tasks[1].Params!=frozenTask.Params||p.tasks[1].InputDigest!=frozenTask.InputDigest{t.Fatal("recovery replaced frozen first task")}
 for _,doc:=range p.documents{
  expected,_:=media.LegacyProfileHash(doc.Variant,coordinator.SRGBSHA)
  if doc.ProfileHash!=expected{t.Fatal("legacy run compiled current profile")}
 }
}

func TestMediaRetryCancellationAfterEveryBlockingPhase(t *testing.T){
 for _,stage:=range []string{"begin","register","publish","materialize","seal"}{t.Run(stage,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  master:=MediaMaster{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},Bytes:100,Width:32,Height:24,MediaType:"image/jpeg"}
  p:=&mediaRetryPorts{lease:&MediaRetryLease{RendererPolicy:media.CurrentRendererPolicy,EventID:uuid.New(),Token:uuid.New(),TenantID:uuid.New(),AssetVersionID:uuid.New(),Variants:[]string{"thumbnail"},Master:master},afterStage:func(current string){if current==stage{cancel()}}}
  done,err:=(MediaRetryCoordinator{Store:p,Publisher:p,SRGBSHA:strings.Repeat("b",64)}).Process(ctx)
  if done||!errors.Is(err,context.Canceled){t.Fatal("late success adopted",done,err)}
  if stage=="begin"&&len(p.registered)!=0||stage=="register"&&len(p.published)!=0||stage=="publish"&&len(p.tasks)!=0||stage=="materialize"&&(len(p.tasks)!=1||p.sealed!=0){t.Fatal("cancel advanced durable phase",stage,p)}
  if stage=="seal"{if p.sealed!=1||p.released!=0{t.Fatal("completed seal incorrectly released",p)}}else if p.released!=1||p.releaseDeadline.IsZero(){t.Fatal("cancelled compilation not released",p)}
 })}
}
