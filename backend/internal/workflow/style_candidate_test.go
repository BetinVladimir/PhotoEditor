package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type candidateRepo struct {
	lease           *StyleCandidateLease
	calls           int
	config, metrics json.RawMessage
	hash, code      string
	low             bool
}

func (r *candidateRepo) Claim(context.Context) (*StyleCandidateLease, error) { return r.lease, nil }
func (r *candidateRepo) Finish(_ context.Context, _ StyleCandidateLease, c json.RawMessage, h string, m json.RawMessage, l bool, code string) (bool, error) {
	r.calls++
	r.config = c
	r.hash = h
	r.metrics = m
	r.low = l
	r.code = code
	return true, nil
}

type candidateObjects struct {
	bodies map[string][]byte
	opens  int
}

func (o *candidateObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	o.opens++
	return io.NopCloser(bytes.NewReader(o.bodies[obj.Key])), nil
}
func candidateObject(key string, raw []byte) objectstore.Object {
	sum := sha256.Sum256(raw)
	return objectstore.Object{Key: key, VersionID: "v1", SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(raw)), ContentType: "application/json"}
}

func TestStyleCandidateMaterializerValidatesAndFinishes(t *testing.T) {
	styleID := uuid.New()
	config := []byte(`{"schema_version":"2.0.0","renderer_version":"photo_renderer_v2","style_id":"` + styleID.String() + `","quality":{"status":"needs_review","transfer_confidence":"unvalidated","warnings":[]}}`)
	report := []byte(`{"pair_count":2}`)
	lease := &StyleCandidateLease{ID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), RunID: uuid.New(), StyleID: styleID, ConfigID: uuid.New(), ReportID: uuid.New(), Config: candidateObject("config", config), Report: candidateObject("report", report)}
	repo := &candidateRepo{lease: lease}
	objects := &candidateObjects{bodies: map[string][]byte{"config": config, "report": report}}
	done, err := (StyleCandidateMaterializer{Repository: repo, Objects: objects}).ProcessOne(context.Background())
	if err != nil || !done || repo.calls != 1 || repo.code != "" || !repo.low || objects.opens != 2 {
		t.Fatal(done, err, repo.calls, repo.code, repo.low, objects.opens)
	}
	canonicalConfig, _ := canonical.Bytes(config)
	sum := sha256.Sum256(canonicalConfig)
	if repo.hash != hex.EncodeToString(sum[:]) || !bytes.Equal(repo.config, canonicalConfig) || !bytes.Equal(repo.metrics, report) {
		t.Fatal("candidate content changed")
	}
}
func TestStyleCandidateMaterializerRejectsWrongStyleBinding(t *testing.T) {
	config := []byte(`{"schema_version":"2.0.0","renderer_version":"photo_renderer_v2","style_id":"wrong","quality":{"status":"measured","transfer_confidence":"validated","warnings":[]}}`)
	report := []byte(`{}`)
	lease := &StyleCandidateLease{ID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), RunID: uuid.New(), StyleID: uuid.New(), ConfigID: uuid.New(), ReportID: uuid.New(), Config: candidateObject("config", config), Report: candidateObject("report", report)}
	repo := &candidateRepo{lease: lease}
	done, err := (StyleCandidateMaterializer{Repository: repo, Objects: &candidateObjects{bodies: map[string][]byte{"config": config, "report": report}}}).ProcessOne(context.Background())
	if !done || err == nil || repo.code != "STYLE_CANDIDATE_INVALID" || repo.calls != 1 {
		t.Fatal(done, err, repo.code, repo.calls)
	}
}
