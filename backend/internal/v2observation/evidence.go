package v2observation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

var ErrEvidence = errors.New("invalid face evidence")

type ArtifactRef struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}

type Binding struct {
	AssetVersionID uuid.UUID   `json:"asset_version_id"`
	Source         ArtifactRef `json:"source"`
	CanvasHash     string      `json:"canvas_hash"`
}

type Face struct {
	ObservationID uuid.UUID   `json:"observation_id"`
	FaceID        uuid.UUID   `json:"face_id"`
	PersonID      *uuid.UUID  `json:"person_id"`
	Box           *FaceRect   `json:"box"`
	Landmarks     []FacePoint `json:"landmarks"`
	Embedding     []float64   `json:"embedding"`
	Quality       *float64    `json:"quality"`
}

type FaceRect struct {
	X0 *float64 `json:"x0"`
	Y0 *float64 `json:"y0"`
	X1 *float64 `json:"x1"`
	Y1 *float64 `json:"y1"`
}
type FacePoint struct {
	X *float64 `json:"x"`
	Y *float64 `json:"y"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func unit(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) && x >= 0 && x <= 1 }

func requiredJSONKeys(raw []byte, keys ...string) bool {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	for _, key := range keys {
		if _, ok := value[key]; !ok {
			return false
		}
	}
	return true
}

type Evidence struct {
	SchemaVersion int     `json:"schema_version"`
	Binding       Binding `json:"binding"`
	Faces         []Face  `json:"faces"`
}

type Canvas struct {
	Width        int64  `json:"width"`
	Height       int64  `json:"height"`
	Orientation  int    `json:"orientation"`
	SpaceID      string `json:"space_id"`
	SourceSHA256 string `json:"source_sha256"`
}

// VerifyWorkingImageBinding checks the immutable document underlying a
// VersionBinding. The version content hash is the original source lineage.
func VerifyWorkingImageBinding(raw []byte, binding Binding, versionContentSHA string) error {
	if len(raw) == 0 || len(raw) > 1_048_576 || !shaPattern.MatchString(versionContentSHA) {
		return ErrEvidence
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != binding.Source.SHA256 || uniqueJSON(raw) != nil {
		return ErrEvidence
	}
	if _, err := canonical.Bytes(raw); err != nil {
		return ErrEvidence
	}
	var doc struct {
		Canvas *Canvas `json:"canvas"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Canvas == nil ||
		doc.Canvas.Width < 1 || doc.Canvas.Height < 1 ||
		doc.Canvas.Width > 2147483647 || doc.Canvas.Height > 2147483647 ||
		doc.Canvas.Width > 9007199254740991/doc.Canvas.Height ||
		doc.Canvas.Orientation != 1 || len(doc.Canvas.SpaceID) == 0 ||
		len(doc.Canvas.SpaceID) > 120 || doc.Canvas.SourceSHA256 != versionContentSHA {
		return ErrEvidence
	}
	hash, err := canonical.Hash(doc.Canvas)
	if err != nil || hash != binding.CanvasHash {
		return ErrEvidence
	}
	return nil
}

// ResolveFaceID accepts only exact, source-bound, unambiguous worker output.
// Authorization, artifact provenance and live consent are checked by the SQL
// confirmation boundary; the caller must read bytes from the pinned object.
func ResolveFaceID(raw []byte, digest string, expected Binding, observation uuid.UUID) (uuid.UUID, error) {
	return resolveFaceEvidence(raw, digest, expected, observation, true)
}

// ValidatePreEditFaces validates the full source-bound detector document without
// requiring a selected face. Identity assignment requires a separate proof.
func ValidatePreEditFaces(raw []byte, expected Binding) error {
	sum := sha256.Sum256(raw)
	_, err := resolveFaceEvidence(raw, hex.EncodeToString(sum[:]), expected, uuid.Nil, false)
	return err
}

func resolveFaceEvidence(raw []byte, digest string, expected Binding, observation uuid.UUID, requireSelected bool) (uuid.UUID, error) {
	sum := sha256.Sum256(raw)
	if len(raw) == 0 || len(raw) > 1_048_576 || requireSelected && observation == uuid.Nil ||
		hex.EncodeToString(sum[:]) != digest {
		return uuid.Nil, ErrEvidence
	}
	if err := uniqueJSON(raw); err != nil {
		return uuid.Nil, ErrEvidence
	}
	if !requiredJSONKeys(raw, "schema_version", "binding", "faces") {
		return uuid.Nil, ErrEvidence
	}
	var fields struct {
		Binding json.RawMessage   `json:"binding"`
		Faces   []json.RawMessage `json:"faces"`
	}
	if json.Unmarshal(raw, &fields) != nil ||
		!requiredJSONKeys(fields.Binding, "asset_version_id", "source", "canvas_hash") {
		return uuid.Nil, ErrEvidence
	}
	for _, item := range fields.Faces {
		if !requiredJSONKeys(item, "observation_id", "face_id", "person_id",
			"box", "landmarks", "embedding", "quality") {
			return uuid.Nil, ErrEvidence
		}
	}
	if _, err := canonical.Bytes(raw); err != nil {
		return uuid.Nil, ErrEvidence
	}
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&evidence) != nil || evidence.SchemaVersion != 1 ||
		evidence.Binding != expected || len(evidence.Faces) > 100 ||
		expected.AssetVersionID == uuid.Nil || expected.Source.ArtifactID == uuid.Nil ||
		!shaPattern.MatchString(expected.Source.SHA256) || !shaPattern.MatchString(expected.CanvasHash) {
		return uuid.Nil, ErrEvidence
	}
	seenObservations := make(map[uuid.UUID]bool)
	seenFaces := make(map[uuid.UUID]bool)
	var selected uuid.UUID
	for _, face := range evidence.Faces {
		if face.ObservationID == uuid.Nil || face.FaceID == uuid.Nil ||
			seenObservations[face.ObservationID] || seenFaces[face.FaceID] ||
			face.PersonID != nil || face.Quality == nil || !unit(*face.Quality) ||
			face.Box == nil || face.Box.X0 == nil || face.Box.Y0 == nil ||
			face.Box.X1 == nil || face.Box.Y1 == nil ||
			!unit(*face.Box.X0) || !unit(*face.Box.Y0) ||
			!unit(*face.Box.X1) || !unit(*face.Box.Y1) ||
			*face.Box.X0 >= *face.Box.X1 || *face.Box.Y0 >= *face.Box.Y1 ||
			len(face.Landmarks) != 68 ||
			(face.Embedding != nil && len(face.Embedding) != 512) {
			return uuid.Nil, ErrEvidence
		}
		for _, point := range face.Landmarks {
			if point.X == nil || point.Y == nil || !unit(*point.X) || !unit(*point.Y) {
				return uuid.Nil, ErrEvidence
			}
		}
		for _, value := range face.Embedding {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < -1 || value > 1 {
				return uuid.Nil, ErrEvidence
			}
		}
		seenObservations[face.ObservationID] = true
		seenFaces[face.FaceID] = true
		if face.ObservationID == observation {
			selected = face.FaceID
		}
	}
	if requireSelected && selected == uuid.Nil {
		return uuid.Nil, ErrEvidence
	}
	return selected, nil
}

func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := uniqueValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrEvidence
	}
	return nil
}

func uniqueValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return ErrEvidence
			}
			seen[key] = true
			if err := uniqueValue(d); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrEvidence
		}
	case '[':
		for d.More() {
			if err := uniqueValue(d); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return ErrEvidence
		}
	default:
		return ErrEvidence
	}
	return nil
}
