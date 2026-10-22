package run

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/styleupload"
	"strings"
	"testing"
	"time"
)

// Actual named owner consumes an operator-provided original StyleV2 package.
// No ready artifact, accepted candidate or qualified model is inserted by fixture.
func TestIntegrationUploadGenuineStylePackage(t *testing.T) {
	raw, output := os.Getenv("PHOTO_EDITOR_STYLE_PACKAGE"), os.Getenv("PHOTO_EDITOR_STYLE_PACKAGE_OUTPUT")
	if raw == "" && output == "" {
		t.Skip("genuine Style package upload fixture not configured")
	}
	if raw == "" || output == "" {
		t.Fatal("partial genuine Style package fixture")
	}
	var f struct {
		Tenant, Project, Actor, Session     uuid.UUID
		Path, SHA256, BeginKey, CompleteKey string
		Objects                             objectstore.Config
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || f.Tenant == uuid.Nil || f.Project == uuid.Nil || f.Actor == uuid.Nil || f.Session == uuid.Nil || !filepath.IsAbs(f.Path) || f.BeginKey == "" || f.CompleteKey == "" {
		t.Fatal("closed real package fixture required")
	}
	info, e := os.Lstat(f.Path)
	if e != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > styleupload.MaxBytes {
		t.Fatal("bounded original regular package required")
	}
	file, e := os.Open(f.Path)
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	opened, e := file.Stat()
	if e != nil || !os.SameFile(info, opened) {
		t.Fatal("source changed before open")
	}
	hash := sha256.New()
	n, e := io.Copy(hash, io.LimitReader(file, styleupload.MaxBytes+1))
	if e != nil || n != info.Size() || hex.EncodeToString(hash.Sum(nil)) != f.SHA256 {
		t.Fatal("independent original package SHA mismatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()
	pool := genuineDedicatedPool(t, ctx, "api")
	objects, e := objectstore.New(f.Objects)
	if e != nil {
		t.Fatal("actual owned object configuration")
	}
	service := styleupload.Service{DB: pool, Objects: objects, Bucket: f.Objects.Bucket}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	session, e := service.Begin(ctx, who, f.Tenant, f.Project, f.BeginKey, styleupload.BeginRequest{Filename: filepath.Base(f.Path), Bytes: n, SHA256: f.SHA256})
	if e != nil {
		t.Fatal("actual package Begin", e)
	}
	ready := false
	defer func() {
		if !ready {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer stop()
			if _, e := service.Abort(cleanup, who, f.Tenant, f.Project, session.ID); e != nil {
				t.Errorf("package abort requires owned tenant cleanup: %v", e)
			}
		}
	}()
	numbers := make([]int, session.Parts)
	for i := range numbers {
		numbers[i] = i + 1
	}
	signed, e := service.Sign(ctx, who, f.Tenant, f.Project, session.ID, numbers)
	if e != nil || len(signed) != session.Parts {
		t.Fatal("actual signed part inventory", e)
	}
	endpoint, e := url.Parse(f.Objects.PublicEndpoint)
	if e != nil || endpoint.Host == "" {
		t.Fatal("owned object endpoint required")
	}
	client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	completed := make([]objectstore.UploadedPart, session.Parts)
	seen := map[int]bool{}
	for _, part := range signed {
		u, e := url.Parse(part.URL)
		if e != nil || u.Scheme != endpoint.Scheme || u.Host != endpoint.Host || u.User != nil || u.Fragment != "" || part.PartNumber < 1 || part.PartNumber > session.Parts || seen[part.PartNumber] {
			t.Fatal("signed inventory escaped owned endpoint")
		}
		seen[part.PartNumber] = true
		offset := int64(part.PartNumber-1) * session.PartBytes
		length := min(session.PartBytes, n-offset)
		request, e := http.NewRequestWithContext(ctx, http.MethodPut, part.URL, io.NewSectionReader(file, offset, length))
		if e != nil {
			t.Fatal(e)
		}
		request.ContentLength = length
		response, e := client.Do(request)
		if e != nil {
			t.Fatal("actual signed PUT", e)
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		closeErr := response.Body.Close()
		etag := response.Header.Get("ETag")
		if readErr != nil || closeErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 || etag == "" {
			t.Fatal("actual PUT not confirmed")
		}
		completed[part.PartNumber-1] = objectstore.UploadedPart{PartNumber: part.PartNumber, ETag: etag}
	}
	after, e := file.Stat()
	if e != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("original package changed during upload")
	}
	session, e = service.Complete(ctx, who, f.Tenant, f.Project, session.ID, f.CompleteKey, styleupload.CompleteRequest{Parts: completed, ExpectedSize: n})
	if e != nil || session.State != "ready" || session.Artifact == nil {
		t.Fatal("verified package publication failed", e)
	}
	ready = true
	// Actual completion replay must identify the same ready artifact.
	replay, e := service.Complete(ctx, who, f.Tenant, f.Project, session.ID, f.CompleteKey, styleupload.CompleteRequest{Parts: completed, ExpectedSize: n})
	if e != nil || replay.Artifact == nil || *replay.Artifact != *session.Artifact {
		t.Fatal("completion replay changed artifact", e)
	}
	genuineStyleWrite(t, output, map[string]any{"schema_version": 1, "tenant": f.Tenant, "project": f.Project, "actor": f.Actor, "session": f.Session, "upload_id": session.ID, "package_artifact": session.Artifact, "package_sha": f.SHA256, "package_bucket": f.Objects.Bucket, "bytes": n, "candidate_review_required": true})
}
