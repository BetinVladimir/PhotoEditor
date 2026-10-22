package retryscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func freshScope() []Upload {
	return []Upload{{UploadID: uuid.New(), RunID: uuid.New(), TenantID: uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841"), File: "IMG_3903.CR2", SHA: "cd22202c17b36502b5a2b591d6476c605937d1c2d8c895d52d72540d720e5a14", Bytes: 31432591}, {UploadID: uuid.New(), RunID: uuid.New(), TenantID: uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841"), File: "IMG_3904.CR2", SHA: "535bef69a70994b5a6a0668e5b2ff206cdba6c98f195db5ff38a08c89af677b3", Bytes: 31211050}}
}
func installScope(t *testing.T, v []Upload) {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(raw)
	t.Setenv("EXPECTED_NEW_UPLOAD_SCOPE", string(raw))
	t.Setenv("EXPECTED_NEW_UPLOAD_SCOPE_SHA256", hex.EncodeToString(h[:]))
}
func TestExact21RetainsBothPreviousFailures(t *testing.T) {
	v := freshScope()
	installScope(t, v)
	uploads, runs, e := Scope()
	if e != nil || len(uploads) != 2 || len(runs) != 21 {
		t.Fatalf("closed scope failed: %v", e)
	}
	for _, id := range []string{"fd6d1654-ffc5-447c-a33f-19174422f1fa", "c43360a2-8b16-462f-a704-fccaabfc4220"} {
		if runs[uuid.MustParse(id)] == uuid.Nil {
			t.Fatal("previous failure missing")
		}
	}
}
func TestSecondFailedUploadCannotBeReplayed(t *testing.T) {
	for _, id := range []string{"cbdbb165-c027-421e-912e-fd3d78ef1d30", "e61f3de1-9694-4da3-9f10-3e53a63df2a9"} {
		v := freshScope()
		v[0].UploadID = uuid.MustParse(id)
		installScope(t, v)
		if _, _, e := Scope(); e == nil {
			t.Fatal("reused failed upload admitted")
		}
	}
}
func TestSecondFailedRunCannotBeReused(t *testing.T) {
	v := freshScope()
	v[0].RunID = uuid.MustParse("fd6d1654-ffc5-447c-a33f-19174422f1fa")
	installScope(t, v)
	if _, _, e := Scope(); e == nil {
		t.Fatal("reused failed run admitted")
	}
}
func TestFutureScopeWrongSourceTenantOrDuplicatesRefused(t *testing.T) {
	for _, mode := range []string{"sha", "bytes", "tenant", "duplicate", "digest"} {
		t.Run(mode, func(t *testing.T) {
			v := freshScope()
			switch mode {
			case "sha":
				v[0].SHA = v[1].SHA
			case "bytes":
				v[0].Bytes++
			case "tenant":
				v[0].TenantID = uuid.New()
			case "duplicate":
				v[1] = v[0]
			}
			installScope(t, v)
			if mode == "digest" {
				t.Setenv("EXPECTED_NEW_UPLOAD_SCOPE_SHA256", "bad")
			}
			if _, _, e := Scope(); e == nil {
				t.Fatal("wrong scope admitted")
			}
		})
	}
}
