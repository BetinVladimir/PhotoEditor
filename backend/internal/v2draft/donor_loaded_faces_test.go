package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"testing"
)

type donorFaceCatalog struct {
	params  v2portrait.OpenEyesParams
	binding v2input.SessionVersionBinding
	bodies  map[uuid.UUID][]byte
	lookups *int
	failAt  int
}

func (c donorFaceCatalog) Lookup(_ context.Context, _ auth.Identity, _ uuid.UUID, node LayerRef) (objectstore.Object, error) {
	*c.lookups++
	if *c.lookups == c.failAt {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if node.ExpectedBinding == nil || *node.ExpectedBinding != c.binding || node.CanvasHash != c.binding.CanvasHash {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if node.Contract == "DonorConfirmation" {
		if c.binding != c.params.Base || node.Ref != c.params.Confirmation || node.ParentRef != c.params.Base.Source {
			return objectstore.Object{}, v2input.ErrInvalid
		}
	} else if node.Contract != "DonorFaceObservations" || node.ParentRef != c.params.Confirmation ||
		(c.binding == c.params.Base && node.Ref != c.params.SourceFaces) || (c.binding == c.params.Donor && node.Ref != c.params.DonorFaces) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	raw, ok := c.bodies[node.Ref.ArtifactID]
	if !ok {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return objectstore.Object{Key: node.Ref.ArtifactID.String(), Bytes: int64(len(raw)), SHA256: node.Ref.SHA256, ContentType: "application/json", VersionID: "pinned"}, nil
}

type donorFaceReader struct {
	bodies           map[uuid.UUID][]byte
	calls, corruptAt int
}

func (r *donorFaceReader) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	r.calls++
	id, err := uuid.Parse(object.Key)
	if err != nil {
		return nil, err
	}
	raw := append([]byte(nil), r.bodies[id]...)
	if r.calls == r.corruptAt {
		raw[0] ^= 1
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

func checkLoadedDonorFaces(t *testing.T, loaded LoadedDonorTaskPlan, p v2portrait.OpenEyesParams, proofRaw []byte) {
	t.Helper()
	var proof v2portrait.DonorConfirmation
	if json.Unmarshal(proofRaw, &proof) != nil {
		t.Fatal("proof")
	}
	bodies := map[uuid.UUID][]byte{p.Confirmation.ArtifactID: proofRaw}
	for i, binding := range []v2input.SessionVersionBinding{p.Base, p.Donor} {
		observation, face := proof.SourceObservationID, p.SourceFaceID
		ref := p.SourceFaces
		if i == 1 {
			observation = *proof.DonorObservationID
			face = p.DonorFaceID
			ref = p.DonorFaces
		}
		points := make([]map[string]float64, 68)
		for j := range points {
			points[j] = map[string]float64{"x": .5, "y": .5}
		}
		raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "binding": binding, "faces": []any{map[string]any{"observation_id": observation, "face_id": face, "person_id": nil, "box": map[string]float64{"x0": 0, "y0": 0, "x1": 1, "y1": 1}, "landmarks": points, "embedding": nil, "quality": .9}}})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		ref.SHA256 = hex.EncodeToString(sum[:])
		bodies[ref.ArtifactID] = raw
		if i == 0 {
			p.SourceFaces = ref
		} else {
			p.DonorFaces = ref
		}
	}
	body, _, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	loaded.Plan, err = v2portrait.CompileDonorTaskPlan(context.Background(), p.Base, p.Donor, loaded.Images.SourceRaw, loaded.Images.DonorRaw, body, loaded.patchWidth, loaded.patchHeight, loaded.runtimeSHA, loaded.capacity)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ok", "deny1", "deny2", "deny3", "corrupt1", "corrupt2", "corrupt3", "calibration", "cancelled"} {
		lookups := 0
		fail, corrupt := 0, 0
		min := .0001
		switch mode {
		case "deny1":
			fail = 1
		case "deny2":
			fail = 2
		case "deny3":
			fail = 3
		case "corrupt1":
			corrupt = 1
		case "corrupt2":
			corrupt = 2
		case "corrupt3":
			corrupt = 3
		case "calibration":
			min = math.NaN()
		}
		base := donorFaceCatalog{p, p.Base, bodies, &lookups, fail}
		donor := donorFaceCatalog{p, p.Donor, bodies, &lookups, fail}
		reader := &donorFaceReader{bodies: bodies, corruptAt: corrupt}
		ctx := context.Background()
		if mode == "cancelled" {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			ctx = cancelled
		}
		got, err := LoadDonorFaceEvidence(ctx, auth.Identity{}, uuid.New(), loaded, reader, base, donor, min)
		if mode == "ok" {
			if err != nil || got.Geometry.SourceDiagonal <= 0 || !bytes.Equal(got.ConfirmationRaw, proofRaw) || reader.calls != 3 {
				t.Fatal("face evidence load", err, reader.calls)
			}
			var issuer v2portrait.DonorConfirmation
			json.Unmarshal(proofRaw, &issuer)
			who := auth.Identity{UserID: issuer.ActorID, SessionID: uuid.New()}
			tx := &donorSessionTestTx{}
			if err := LockLoadedDonorFaceSession(ctx, tx, who, uuid.New(), uuid.New(), uuid.New(), loaded, got); err != nil || tx.calls != 2 {
				t.Fatal("verified face session gate", err, tx.calls)
			}
			for _, alteration := range []string{"geometry", "source_bytes", "calibration"} {
				changed := got
				switch alteration {
				case "geometry":
					changed.Geometry.SourceLandmarks[0][0]++
				case "source_bytes":
					changed.SourceFacesRaw = append([]byte(nil), got.SourceFacesRaw...)
					changed.SourceFacesRaw[0] ^= 1
				case "calibration":
					changed.minEdge = 0
				}
				tx := &donorSessionTestTx{}
				if err := LockLoadedDonorFaceSession(ctx, tx, who, uuid.New(), uuid.New(), uuid.New(), loaded, changed); err == nil || tx.calls != 0 {
					t.Fatal("altered face evidence reached SQL", alteration, err)
				}
			}
		} else if err == nil || got.ConfirmationRaw != nil || got.SourceFacesRaw != nil || got.DonorFacesRaw != nil || got.Geometry != (v2portrait.DonorFaceGeometry{}) {
			t.Fatal("partial evidence escaped", mode, err)
		}
		if fail > 0 && reader.calls != fail-1 {
			t.Fatal("denied object read", mode, reader.calls)
		}
		if corrupt > 0 && reader.calls != corrupt {
			t.Fatal("read after corruption", mode, reader.calls)
		}
		if (mode == "calibration" || mode == "cancelled") && (lookups != 0 || reader.calls != 0) {
			t.Fatal("invalid preflight performed IO", mode)
		}
	}
}
