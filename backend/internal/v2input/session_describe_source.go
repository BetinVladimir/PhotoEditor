package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// SessionDescribeImageInputs is resolved from the pinned WorkingImage document
// before admission. Pixel and alpha NPY refs are declared explicitly because
// the worker opens them through the artifact gateway.
type SessionDescribeImageInputs struct {
	Ordinal int
	Pixels  Ref
	Alpha   *Ref
}

type SessionDescribeClosure struct {
	Open        CursorFactory
	Direct      []Ref
	InputDigest string
	Profile     json.RawMessage
}

// CompileSessionDescribeClosure binds one frozen descriptor shard to all
// artifacts its worker can read. It never consults mutable collection state.
func CompileSessionDescribeClosure(workflow, run uuid.UUID, task SessionDescriptorTask,
	snapshot SessionInputSnapshot, imageInputs []SessionDescribeImageInputs,
	snapshotRef, consents, calibration, params Ref, policySHA string,
	profile json.RawMessage) (SessionDescribeClosure, error) {
	if workflow == uuid.Nil || run == uuid.Nil || task.ID == uuid.Nil || len(task.Ordinals) < 1 ||
		len(task.Ordinals) > 128 || len(imageInputs) != len(task.Ordinals) ||
		snapshot.SchemaVersion != 1 || snapshot.SnapshotID == uuid.Nil ||
		!digestPattern.MatchString(snapshot.SnapshotDigest) ||
		len(snapshot.Items) < 1 || len(snapshot.Items) > 5000 ||
		!validRef(snapshotRef) || !validRef(consents) ||
		!validRef(calibration) || !validRef(params) ||
		!digestPattern.MatchString(policySHA) || !validSessionDescribeProfile(profile) {
		return SessionDescribeClosure{}, ErrInvalid
	}
	shard := task.Ordinals[0] / 128
	expectedTask, err := TaskID(workflow, "session.describe", shard)
	if err != nil || task.ID != expectedTask ||
		task.NodeKey != fmt.Sprintf("session.describe:%d", shard) ||
		task.Ordinals[0] != shard*128 {
		return SessionDescribeClosure{}, ErrInvalid
	}
	compiled, _, err := CompileSessionInputSnapshot(run, snapshot.Items)
	if err != nil || compiled.SnapshotID != snapshot.SnapshotID ||
		compiled.SnapshotDigest != snapshot.SnapshotDigest {
		return SessionDescribeClosure{}, ErrInvalid
	}
	entries := []Entry{
		{Ref: params, Contract: "Params"},
		{Ref: snapshotRef, Contract: "InputSnapshot"},
		{Ref: consents, Contract: "ConsentSnapshot"},
		{Ref: calibration, Contract: "Calibration"},
	}
	for i, ordinal := range task.Ordinals {
		if ordinal < 0 || ordinal >= len(snapshot.Items) ||
			(i > 0 && ordinal != task.Ordinals[i-1]+1) ||
			imageInputs[i].Ordinal != ordinal || !validRef(imageInputs[i].Pixels) {
			return SessionDescribeClosure{}, ErrInvalid
		}
		item := snapshot.Items[ordinal]
		if item.Ordinal != ordinal || item.WorkingImage != item.Binding.Source ||
			!validRef(item.WorkingImage) || !validRef(item.Recipe) {
			return SessionDescribeClosure{}, ErrInvalid
		}
		entries = append(entries,
			Entry{Ref: item.WorkingImage, Contract: "WorkingImage"},
			Entry{Ref: item.Recipe, Contract: "EditingRecipeV3"},
			Entry{Ref: imageInputs[i].Pixels, Contract: "NpyArray"})
		if imageInputs[i].Alpha != nil {
			if !validRef(*imageInputs[i].Alpha) {
				return SessionDescribeClosure{}, ErrInvalid
			}
			entries = append(entries, Entry{Ref: *imageInputs[i].Alpha, Contract: "NpyArray"})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	unique := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if len(unique) > 0 && unique[len(unique)-1].Ref.ArtifactID == entry.Ref.ArtifactID {
			if unique[len(unique)-1] != entry {
				return SessionDescribeClosure{}, ErrInvalid
			}
			continue
		}
		unique = append(unique, entry)
	}
	refs := make([]Ref, len(unique))
	for i, entry := range unique {
		refs[i] = entry.Ref
	}
	digest, err := canonical.Hash(struct {
		Version          string `json:"version"`
		Operation        string `json:"operation"`
		OperationVersion string `json:"operation_version"`
		Renderer         string `json:"renderer"`
		SnapshotDigest   string `json:"snapshot_digest"`
		PolicySHA        string `json:"policy_sha"`
		Params           Ref    `json:"params"`
		Inputs           []Ref  `json:"inputs"`
	}{"task_input_v2", "session.describe", "1", "v2.2",
		snapshot.SnapshotDigest, policySHA, params, refs})
	if err != nil {
		return SessionDescribeClosure{}, err
	}
	return SessionDescribeClosure{
		Open: func(context.Context) (Cursor, error) {
			return &sessionEntryCursor{entries: unique}, nil
		},
		Direct:      []Ref{params, snapshotRef, consents, calibration},
		InputDigest: digest,
		Profile:     profile,
	}, nil
}
