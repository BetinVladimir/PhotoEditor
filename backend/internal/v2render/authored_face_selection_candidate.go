package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Metadata must come from authored_render_face_producer_current before AND
// after physical reads. A value of this type alone never certifies authority.
type AuthoredFaceProducerObservation struct {
	RunState       string                   `json:"run_state"`
	ReviewRequired bool                     `json:"review_required"`
	Expected       v2qualification.Expected `json:"qualification_expected"`
	Operation      string                   `json:"operation"`
	Version        string                   `json:"operation_version"`
	Run            uuid.UUID                `json:"run_id"`
	Task           uuid.UUID                `json:"task_id"`
	Result         uuid.UUID                `json:"result_id"`
	Attempt        int                      `json:"attempt"`
	Fence          int64                    `json:"fence"`
	Params         v2input.Ref              `json:"params"`
	InputDigest    string                   `json:"input_digest"`
	Profile        json.RawMessage          `json:"profile"`
	Roots          []v2input.Ref            `json:"roots"`
}
type AuthoredFaceSelectionCandidate struct {
	Tenant         uuid.UUID                       `json:"tenant_id"`
	Parent         uuid.UUID                       `json:"parent_run_id"`
	Instruction    uuid.UUID                       `json:"instruction_id"`
	Ordinal        int                             `json:"ordinal"`
	ProgramSHA     string                          `json:"program_sha256"`
	PredecessorSHA string                          `json:"predecessor_sha256"`
	Source         v2input.SessionVersionBinding   `json:"source"`
	Detector       AuthoredFaceProducerObservation `json:"detector"`
	Landmarks      AuthoredFaceProducerObservation `json:"landmarks"`
	Parser         AuthoredFaceProducerObservation `json:"parser"`
	Objects        []uuid.UUID                     `json:"objects"`
	DetectorJCS    string                          `json:"detector_jcs"`
	LandmarksJCS   string                          `json:"landmarks_jcs"`
	ParserJCS      string                          `json:"parser_jcs"`
}

// CompileAuthoredFaceSelectionCandidate validates real source-bound typed
// output bytes. It does not reuse the old selection, infer identity matching or
// decide acceptance. Explicit empty selection is possible only at review time.
func CompileAuthoredFaceSelectionCandidate(source AuthoredInstructionSource, imageRaw, detectorRaw, landmarksRaw, parserRaw []byte, detector, landmarks, parser AuthoredFaceProducerObservation) (AuthoredFaceSelectionCandidate, []byte, error) {
	fail := func() (AuthoredFaceSelectionCandidate, []byte, error) {
		return AuthoredFaceSelectionCandidate{}, nil, v2input.ErrInvalid
	}
	hash := func(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
	canonicalExact := func(raw []byte) bool {
		if len(raw) < 2 || len(raw) > 1048576 {
			return false
		}
		v, e := canonical.Bytes(raw)
		return e == nil && bytes.Equal(v, raw)
	}
	if !canonicalExact(imageRaw) || hash(imageRaw) != source.Source.Source.SHA256 {
		return fail()
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return fail()
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != source.Source.CanvasHash {
		return fail()
	}
	observations := []AuthoredFaceProducerObservation{detector, landmarks, parser}
	operations := []string{"face.detect", "face.landmarks", "face.parse"}
	bodies := [][]byte{detectorRaw, landmarksRaw, parserRaw}
	for n, o := range observations {
		run, workflow, e := AuthoredFacePrerequisiteIDs(source, operations[n])
		if e != nil {
			return fail()
		}
		task, e := v2input.TaskID(workflow, operations[n], 0)
		count := 1
		if n == 2 {
			count = 2
		}
		if e != nil || o.Operation != operations[n] || o.Version != "1" || o.Run != run || o.Task != task || o.Result == uuid.Nil || o.Attempt < 1 || o.Fence < 1 || !validLayerArtifactRef(o.Params) || !authoredFinalDigest(o.InputDigest) || !json.Valid(o.Profile) || len(o.Roots) != count {
			return fail()
		}
		for _, ref := range o.Roots {
			if !validLayerArtifactRef(ref) {
				return fail()
			}
		}
		var content struct {
			Warnings []string `json:"warnings"`
			Model    string   `json:"model_lock_id"`
			Backend  string   `json:"backend"`
		}
		if json.Unmarshal(bodies[n], &content) != nil || o.Expected.Operation != operations[n] || o.Expected.Version != "1" || content.Model != o.Expected.ModelLockSHA || content.Backend != o.Expected.Backend {
			return fail()
		}

		// These are observations, not an execution acceptance. Warning results
		// remain waiting_review until the explicit current selection is decided.
		if content.Warnings == nil || o.ReviewRequired != (len(content.Warnings) > 0) ||
			(!o.ReviewRequired && o.RunState != "completed") ||
			(o.ReviewRequired && o.RunState != "waiting_review") {
			return fail()
		}
		root := o.Roots[len(o.Roots)-1]
		if !canonicalExact(bodies[n]) || hash(bodies[n]) != root.SHA256 {
			return fail()
		}
	}
	geometry := image.Source(review.ArtifactRef{ID: source.Source.Source.ArtifactID, SHA256: source.Source.Source.SHA256})
	boxes, err := review.ParseFaceBoxes(detectorRaw, geometry)
	if err != nil {
		return fail()
	}
	faces, err := review.ParseFaceLandmarkCollection(landmarksRaw, geometry)
	if err != nil {
		return fail()
	}
	labels, err := review.ParseFaceLabels19(parserRaw, geometry)
	if err != nil {
		return fail()
	}
	detectorRef := review.ArtifactRef{ID: detector.Roots[0].ArtifactID, SHA256: detector.Roots[0].SHA256}
	var landmarkLink struct {
		Faces review.ArtifactRef `json:"faces"`
	}
	if json.Unmarshal(landmarksRaw, &landmarkLink) != nil || landmarkLink.Faces != detectorRef || labels.Faces != detectorRef || labels.Labels.ID != parser.Roots[0].ArtifactID || labels.Labels.SHA256 != parser.Roots[0].SHA256 {
		return fail()
	}
	if len(boxes.Faces) > 64 || len(boxes.Faces) != len(faces) || labels.ParsedFaces != len(boxes.Faces) {
		return fail()
	}
	available := map[uuid.UUID]bool{}
	for _, box := range boxes.Faces {
		available[box.ObjectID] = true
	}
	objects := make([]uuid.UUID, 0, len(faces))
	for _, face := range faces {
		if !available[face.ObjectID] {
			return fail()
		}
		delete(available, face.ObjectID)
		objects = append(objects, face.ObjectID)
	}
	if len(available) != 0 {
		return fail()
	}
	sort.Slice(objects, func(i, j int) bool { return bytes.Compare(objects[i][:], objects[j][:]) < 0 })
	result := AuthoredFaceSelectionCandidate{source.Tenant, source.Parent, source.Instruction.Identity, source.Instruction.Ordinal, source.ProgramSHA, source.PredecessorSHA, source.Source, detector, landmarks, parser, objects, string(detectorRaw), string(landmarksRaw), string(parserRaw)}
	raw, err := canonical.Marshal(result)
	if err != nil {
		return fail()
	}
	return result, raw, nil
}
