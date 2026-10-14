package v2portrait

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func checkDonorFaceEvidence(t *testing.T, p OpenEyesParams, proofRaw []byte) (OpenEyesParams, []byte, []byte) {
	t.Helper()
	var proof DonorConfirmation
	if json.Unmarshal(proofRaw, &proof) != nil {
		t.Fatal("proof")
	}
	build := func(binding v2input.SessionVersionBinding, observation, face uuid.UUID, point float64) ([]byte, string) {
		points := make([]map[string]float64, 68)
		for i := range points {
			points[i] = map[string]float64{"x": point, "y": .5}
		}
		raw, err := canonical.Marshal(map[string]any{"schema_version": 1, "binding": binding, "faces": []any{map[string]any{"observation_id": observation, "face_id": face, "person_id": nil, "box": map[string]float64{"x0": .25, "y0": .25, "x1": .75, "y1": .75}, "landmarks": points, "embedding": nil, "quality": .9}}})
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		return raw, hex.EncodeToString(sum[:])
	}
	source, sha := build(p.Base, proof.SourceObservationID, p.SourceFaceID, .5)
	p.SourceFaces.SHA256 = sha
	donor, sha := build(p.Donor, *proof.DonorObservationID, p.DonorFaceID, .5)
	p.DonorFaces.SHA256 = sha
	geometry, err := VerifyOpenEyesFaces(p, proofRaw, source, donor, [2]int64{100, 200}, [2]int64{200, 100}, 50)
	if err != nil || geometry.SourceLandmarks[0] != [2]float64{50, 100} || geometry.DonorLandmarks[0] != [2]float64{100, 50} || geometry.SourceDiagonal != math.Hypot(50, 100) {
		t.Fatal("geometry", geometry, err)
	}
	for _, mode := range []string{"small", "roi", "face", "observation", "digest", "canvas_size", "calibration"} {
		params := p
		raw := source
		size := [2]int64{100, 200}
		min := 50.0
		switch mode {
		case "small":
			min = 51
		case "roi":
			raw, params.SourceFaces.SHA256 = build(p.Base, proof.SourceObservationID, p.SourceFaceID, .9)
		case "face":
			raw, params.SourceFaces.SHA256 = build(p.Base, proof.SourceObservationID, uuid.New(), .5)
		case "observation":
			raw, params.SourceFaces.SHA256 = build(p.Base, uuid.New(), p.SourceFaceID, .5)
		case "digest":
			raw = append([]byte(nil), source...)
			raw[0] ^= 1
		case "canvas_size":
			size = [2]int64{200000001, 1}
		case "calibration":
			min = math.NaN()
		}
		if got, err := VerifyOpenEyesFaces(params, proofRaw, raw, donor, size, [2]int64{200, 100}, min); err == nil || got != (DonorFaceGeometry{}) {
			t.Fatal("invalid evidence admitted", mode, err)
		}
	}
	return p, source, donor

}
