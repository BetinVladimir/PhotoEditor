package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

type semanticSourceKey struct {
	Ref      Ref
	Contract string
}
type semanticSourceLocation struct {
	Bucket string
	Object objectstore.Object
}

// Qualification must verify the installed calibration/model binding; ready
// object metadata alone does not establish algorithm or GPU qualification.
type SQLSemanticAssemblySource struct {
	DB            *pgxpool.Pool
	Objects       SessionAnalysisObjectReader
	Bucket        string
	Qualification func(context.Context, SemanticAssemblyLease, Ref, []byte, string) error
}

func (s SQLSemanticAssemblySource) Load(ctx context.Context, l SemanticAssemblyLease, results []SemanticTransferResult) (SemanticAssemblyMaterial, error) {
	var empty SemanticAssemblyMaterial
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || s.Qualification == nil || !validSemanticAssemblyLease(l) || len(results) < 1 || len(results) > 5000 {
		return empty, ErrInvalid
	}
	rows, err := s.DB.Query(ctx, `SELECT task_id,artifact_id,sha256,contract,bucket,object_key,object_version,bytes,media_type FROM photo.v27_semantic_assembly_source_inventory($1,$2,$3,$4)`, l.TenantID, l.RunID, l.ExpansionID, l.Token)
	if err != nil {
		return empty, err
	}
	locations := map[semanticSourceKey]semanticSourceLocation{}
	params := map[uuid.UUID]Ref{}
	roots := map[uuid.UUID]Ref{}
	var snapshot Ref
	for rows.Next() {
		var task *uuid.UUID
		var key semanticSourceKey
		var loc semanticSourceLocation
		if err := rows.Scan(&task, &key.Ref.ArtifactID, &key.Ref.SHA256, &key.Contract, &loc.Bucket, &loc.Object.Key, &loc.Object.VersionID, &loc.Object.Bytes, &loc.Object.ContentType); err != nil {
			rows.Close()
			return empty, err
		}
		limit := int64(1 << 20)
		if key.Contract == "InputSnapshot" {
			limit = 8 << 20
		}
		if !validRef(key.Ref) || loc.Bucket != s.Bucket || loc.Object.Key == "" || loc.Object.VersionID == "" || loc.Object.VersionID == "null" || loc.Object.ContentType != "application/json" || loc.Object.Bytes < 2 || loc.Object.Bytes > limit || len(locations) > 25000 {
			rows.Close()
			return empty, ErrInvalid
		}
		loc.Object.SHA256 = key.Ref.SHA256
		if prior, ok := locations[key]; ok && prior != loc {
			rows.Close()
			return empty, ErrInvalid
		}
		locations[key] = loc
		switch key.Contract {
		case "Params", "TransferCandidate":
			if task == nil {
				rows.Close()
				return empty, ErrInvalid
			}
			dst := params
			if key.Contract == "TransferCandidate" {
				dst = roots
			}
			if prior, ok := dst[*task]; ok && prior != key.Ref {
				rows.Close()
				return empty, ErrInvalid
			}
			dst[*task] = key.Ref
		case "InputSnapshot":
			if task != nil || snapshot.ArtifactID != uuid.Nil && snapshot != key.Ref {
				rows.Close()
				return empty, ErrInvalid
			}
			snapshot = key.Ref
		case "EditIntent", "Calibration", "WorkingImage":
		default:
			rows.Close()
			return empty, ErrInvalid
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	if !validRef(snapshot) || len(params) != len(results) || len(roots) != len(results) {
		return empty, ErrExpired
	}
	cache := map[semanticSourceKey][]byte{}
	var total int64
	read := func(ref Ref, contract string) ([]byte, error) {
		key := semanticSourceKey{ref, contract}
		if raw, ok := cache[key]; ok {
			return raw, nil
		}
		loc, ok := locations[key]
		if !ok {
			return nil, ErrInvalid
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if total+loc.Object.Bytes > 256<<20 {
			return nil, ErrInvalid
		}
		raw, err := readSemanticSourceBytes(ctx, s.Objects, loc.Object, ref)
		if err != nil {
			return nil, err
		}
		total += int64(len(raw))
		cache[key] = raw
		return raw, nil
	}
	snapshotBody, err := read(snapshot, "InputSnapshot")
	if err != nil {
		return empty, err
	}
	if _, err := DecodePinnedSessionInputSnapshot(snapshot, snapshotBody); err != nil {
		return empty, err
	}
	material := SemanticAssemblyMaterial{Snapshot: snapshot, SnapshotBody: snapshotBody}
	seen := map[uuid.UUID]bool{}
	for i, result := range results {
		expected, e := TaskID(l.WorkflowID, "semantic.transfer", i)
		if e != nil || result.Ordinal != i || result.TaskID != expected || roots[result.TaskID] != result.Candidate || seen[result.TaskID] {
			return empty, ErrInvalid
		}
		seen[result.TaskID] = true
		raw, err := read(params[result.TaskID], "Params")
		if err != nil {
			return empty, err
		}
		p, err := DecodeSemanticTransferParams(raw)
		if err != nil {
			return empty, err
		}
		if i == 0 {
			intentRaw, err := read(p.Intent, "EditIntent")
			if err != nil {
				return empty, err
			}
			intent, err := DecodeSemanticEditIntent(intentRaw)
			if err != nil {
				return empty, err
			}
			calibration, err := read(p.Calibration, "Calibration")
			if err != nil {
				return empty, err
			}
			if err := s.Qualification(ctx, l, p.Calibration, append([]byte(nil), calibration...), p.ModelLockSHA); err != nil {
				return empty, err
			}
			material.IntentRef = p.Intent
			material.Intent = intent
			material.Calibration = p.Calibration
			material.ModelLockSHA = p.ModelLockSHA
		}
		if p.Intent != material.IntentRef || p.Calibration != material.Calibration || p.ModelLockSHA != material.ModelLockSHA {
			return empty, ErrInvalid
		}
		if _, _, err := BindSemanticTransferIntent(p, material.Intent); err != nil {
			return empty, err
		}
		candidateRaw, err := read(result.Candidate, "TransferCandidate")
		if err != nil {
			return empty, err
		}
		candidate, err := DecodeSemanticCandidate(candidateRaw)
		if err != nil || candidate.Target != p.Target {
			return empty, ErrInvalid
		}
		targetBody, err := read(p.Target.Source, "WorkingImage")
		if err != nil {
			return empty, err
		}
		material.Candidates = append(material.Candidates, SemanticCandidateDocument{Ref: result.Candidate, Body: candidateRaw, TargetBody: targetBody})
	}
	return material, nil
}

func readSemanticSourceBytes(ctx context.Context, objects SessionAnalysisObjectReader, obj objectstore.Object, ref Ref) ([]byte, error) {
	return ReadPinnedSemanticDocument(ctx, objects, obj, ref)
}

// ReadPinnedSemanticDocument reads exact versioned JSON bytes with an 8 MiB
// bound, verifies declared size and SHA and closes the stream on every read exit.
// The caller must resolve the object through an authorized scoped catalog.
func ReadPinnedSemanticDocument(ctx context.Context, objects SessionAnalysisObjectReader, obj objectstore.Object, ref Ref) ([]byte, error) {
	if objects == nil || !validRef(ref) || obj.SHA256 != ref.SHA256 || obj.VersionID == "" || obj.VersionID == "null" || obj.ContentType != "application/json" || obj.Bytes < 2 || obj.Bytes > 8<<20 {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, obj.Bytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	if int64(len(raw)) != obj.Bytes || hex.EncodeToString(hash[:]) != ref.SHA256 {
		return nil, ErrInvalid
	}
	return raw, nil
}
