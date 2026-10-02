package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type adoptedRootFixture struct {
	binding       AdoptedLensBinding
	object        objectstore.Object
	body          []byte
	opens, closes int
}

func (f *adoptedRootFixture) LoadObject(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, v2input.Ref, string) (AdoptedLensBinding, objectstore.Object, error) {
	return f.binding, f.object, nil
}
func (f *adoptedRootFixture) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	f.opens++
	return &adoptedRootReader{Reader: bytes.NewReader(f.body), owner: f}, nil
}

type adoptedRootReader struct {
	*bytes.Reader
	owner *adoptedRootFixture
}

func (r *adoptedRootReader) Close() error { r.owner.closes++; return nil }

func TestAdoptedRootExactBytesSchemaAndCleanup(t *testing.T) {
	for _, mutation := range []string{"valid", "truncated", "extra", "sha", "missing-field", "noncanonical"} {
		t.Run(mutation, func(t *testing.T) {
			_, base, child := compositionInputs()
			value := map[string]any{"schema_version": 1, "working_image": child, "changed_mask": child, "report": child, "generated_mask": nil}
			if mutation == "missing-field" {
				delete(value, "generated_mask")
			}
			body, _ := canonical.Marshal(value)
			if mutation == "noncanonical" {
				body = append([]byte(" "), body...)
			}
			digest := sha256.Sum256(body)
			root := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
			f := &adoptedRootFixture{binding: AdoptedLensBinding{ResultID: uuid.New(), Result: root, Base: base}, object: objectstore.Object{Key: "root", VersionID: "immutable-version", Bytes: int64(len(body)), SHA256: root.SHA256, ContentType: "application/json"}, body: body}
			switch mutation {
			case "truncated":
				f.body = body[:len(body)-1]
			case "extra":
				f.body = append(append([]byte(nil), body...), byte(' '))
			case "sha":
				f.body = append([]byte(nil), body...)
				f.body[0] = '['
			}
			binding, result, err := LoadAdoptedLensResult(context.Background(), f, f, auth.Identity{}, uuid.New(), uuid.New(), base.AssetVersionID, uuid.New(), root, "test")
			if mutation == "valid" {
				if err != nil || binding != f.binding || result.WorkingImage != child || result.GeneratedMask != nil {
					t.Fatal(result, err)
				}
			} else if err == nil {
				t.Fatal("invalid root accepted")
			}
			if f.opens != 1 || f.closes != 1 {
				t.Fatal("reader leaked", f.opens, f.closes)
			}
		})
	}
}
