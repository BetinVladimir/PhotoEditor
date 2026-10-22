// Operator-only closed scope; external root inventory/quiescence receipt remains mandatory.
package retryscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
)

type Upload struct {
	UploadID uuid.UUID `json:"upload_id"`
	RunID    uuid.UUID `json:"run_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	File     string    `json:"file"`
	SHA      string    `json:"sha256"`
	Bytes    int64     `json:"bytes"`
}

func Scope() (map[uuid.UUID]uuid.UUID, map[uuid.UUID]uuid.UUID, error) {
	raw := []byte(os.Getenv("EXPECTED_NEW_UPLOAD_SCOPE"))
	var fresh []Upload
	if json.Unmarshal(raw, &fresh) != nil || len(fresh) != 2 {
		return nil, nil, errors.New("exact two new upload bindings required")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != os.Getenv("EXPECTED_NEW_UPLOAD_SCOPE_SHA256") {
		return nil, nil, errors.New("new scope digest drift")
	}
	originals, e := os.ReadFile("/Users/freelancer/Documents/PhotoEditor/work/rtx-fifteen-upload-completion-r2/scope.json")
	if e != nil {
		return nil, nil, e
	}
	h := sha256.Sum256(originals)
	if hex.EncodeToString(h[:]) != "9b7f80d2a9cde6ded65892c7e4386436e47db367d6d481a3c903aa768690262f" {
		return nil, nil, errors.New("historical scope drift")
	}
	var old struct {
		Uploads []Upload `json:"uploads"`
	}
	if json.Unmarshal(originals, &old) != nil || len(old.Uploads) != 15 {
		return nil, nil, errors.New("historical fifteen required")
	}
	runs := map[uuid.UUID]uuid.UUID{}
	for _, x := range old.Uploads {
		if x.RunID == uuid.Nil || x.TenantID == uuid.Nil || runs[x.RunID] != uuid.Nil {
			return nil, nil, errors.New("historical binding invalid")
		}
		runs[x.RunID] = x.TenantID
	}
	tenant := uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841")
	for _, id := range []string{"39858245-cbe6-4301-a90d-7023832c33ee", "13dce0c7-d888-4bf1-a550-99a9e1cef6d5", "fd6d1654-ffc5-447c-a33f-19174422f1fa", "c43360a2-8b16-462f-a704-fccaabfc4220"} {
		runs[uuid.MustParse(id)] = tenant
	}
	uploads := map[uuid.UUID]uuid.UUID{}
	seen := map[string]bool{}
	for _, x := range fresh {
		expectedSHA := ""
		var expectedBytes int64
		switch x.File {
		case "IMG_3903.CR2":
			expectedSHA = "cd22202c17b36502b5a2b591d6476c605937d1c2d8c895d52d72540d720e5a14"
			expectedBytes = 31432591
		case "IMG_3904.CR2":
			expectedSHA = "535bef69a70994b5a6a0668e5b2ff206cdba6c98f195db5ff38a08c89af677b3"
			expectedBytes = 31211050
		default:
			return nil, nil, errors.New("unexpected original")
		}
		if seen[x.File] || x.SHA != expectedSHA || x.Bytes != expectedBytes || x.TenantID != tenant || x.UploadID == uuid.Nil || x.RunID == uuid.Nil || uploads[x.UploadID] != uuid.Nil || runs[x.RunID] != uuid.Nil {
			return nil, nil, errors.New("new binding scope invalid")
		}
		if x.UploadID == uuid.MustParse("cbdbb165-c027-421e-912e-fd3d78ef1d30") || x.UploadID == uuid.MustParse("e61f3de1-9694-4da3-9f10-3e53a63df2a9") {
			return nil, nil, errors.New("second failed upload cannot be replayed")
		}
		for _, prior := range old.Uploads {
			if x.UploadID == prior.UploadID {
				return nil, nil, errors.New("old upload cannot be replayed")
			}
		}
		seen[x.File] = true
		uploads[x.UploadID] = tenant
		runs[x.RunID] = tenant
	}
	if len(runs) != 21 {
		return nil, nil, errors.New("twenty-one run superset required")
	}
	return uploads, runs, nil
}
