package httpapi

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/collection"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/exportjob"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/style"
	"photoeditor/backend/internal/styleupload"
	"photoeditor/backend/internal/tenant"
	"testing"
	"time"
)

// Actual OTP/session auth, restricted logins, HTTP handlers and versioned S3 upload.
func TestIntegrationStyleImportClosedRunKindAfterGenuinePackageUpload(t *testing.T) {
	if os.Getenv("PHOTO_STYLE_KIND_OWNER_DSN") == "" {
		t.Skip("isolated style-kind DB required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	check := func(stage string, e error) {
		t.Helper()
		if e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) {
				t.Fatalf("%s SQLSTATE=%s constraint=%s", stage, pg.Code, pg.ConstraintName)
			}
			t.Fatalf("%s error_class=%T", stage, e)
		}
	}
	owner, e := pgxpool.New(ctx, os.Getenv("PHOTO_STYLE_KIND_OWNER_DSN"))
	check("owner", e)
	defer owner.Close()
	var name string
	check("database", owner.QueryRow(ctx, "select current_database()").Scan(&name))
	if name != "photoeditor_style_kind_r1" {
		t.Fatal("isolated DB refused")
	}
	app := func(key, role string) *pgxpool.Pool {
		c, e := pgxpool.ParseConfig(os.Getenv(key))
		check("app-config", e)
		c.ConnConfig.RuntimeParams["role"] = role
		c.MaxConns = 3
		database.GuardApplicationRole(c)
		p, e := pgxpool.NewWithConfig(ctx, c)
		check("app-pool", e)
		check("app-role", database.RequireApplicationRole(ctx, p, role))
		var restricted bool
		check("app-login", p.QueryRow(ctx, "select not rolsuper and not rolbypassrls and not rolcreatedb and not rolcreaterole from pg_roles where rolname=session_user").Scan(&restricted))
		if !restricted {
			t.Fatal("privileged app login")
		}
		t.Cleanup(p.Close)
		return p
	}
	api, adb := app("PHOTO_STYLE_KIND_API_DSN", "photo_api"), app("PHOTO_STYLE_KIND_AUTH_DSN", "photo_auth")
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	check("key", e)
	key := make([]byte, 32)
	_, e = rand.Read(key)
	check("mail-key", e)
	ring := auth.KeyRing{Active: "isolated", Keys: map[string][]byte{"isolated": key}}
	a := &auth.Service{DB: adb, Pepper: key, MailKeys: ring, Signer: auth.TokenSigner{Active: "isolated", Private: priv, Public: map[string]ed25519.PublicKey{"isolated": pub}, Issuer: "isolated", Audience: "api"}, Registration: true}
	challenge, e := a.RequestOTP(ctx, uuid.NewString()+"@example.test", "en", uuid.NewString())
	check("request-otp", e)
	var mid uuid.UUID
	var kid string
	var cipher []byte
	check("encrypted-mail", adb.QueryRow(ctx, "select id,key_id,payload_cipher from photo.mail_payloads where challenge_id=$1", challenge.ID).Scan(&mid, &kid, &cipher))
	plain, e := ring.Open(kid, cipher, []byte(mid.String()+":payload"))
	check("mail-delivery", e)
	var mail map[string]string
	check("mail-json", json.Unmarshal(plain, &mail))
	token, e := a.VerifyOTP(ctx, challenge.ID, mail["code"], "native")
	check("verify-otp", e)
	who, e := a.AuthenticateAccess(ctx, token.Access)
	check("authenticate", e)
	ten, e := (&tenant.Service{DB: api, MailKeys: ring}).Create(ctx, who, uuid.NewString(), tenant.CreateRequest{Name: "Actual style-kind regression"})
	check("tenant", e)
	p, e := (&project.Service{DB: api}).Create(ctx, who, ten.ID, uuid.NewString(), project.Create{Name: "Upload import", Visibility: "workspace", DownloadAllowed: true})
	check("project", e)
	if p.ResourceID == nil {
		t.Fatal("missing project")
	}
	pid := *p.ResourceID
	bucket := "photo-style-kind-" + uuid.NewString()
	store, e := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), PublicEndpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	check("store", e)
	check("bootstrap", store.Bootstrap(ctx))
	handler := (&Server{DB: api, Auth: a, Media: &media.Service{DB: api, Objects: store, Bucket: bucket}, Config: config.Config{MaxJSONBytes: 1 << 20, Environment: "development"}}).Routes()
	call := func(path string, body any, key string, status int) []byte {
		t.Helper()
		b, e := json.Marshal(body)
		check("body", e)
		r := httptest.NewRequest("POST", path, bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+token.Access)
		r.Header.Set("X-Photo-Client", "native")
		r.Header.Set("X-CSRF-Token", token.CSRF)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("HTTP wanted=%d actual=%d", status, w.Code)
		}
		return w.Body.Bytes()
	}
	raw, e := os.ReadFile(os.Getenv("PHOTO_STYLE_KIND_PACKAGE"))
	check("package", e)
	sum := sha256.Sum256(raw)
	root := "/api/v1/tenants/" + ten.ID.String()
	uploads := root + "/projects/" + pid.String() + "/style-package-uploads"
	var upload styleupload.Session
	check("begin", json.Unmarshal(call(uploads, styleupload.BeginRequest{Filename: "style.json", Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:])}, uuid.NewString(), 200), &upload))
	var parts struct {
		Parts []objectstore.PartURL `json:"parts"`
	}
	check("parts", json.Unmarshal(call(uploads+"/"+upload.ID.String()+"/parts", map[string]any{"part_numbers": []int{1}}, "", 200), &parts))
	if len(parts.Parts) != 1 {
		t.Fatal("part inventory")
	}
	req, e := http.NewRequestWithContext(ctx, "PUT", parts.Parts[0].URL, bytes.NewReader(raw))
	check("put", e)
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(req)
	check("S3 PUT", e)
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode/100 != 2 || response.Header.Get("ETag") == "" {
		t.Fatal("part not stored")
	}
	var ready styleupload.Session
	check("complete", json.Unmarshal(call(uploads+"/"+upload.ID.String()+"/complete", styleupload.CompleteRequest{ExpectedSize: int64(len(raw)), Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: response.Header.Get("ETag")}}}, uuid.NewString(), 200), &ready))
	if ready.State != "ready" || ready.Artifact == nil {
		t.Fatal("package not genuinely ready")
	}
	input := style.ImportRequest{ProjectID: pid, Name: "Manual kind regression", UploadArtifactID: *ready.Artifact}
	importKey := uuid.NewString()
	call(root+"/styles/import", input, importKey, 500)
	_, e = (style.Service{DB: api, Bucket: bucket}).Import(ctx, who, ten.ID, importKey, input)
	var pg *pgconn.PgError
	if !errors.As(e, &pg) || pg.Code != "23514" || pg.ConstraintName != "runs_domain_0" {
		t.Fatal("baseline mismatch")
	}
	var count int
	check("rollback", owner.QueryRow(ctx, "select count(*) from photo.style_import_builds where tenant_id=$1", ten.ID).Scan(&count))
	if count != 0 {
		t.Fatal("partial baseline")
	}

	// Independent image fixture stores real encoded bytes; no image processing or
	// quality claim is inferred from this admission-only regression.
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{R: 180, G: 70, B: 40, A: 255})
	var encoded bytes.Buffer
	check("png", png.Encode(&encoded, img))
	imageSHA := sha256.Sum256(encoded.Bytes())
	imageDigest := hex.EncodeToString(imageSHA[:])
	object, e := store.PutImmutable(ctx, "fixtures/"+uuid.NewString()+".png", bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), imageDigest, "image/png")
	check("real-image", e)
	var artifact, asset, version uuid.UUID
	check("image-artifact", owner.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES($1,$2,$3,$4,$5,$6,'image/png','working_image') RETURNING id`, ten.ID, bucket, object.Key, object.VersionID, imageDigest, object.Bytes).Scan(&artifact))
	check("image-asset", owner.QueryRow(ctx, `INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, ten.ID, pid, artifact).Scan(&asset))
	check("image-version", owner.QueryRow(ctx, `INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`, ten.ID, asset, artifact, imageDigest).Scan(&version))
	cs := collection.Service{DB: api}
	created, e := cs.Create(ctx, who, ten.ID, uuid.NewString(), collection.Create{ProjectID: pid, Name: "Actual export selection"})
	check("collection-create", e)
	var col collection.CollectionCreated
	check("collection-receipt", json.Unmarshal(created.Body, &col))
	var item uuid.UUID
	check("collection-item", owner.QueryRow(ctx, `INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES($1,$2,$3,$4,0) RETURNING id`, ten.ID, col.Collection.ID, asset, version).Scan(&item))
	var revision string
	check("collection-revision", owner.QueryRow(ctx, "select revision::text from photo.collections where tenant_id=$1 and id=$2", ten.ID, col.Collection.ID).Scan(&revision))
	selection, e := cs.CreateSelection(ctx, who, ten.ID, col.Collection.ID, uuid.NewString(), collection.SelectionRequest{CollectionRevision: revision, Scope: "selected", ItemIDs: []uuid.UUID{item}})
	check("actual-selection", e)
	var selected collection.SnapshotRef
	check("selection-receipt", json.Unmarshal(selection.Body, &selected))
	quality := 95
	preset, e := (exportpreset.Service{DB: api}).Create(ctx, who, ten.ID, uuid.NewString(), exportpreset.Create{Name: "Exact JPEG", Config: exportpreset.Config{Format: "jpeg", ICCID: "srgb_v1", Quality: &quality, Subsampling: "444", Alpha: "matte", MatteRGB: []float64{1, 1, 1}, MetadataPolicy: "strip", NamingTemplate: "{original_name}"}})
	check("preset", e)
	var presetValue exportpreset.Preset
	check("preset-receipt", json.Unmarshal(preset.Body, &presetValue))
	exportRequest := exportjob.CreateRequest{SnapshotID: selected.ID, Presets: []uuid.UUID{presetValue.ID}}
	exportKey := uuid.NewString()
	call(root+"/exports", exportRequest, exportKey, 500)
	_, e = (exportjob.Service{DB: api}).Create(ctx, who, ten.ID, exportKey, exportRequest)
	if !errors.As(e, &pg) || pg.Code != "23514" || pg.ConstraintName != "runs_domain_0" {
		t.Fatal("export baseline mismatch")
	}

	const unchangedMetadata = `SELECT jsonb_build_object('constraints',(SELECT jsonb_agg(jsonb_build_array(conname,pg_get_constraintdef(oid),convalidated) ORDER BY conname) FROM pg_constraint WHERE conrelid='photo.runs'::regclass AND conname<>'runs_domain_0'),'table',(SELECT jsonb_build_array(relrowsecurity,relforcerowsecurity,relacl) FROM pg_class WHERE oid='photo.runs'::regclass),'policies',(SELECT jsonb_agg(to_jsonb(p) ORDER BY policyname) FROM pg_policies p WHERE schemaname='photo' AND tablename='runs'))::text`
	var priorMetadata string
	check("metadata-before", owner.QueryRow(ctx, unchangedMetadata).Scan(&priorMetadata))
	migration, e := os.ReadFile(os.Getenv("PHOTO_STYLE_KIND_MIGRATION"))
	check("migration", e)
	_, e = owner.Exec(ctx, string(bytes.Split(migration, []byte("-- +goose Down"))[0]))
	check("candidate-up", e)
	var afterMetadata string
	check("metadata-after", owner.QueryRow(ctx, unchangedMetadata).Scan(&afterMetadata))
	if afterMetadata != priorMetadata {
		t.Fatal("unrelated constraints/ACL/RLS changed")
	}

	exportAccepted := call(root+"/exports", exportRequest, exportKey, 202)
	if !bytes.Equal(exportAccepted, call(root+"/exports", exportRequest, exportKey, 202)) {
		t.Fatal("export replay changed")
	}
	var exportResult exportjob.RunAccepted
	check("export-receipt", json.Unmarshal(exportAccepted, &exportResult))
	var exportKind string
	check("export-kind", owner.QueryRow(ctx, "select kind from photo.runs where tenant_id=$1 and id=$2", ten.ID, exportResult.RunID).Scan(&exportKind))
	if exportKind != "export_batch" {
		t.Fatal("wrong export kind")
	}
	accepted := call(root+"/styles/import", input, importKey, 202)
	if !bytes.Equal(accepted, call(root+"/styles/import", input, importKey, 202)) {
		t.Fatal("replay changed")
	}
	var result struct {
		RunID uuid.UUID `json:"run_id"`
	}
	check("accepted", json.Unmarshal(accepted, &result))
	var kind, state string
	check("run", owner.QueryRow(ctx, "select kind,status from photo.runs where tenant_id=$1 and id=$2", ten.ID, result.RunID).Scan(&kind, &state))
	if kind != "style_import" || state != "queued" {
		t.Fatal("wrong admission")
	}
	check("one-build", owner.QueryRow(ctx, "select count(*) from photo.style_import_builds where tenant_id=$1", ten.ID).Scan(&count))
	if count != 1 {
		t.Fatal("duplicate")
	}
	tx, e := owner.Begin(ctx)
	check("negative", e)
	_, e = tx.Exec(ctx, "insert into photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) select tenant_id,'style_import_typo','queued',input_snapshot_id,config_snapshot,config_hash,created_by from photo.runs where id=$1", result.RunID)
	if !errors.As(e, &pg) || pg.Code != "23514" || pg.ConstraintName != "runs_domain_0" {
		t.Fatal("unknown kind admitted")
	}
	tx.Rollback(ctx)
	var preserved bool
	check("v2-invariant", owner.QueryRow(ctx, "select exists(select from pg_constraint where conrelid='photo.runs'::regclass and conname='runs_v2_fail_fast' and convalidated)").Scan(&preserved))
	if !preserved {
		t.Fatal("v2 invariant changed")
	}
}
