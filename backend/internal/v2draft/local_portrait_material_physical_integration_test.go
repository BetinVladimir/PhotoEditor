package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"strings"
	"testing"
	"time"
)

// This exercises material assembly only. Request compilation/admission and pixel
// raster replay remain separate gates; the fixture seeds a finished producer.
func TestIntegrationLocalPortraitMaterialPhysical(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_MANUAL_DOCUMENTS")
	if raw == "" {
		t.Skip("isolated physical fixture required")
	}
	var f struct {
		Tenant, Project, Snapshot, Actor, Session                  uuid.UUID
		Binding                                                    v2input.SessionVersionBinding
		Target, Guard, TargetPixel, GuardPixel, RootRef, ParamsRef v2input.Ref
		VectorRef                                                  *v2input.Ref
		Source, Root, Params, Failure, Vectors                     string
		Objects                                                    objectstore.Config
	}
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := func(role string) *pgxpool.Pool {
		t.Helper()
		cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal(err)
		}
		if role != "" {
			cfg.ConnConfig.RuntimeParams["role"] = role
		}
		db, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(db.Close)
		return db
	}
	api, scheduler := pool("photo_api"), pool("photo_scheduler")
	observer := pool("")
	store, err := objectstore.New(f.Objects)
	if err != nil {
		t.Fatal(err)
	}
	loader := SQLLocalPortraitMaterialLoader{DB: scheduler, Bucket: f.Objects.Bucket, Resolver: ClosureResolver{Catalog: SQLArtifactCatalog{DB: api, Bucket: f.Objects.Bucket, ProjectID: f.Project, AssetVersionID: f.Binding.AssetVersionID}, Objects: store}}
	params, err := canonical.Marshal(map[string]any{"mask": f.Target, "protected": f.Guard})
	if err != nil {
		t.Fatal(err)
	}
	request := LocalPortraitOwnerRequest{SchemaVersion: 1, Tenant: f.Tenant, Project: f.Project, Snapshot: f.Snapshot, Actor: f.Actor, Session: f.Session, ManualRoot: f.RootRef, OriginalParams: f.ParamsRef, TargetPixels: f.TargetPixel, GuardPixels: f.GuardPixel, Vectors: f.VectorRef, Intent: LocalPortraitIntent{Binding: f.Binding, ParamsJCS: string(params)}}
	requestRaw, err := canonical.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	got, err := loader.Load(ctx, who, f.Tenant, requestRaw)
	if f.Failure != "" {
		if err == nil || len(got.Source) != 0 || len(got.Proof.RootRaw) != 0 {
			t.Fatal("unsafe material returned", f.Failure)
		}
		t.Log("LOCAL_PORTRAIT_MATERIAL_REFUSED", f.Failure)
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Source) != f.Source || string(got.Proof.RootRaw) != f.Root || string(got.Proof.OriginalRaw) != f.Params || string(got.Proof.VectorsRaw) != f.Vectors || got.Proof.Root != f.RootRef || got.Proof.OriginalParams != f.ParamsRef {
		t.Fatal("material pins/bytes changed")
	}
	// Request pins must agree with independently authorized original producer.
	for _, mode := range []string{"root", "params", "vectors", "noncanonical", "actor"} {
		bad := request
		identity := who
		switch mode {
		case "root":
			bad.ManualRoot.ArtifactID = uuid.New()
		case "params":
			bad.OriginalParams.ArtifactID = uuid.New()
		case "vectors":
			ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: f.Target.SHA256}
			bad.Vectors = &ref
		case "actor":
			identity.UserID = uuid.New()
		}
		body, e := canonical.Marshal(bad)
		if e != nil {
			t.Fatal(e)
		}
		if mode == "noncanonical" {
			body = append([]byte(" "), body...)
		}
		refused, e := loader.Load(ctx, identity, f.Tenant, body)
		if e == nil || len(refused.Source) != 0 || len(refused.Proof.RootRaw) != 0 {
			t.Fatal("request substitution accepted", mode)
		}
	}
	loader.Resolver.Catalog = tracedPortraitCatalog{inner: loader.Resolver.Catalog, t: t}
	image, e := review.ParseWorkingImage(got.Source)
	if e != nil {
		t.Fatal(e)
	}
	capacity, e := v2portrait.LocalPortraitResources(image.Canvas.Width, image.Canvas.Height)
	if e != nil {
		t.Fatal(e)
	}
	for _, operation := range []string{"portrait.hair", "portrait.clothing"} {
		var body []byte
		if operation == "portrait.hair" {
			body, _, e = (v2portrait.HairParams{SchemaVersion: 1, Base: f.Binding, Mask: f.Target, Protected: f.Guard, Strength: 1, SaturationFactor: 1, Detail: .5}).Compile()
		} else {
			body, _, e = (v2portrait.ClothingParams{SchemaVersion: 1, Base: f.Binding, Mask: f.Target, Protected: f.Guard, Strength: 1, SaturationFactor: 1, WrinkleStrength: .5}).Compile()
		}
		if e != nil {
			t.Fatal(e)
		}
		compiled, _, e := loader.Resolver.CompileLocalPortraitOwnerRequest(ctx, who, f.Tenant, f.Project, f.Snapshot, uuid.New(), 0, operation, f.Binding, got.Source, body, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", capacity, got.Proof)
		if e != nil || len(compiled) == 0 {
			t.Fatal("compile physical material", operation, e)
		}
		publisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: f.Objects.Bucket}, Objects: store}
		service, e := NewSQLLocalPortraitRecoveryService(scheduler, api, f.Objects.Bucket, store, publisher, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", capacity)
		if e != nil {
			t.Fatal(e)
		}
		factory := service.Factory
		service.Factory = localPortraitFactoryFunc(func(ctx context.Context, r LocalPortraitOwnerRequest) (LocalPortraitMaterialLoader, LocalPortraitCoordinator, error) {
			m, c, e := factory.Build(ctx, r)
			c.Resolver.Catalog = tracedPortraitCatalog{inner: c.Resolver.Catalog, t: t}
			return m, c, e
		})
		var owner LocalPortraitOwnerRequest
		if e = json.Unmarshal(compiled, &owner); e != nil {
			t.Fatal(e)
		}
		if e = (SQLLocalPortraitOwnerScope{DB: scheduler}).Check(ctx, compiled); e != nil {
			t.Fatal("owner preflight", e)
		}

		admitted, e := service.Submit(ctx, who, f.Tenant, compiled, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", owner.Intent.ReservedBytes+65536+2097152)
		if e == nil || !strings.Contains(e.Error(), "graph not sealed") || admitted != (LocalPortraitAdmission{}) {
			t.Fatal("unqualified graph must refuse admission", operation, e)
		}
		var qualified bool
		if e = scheduler.QueryRow(ctx, "SELECT photo.v28_local_runtime_qualification_ready($1,$2,$3,$4)", f.Tenant, f.Project, operation, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&qualified); e != nil || qualified {
			t.Fatal("fixture unexpectedly qualified", e)
		}
		snapshot := func() string {
			t.Helper()
			var wire string
			err := observer.QueryRow(ctx, `SELECT jsonb_build_object(
              'usage',(SELECT jsonb_build_array(storage_bytes,reserved_bytes) FROM photo.tenant_usage WHERE tenant_id=$1),
              'documents',(SELECT jsonb_agg(jsonb_build_array(artifact_id,sha256,bytes,object_version,state) ORDER BY purpose) FROM photo.backend_documents WHERE tenant_id=$1 AND run_id=$2),
              'quota',(SELECT jsonb_agg(jsonb_build_array(bytes,state) ORDER BY id) FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2),
              'tasks',(SELECT jsonb_agg(jsonb_build_array(id,state,attempt,fencing_token) ORDER BY id) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2),
              'plan',(SELECT v28_local_task_plan FROM photo.runs WHERE tenant_id=$1 AND id=$2))::text`, f.Tenant, owner.Run).Scan(&wire)
			if err != nil {
				t.Fatal(err)
			}
			return wire
		}
		before := snapshot()
		for retry := 0; retry < 2; retry++ {
			resumed, err := service.Resume(ctx, who, f.Tenant, owner.Run)
			if err == nil || !strings.Contains(err.Error(), "graph not sealed") || resumed != (LocalPortraitAdmission{}) {
				t.Fatal("resume unqualified owner", operation, err)
			}
			if snapshot() != before {
				t.Fatal("resume changed durable documents/quota/task/plan", operation)
			}
		}
		for _, mode := range []string{"tenant", "actor", "session"} {
			identity, tenant := who, f.Tenant
			switch mode {
			case "tenant":
				tenant = uuid.New()
			case "actor":
				identity.UserID = uuid.New()
			case "session":
				identity.SessionID = uuid.New()
			}
			denied, err := service.Resume(ctx, identity, tenant, owner.Run)
			if err == nil || denied != (LocalPortraitAdmission{}) {
				t.Fatal("foreign resume accepted", mode)
			}
			if snapshot() != before {
				t.Fatal("foreign resume changed durable state", mode)
			}
		}
		t.Log("LOCAL_PORTRAIT_RESUME_STABLE", operation)
		t.Log("LOCAL_PORTRAIT_STAGED_UNQUALIFIED", operation, owner.Run, owner.Intent.TaskID)
	}
	t.Log("LOCAL_PORTRAIT_MATERIAL_PHYSICAL_OK")
}

type tracedPortraitCatalog struct {
	inner ArtifactCatalog
	t     *testing.T
}

func (c tracedPortraitCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (objectstore.Object, error) {
	o, e := c.inner.Lookup(ctx, who, tenant, node)
	if e != nil {
		c.t.Log("catalog denied", node.Path, node.Contract, e)
	}
	return o, e
}
