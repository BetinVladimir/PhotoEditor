package workflow

import (
	"bytes"
	"context"
	"io"
 "errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type downloadRows struct {
	pgx.Rows
	values [][]any
	index  int
	closed bool
}

func (r *downloadRows) Next() bool { return r.index < len(r.values) }
func (r *downloadRows) Scan(dest ...any) error {
	for index, value := range r.values[r.index] {
		reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(value))
	}
	r.index++
	return nil
}
func (r *downloadRows) Close()     { r.closed = true }
func (r *downloadRows) Err() error { return nil }

type downloadLoaderDB struct {
	contextRows, inputRows *downloadRows
 boundRows *downloadRows
	calls                  int
}

func (d *downloadLoaderDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	d.calls++
	if d.calls == 1 {
		return d.contextRows, nil
	}
	if d.calls==2{return d.inputRows,nil};if d.boundRows!=nil{return d.boundRows,nil};return &downloadRows{},nil
}

type downloadObjects struct{ body map[string][]byte }

func (o downloadObjects) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o.body[object.Key])), nil
}

func downloadLoaderFixture(t *testing.T) (DownloadExportLoader, uuid.UUID, uuid.UUID, *downloadLoaderDB) {
	t.Helper()
	tenant, run, job, snapshot, version := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	masterID, pixelID := uuid.New(), uuid.New()
	workSHA, targetSHA := downloadDigest("work"), downloadDigest("target")
	sourceSHA := downloadDigest("source")
 pixelBody:=deliveryTestRGBNPY(8,4)
 pixelSHA:=downloadDigest(string(pixelBody))
	chain, err := canonical.Marshal(struct {
		Steps []any `json:"steps"`
	}{Steps: []any{}})
	if err != nil {
		t.Fatal(err)
	}
	chainHash, _ := canonical.Hash(struct {
		Steps []any `json:"steps"`
	}{Steps: []any{}})
	image := review.WorkingImage{
		Pixels: review.ArtifactRef{ID:pixelID,SHA256:pixelSHA}, Canvas: review.Canvas{Width: 8, Height: 4, Orientation: 1, SourceSHA256: sourceSHA, SpaceID: "oriented:" + sourceSHA},
		Color: review.ColorSpace{ProfileID: "work_rec2020_gamma24_v1", ICCSHA256: workSHA, Encoding: "gamma24"},
		DType: "float32", TransformChain: append([]byte(nil), chain...),
	}
	// ParseWorkingImage requires the transform-chain hash inside the object.
	image.TransformChain, _ = canonical.Marshal(struct {
		Steps []any  `json:"steps"`
		Hash  string `json:"hash"`
	}{[]any{}, chainHash})
	raw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	masterSHA := downloadDigest(string(raw))
	quality := 95
	preset := DownloadPreset{Format: "jpeg", ICCID: "srgb_v1", Quality: &quality, Subsampling: "444", Alpha: "matte", MatteRGB: []float64{1, 1, 1}, MetadataPolicy: "strip"}
	presetHash, _ := canonical.Hash(preset)
	envelope, _ := canonical.Marshal(downloadPresetEnvelope{ID: uuid.New(), Revision: 1, Hash: presetHash, Config: preset})
	contextRows := &downloadRows{values: [][]any{{job, snapshot, downloadDigest("snapshot"), int64(1), envelope, int64(0), version,
		masterID, masterSHA, "photos", "master", "v1", int64(len(raw)), "application/json"}}}
	inputRows := &downloadRows{values: [][]any{{int64(0), "working_rgb", pixelID, pixelSHA, int64(len(pixelBody)), "photos", "pixels", "v2", "application/octet-stream"}}}
	db := &downloadLoaderDB{contextRows: contextRows, inputRows: inputRows}
	return DownloadExportLoader{DB: db, Objects: downloadObjects{map[string][]byte{"master": raw,"pixels":pixelBody}}, Bucket: "photos",
		WorkICCSHA256: workSHA, TargetICCs: map[string]string{"srgb_v1": targetSHA}}, tenant, run, db
}

func pixelRef(id uuid.UUID) review.ArtifactRef {
	return review.ArtifactRef{ID: id, SHA256: downloadDigest(id.String())}
}

func TestDownloadExportLoaderBindsCanonicalNestedOutputs(t *testing.T) {
	loader, tenant, run, db := downloadLoaderFixture(t)
	batch, err := loader.Load(context.Background(), tenant, run)
	if err != nil || len(batch.Spec.Items) != 1 || len(batch.Spec.Items[0].Image.Pixels.SHA256) != 64 {
		t.Fatal("valid download export context rejected", batch, err)
	}
	if !db.contextRows.closed || !db.inputRows.closed {
		t.Fatal("SQL cursors remained open over object IO")
	}
}

func TestDownloadExportLoaderRejectsManifestRefNotAttestedByProducer(t *testing.T) {
	loader, tenant, run, db := downloadLoaderFixture(t)
	db.inputRows.values[0][2] = uuid.New()
	if _, err := loader.Load(context.Background(), tenant, run); err == nil {
		t.Fatal("rebound pixel artifact accepted")
	}
}

