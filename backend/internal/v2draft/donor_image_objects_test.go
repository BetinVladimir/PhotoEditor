package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"strings"
	"testing"
)

type donorImageCatalog struct {
	binding     v2input.SessionVersionBinding
	calls       int
	failAt      int
	wrongDigest bool
	manifest    []byte
}

func (c *donorImageCatalog) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID, node LayerRef) (objectstore.Object, error) {
	c.calls++
	if c.calls == c.failAt {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if node.ExpectedBinding == nil || *node.ExpectedBinding != c.binding {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if node.Contract == "WorkingImage" {
		if node.Ref != c.binding.Source || node.ParentRef != (v2input.Ref{}) {
			return objectstore.Object{}, v2input.ErrInvalid
		}
	} else if (node.Contract != "WorkingPixels" && node.Contract != "WorkingAlpha") || node.ParentRef != c.binding.Source {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	digest := node.Ref.SHA256
	if c.wrongDigest {
		digest = "wrong"
	}
	if node.Contract == "WorkingImage" && c.manifest != nil {
		return objectstore.Object{Bytes: int64(len(c.manifest)), SHA256: digest, ContentType: "application/json", VersionID: "pinned"}, nil
	}
	return objectstore.Object{Bytes: 128, SHA256: digest}, nil
}

type donorManifestReader struct {
	raw     []byte
	calls   int
	corrupt bool
}

func (r *donorManifestReader) Open(_ context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	r.calls++
	raw := append([]byte(nil), r.raw...)
	if r.corrupt {
		raw[0] ^= 1
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

type donorSessionTestTx struct {
	pgx.Tx
	calls int
}
type donorSessionTestRow struct{ role bool }

func (r donorSessionTestRow) Scan(dest ...any) error {
	if r.role {
		*dest[0].(*string) = "photo_scheduler"
	} else {
		*dest[0].(*bool) = true
	}
	return nil
}
func (tx *donorSessionTestTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	tx.calls++
	return donorSessionTestRow{role: tx.calls == 1}
}

type donorConfirmationCatalog struct {
	params v2portrait.OpenEyesParams
	raw    []byte
	deny   bool
	calls  int
}

func (c *donorConfirmationCatalog) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID, node LayerRef) (objectstore.Object, error) {
	c.calls++
	if c.deny || node.Contract != "DonorConfirmation" || node.Ref != c.params.Confirmation || node.ParentRef != c.params.Base.Source || node.ExpectedBinding == nil || *node.ExpectedBinding != c.params.Base {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return objectstore.Object{Bytes: int64(len(c.raw)), SHA256: c.params.Confirmation.SHA256, ContentType: "application/json", VersionID: "confirmation-pinned"}, nil
}

func TestDonorImageObjectsIndependentVersionAuthorization(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("images")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	image.Pixels.ID = uuid.New()
	if image.Alpha != nil {
		alpha := *image.Alpha
		alpha.ID = uuid.New()
		image.Alpha = &alpha
	}
	source, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../v2portrait/testdata/open-eyes-params-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Params v2portrait.OpenEyesParams `json:"params"`
	}
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("params")
	}
	p := fixture.Params
	sum := sha256.Sum256(source)
	p.Base.Source.SHA256 = hex.EncodeToString(sum[:])
	p.Donor.Source.SHA256 = p.Base.Source.SHA256
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	p.Base.CanvasHash = canvas
	p.Donor.CanvasHash = canvas
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	donorBinding := p.Donor
	donorFace := p.DonorFaceID
	donorObservation := uuid.New()
	proof := v2portrait.DonorConfirmation{SchemaVersion: 1, PersonID: uuid.New(), Source: p.Base, Donor: &donorBinding, SourceFaceID: p.SourceFaceID, DonorFaceID: &donorFace, SourceObservationID: uuid.New(), DonorObservationID: &donorObservation, ConsentID: uuid.New(), Epoch: "1", ActorID: who.UserID}
	proofRaw, proofSHA, err := proof.Compile()
	if err != nil {
		t.Fatal(err)
	}
	p.Confirmation.SHA256 = proofSHA
	arrays := 1
	if image.Alpha != nil {
		arrays++
	}
	for fail := 0; fail <= arrays+1; fail++ {
		base := &donorImageCatalog{binding: p.Base}
		donor := &donorImageCatalog{binding: p.Donor, failAt: fail}
		got, err := ResolveDonorImageObjects(context.Background(), auth.Identity{}, uuid.New(), p, source, source, base, donor)
		if fail == 0 {
			if err != nil || len(got) != 2*arrays || base.calls != arrays+1 || donor.calls != arrays+1 {
				t.Fatal("independent scopes", got, err, base.calls, donor.calls)
			}
		} else if err == nil || got != nil {
			t.Fatal("partial evidence escaped failed donor authorization")
		}
	}
	base := &donorImageCatalog{binding: p.Base}
	donor := &donorImageCatalog{binding: p.Donor, wrongDigest: true}
	if got, err := ResolveDonorImageObjects(context.Background(), auth.Identity{}, uuid.New(), p, source, source, base, donor); err == nil || got != nil {
		t.Fatal("catalog digest mismatch admitted")
	}
	for _, mode := range []string{"ok", "corrupt", "denied"} {
		base := &donorImageCatalog{binding: p.Base, manifest: source}
		donor := &donorImageCatalog{binding: p.Donor, manifest: source}
		reader := &donorManifestReader{raw: source, corrupt: mode == "corrupt"}
		if mode == "denied" {
			donor.failAt = 1
		}
		evidence, err := LoadDonorImageObjects(context.Background(), auth.Identity{}, uuid.New(), p, reader, base, donor)
		if mode == "ok" {
			if err != nil || !bytes.Equal(evidence.SourceRaw, source) || !bytes.Equal(evidence.DonorRaw, source) || len(evidence.Physical) != 2*arrays || reader.calls != 2 {
				t.Fatal("manifest load", evidence, err, reader.calls)
			}
		} else if err == nil || evidence.SourceRaw != nil || evidence.DonorRaw != nil || evidence.Physical != nil || reader.calls != 1 {
			t.Fatal("failed load leaked evidence or read denied manifest", mode, err, reader.calls)
		}
	}
	capacity, err := v2portrait.DonorPortraitResources(image.Canvas.Width, image.Canvas.Height, image.Canvas.Width, image.Canvas.Height, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ok", "runtime", "capacity", "denied"} {
		base := &donorImageCatalog{binding: p.Base, manifest: source}
		donor := &donorImageCatalog{binding: p.Donor, manifest: source}
		reader := &donorManifestReader{raw: source}
		runtime := strings.Repeat("a", 64)
		budget := capacity
		if mode == "runtime" {
			runtime = "invalid"
		}
		if mode == "capacity" {
			budget.RAMBytes--
		}
		if mode == "denied" {
			donor.failAt = 1
		}
		got, err := LoadAndCompileDonorTaskPlan(context.Background(), auth.Identity{}, uuid.New(), p, reader, base, donor, 1, 1, runtime, budget)
		if mode == "ok" {
			if err != nil || got.Plan.Operation != "portrait.open_eyes" || len(got.Images.Physical) != 2*arrays || len(got.Plan.PhysicalImageOccurrences) != 2*arrays {
				t.Fatal("loaded plan", got, err)
			}
			checkLoadedDonorFaces(t, got, p, proofRaw)
			tx := &donorSessionTestTx{}
			if err := LockLoadedDonorSession(context.Background(), tx, who, uuid.New(), uuid.New(), uuid.New(), got, proofRaw); err != nil || tx.calls != 2 {
				t.Fatal("loaded session", err, tx.calls)
			}
			altered := got
			altered.Plan.ParamsSHA = strings.Repeat("f", 64)
			tx = &donorSessionTestTx{}
			if err := LockLoadedDonorSession(context.Background(), tx, who, uuid.New(), uuid.New(), uuid.New(), altered, proofRaw); err == nil || tx.calls != 0 {
				t.Fatal("modified plan reached SQL")
			}
			for _, proofMode := range []string{"ok", "denied", "corrupt"} {
				catalog := &donorConfirmationCatalog{params: p, raw: proofRaw, deny: proofMode == "denied"}
				reader := &donorManifestReader{raw: proofRaw, corrupt: proofMode == "corrupt"}
				tx := &donorSessionTestTx{}
				err := LockLoadedDonorSessionFromStore(context.Background(), tx, who, uuid.New(), uuid.New(), uuid.New(), got, catalog, reader)
				if proofMode == "ok" {
					if err != nil || tx.calls != 2 || reader.calls != 1 {
						t.Fatal("stored confirmation", err, tx.calls, reader.calls)
					}
				} else if err == nil || tx.calls != 0 {
					t.Fatal("untrusted confirmation reached SQL", proofMode, err, tx.calls)
				}
				if proofMode == "denied" && reader.calls != 0 {
					t.Fatal("denied confirmation downloaded")
				}
			}
			for _, field := range []string{"profile", "reserve", "inputs", "physical", "occurrences"} {
				altered := got
				switch field {
				case "profile":
					altered.Plan.Profile = append([]byte(nil), got.Plan.Profile...)
					altered.Plan.Profile[0] ^= 1
				case "reserve":
					altered.Plan.ReservedBytes--
				case "inputs":
					altered.Plan.Inputs = append([]v2input.Ref(nil), got.Plan.Inputs...)
					altered.Plan.Inputs[0].ArtifactID = uuid.New()
				case "physical":
					altered.Plan.PhysicalImageInputs = append([]v2input.Entry(nil), got.Plan.PhysicalImageInputs...)
					altered.Plan.PhysicalImageInputs[0].Contract = "Artifact"
				case "occurrences":
					altered.Plan.PhysicalImageOccurrences = append([]v2portrait.DonorImageOccurrence(nil), got.Plan.PhysicalImageOccurrences...)
					altered.Plan.PhysicalImageOccurrences[0].Role = "WorkingAlpha"
				}
				tx := &donorSessionTestTx{}
				if err := LockLoadedDonorSession(context.Background(), tx, who, uuid.New(), uuid.New(), uuid.New(), altered, proofRaw); err == nil || tx.calls != 0 {
					t.Fatal("modified derived field reached SQL", field, err, tx.calls)
				}
			}
		} else if err == nil || got.Plan.Params != nil || got.Images.SourceRaw != nil {
			t.Fatal("failed assembly leaked plan", mode, err)
		}
		if mode == "runtime" && (reader.calls != 0 || base.calls != 0 || donor.calls != 0) {
			t.Fatal("invalid runtime performed IO")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	base.calls = 0
	if got, err := ResolveDonorImageObjects(ctx, auth.Identity{}, uuid.New(), p, source, source, base, donor); err == nil || got != nil || base.calls != 0 {
		t.Fatal("cancelled resolver performed IO")
	}
}