func TestDownloadRenderDeclaredRGBUsesExactCurrentAuthorityAndPhysicalBytes(t *testing.T){
 for _,mode:=range []string{"valid","missing_authority","corrupt_physical","wrong_ref","wrong_media","wrong_version"}{t.Run(mode,func(t *testing.T){
  loader,tenant,run,db:=downloadLoaderFixture(t)
  row:=append([]any(nil),db.inputRows.values[0]...)
  db.inputRows.values=nil
  row[8]="application/x-npy"
  db.boundRows=&downloadRows{values:[][]any{row}}
  switch mode{
  case "missing_authority":db.boundRows.values=nil
  case "wrong_ref":row[2]=uuid.New()
  case "wrong_media":row[8]="image/png"
  case "wrong_version":row[7]="null"
  case "corrupt_physical":objects:=loader.Objects.(downloadObjects);objects.body["pixels"][len(objects.body["pixels"])-1]^=1
  }
  got,err:=loader.Load(context.Background(),tenant,run)
  if mode=="valid"{if err!=nil||len(got.Spec.Items)!=1||!db.boundRows.closed{t.Fatal(got,err)}}else if err==nil{t.Fatal("accepted",mode)}
 })}
}

// Real objectstore verifiedReader reports integrity failures from Read; it need
// not return corrupt bytes successfully for the loader's manual hash to see.
type downloadFailingMasterObjects struct {
 fallback ArtifactOpener
 readErr,closeErr error
 cancel context.CancelFunc
 closed *bool
}
type downloadFailingMasterReader struct {
 source io.Reader
 readErr,closeErr error
 cancel context.CancelFunc
 closed *bool
}
func(r *downloadFailingMasterReader)Read(p []byte)(int,error){
 if r.cancel!=nil{r.cancel();r.cancel=nil}
 if r.readErr!=nil{return 0,r.readErr}
 return r.source.Read(p)
}
func(r *downloadFailingMasterReader)Close()error{*r.closed=true;return r.closeErr}
func(o downloadFailingMasterObjects)Open(ctx context.Context,object objectstore.Object)(io.ReadCloser,error){
 if object.Key!="master"{return o.fallback.Open(ctx,object)}
 raw:=o.fallback.(downloadObjects).body["master"]
 return &downloadFailingMasterReader{bytes.NewReader(raw),o.readErr,o.closeErr,o.cancel,o.closed},nil
}
func TestDownloadMasterReadAndClosePhysicalFailuresRetainFrozenIdentity(t *testing.T){
 for _,phase:=range []string{"read","close"}{
  for _,cause:=range []error{objectstore.ErrIntegrity,objectstore.ErrMissing}{
   t.Run(phase+cause.Error(),func(t *testing.T){
    loader,tenant,run,db:=downloadLoaderFixture(t);closed:=false
    objects:=downloadFailingMasterObjects{fallback:loader.Objects,closed:&closed}
    if phase=="read"{objects.readErr=cause}else{objects.closeErr=cause};loader.Objects=objects
    _,err:=loader.Load(context.Background(),tenant,run)
    var failure *downloadSourceFailure
    if !errors.As(err,&failure){t.Fatal("physical stream failure lost",err)}
    code:="ARTIFACT_HASH_MISMATCH";if cause==objectstore.ErrMissing{code="ARTIFACT_MISSING"}
    if failure.ordinal!=0||failure.master!=db.contextRows.values[0][7].(uuid.UUID)||failure.sha!=db.contextRows.values[0][8].(string)||failure.code!=code{t.Fatal("frozen failure identity changed",failure)}
    if !closed{t.Fatal("failed source reader not closed")}
   })
  }
 }
}
func TestDownloadMasterStreamTransientAndCancellationNeverBecomePermanent(t *testing.T){
 for _,phase:=range []string{"read","close"}{
  for _,cause:=range []error{io.ErrUnexpectedEOF,context.Canceled,context.DeadlineExceeded}{
   t.Run(phase+cause.Error(),func(t *testing.T){
    loader,tenant,run,_:=downloadLoaderFixture(t);closed:=false
    objects:=downloadFailingMasterObjects{fallback:loader.Objects,closed:&closed}
    if phase=="read"{objects.readErr=cause}else{objects.closeErr=cause};loader.Objects=objects
    _,err:=loader.Load(context.Background(),tenant,run);var failure *downloadSourceFailure
    if !errors.Is(err,cause)||errors.As(err,&failure)||!closed{t.Fatal("transient stream failure became permanent",err)}
   })
  }
 }
 loader,tenant,run,_:=downloadLoaderFixture(t);closed:=false
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 loader.Objects=downloadFailingMasterObjects{fallback:loader.Objects,readErr:objectstore.ErrIntegrity,cancel:cancel,closed:&closed}
 _,err:=loader.Load(ctx,tenant,run);var failure *downloadSourceFailure
 if !errors.Is(err,context.Canceled)||errors.As(err,&failure)||!closed{t.Fatal("cancelled stream terminalized source",err)}
}
