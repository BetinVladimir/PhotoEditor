package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Immutable locators, not authority. AuthoritySHA binds the complete original
// private proof including scope, policy, physical evidence and object versions.
// Recovery must reread and requalify that exact proof before comparing locators.
type nativeDefectPersonRecovery struct {
	Schema       int                       `json:"schema_version"`
	Root         v2input.Ref               `json:"root"`
	Pixels       v2input.Ref               `json:"pixels"`
	Report       v2input.Ref               `json:"qualification_report"`
	AuthoritySHA string                    `json:"authority_sha"`
	Files        []nativeSkinCompletedFile `json:"files"`
}

func nativeDefectPersonRecoveryFromProof(ctx context.Context, p nativePersonInputAuthority) (nativeDefectPersonRecovery, error) {
	var zero nativeDefectPersonRecovery
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	digest, err := nativePersonInputAuthorityDigest(p)
	if err != nil {
		return zero, err
	}
	if p.seal == "" || p.seal != digest {
		return zero, v2input.ErrConflict
	}
	m := nativeDefectPersonRecovery{Schema: 1, Root: p.root, Pixels: p.pixels, Report: p.report, AuthoritySHA: digest, Files: make([]nativeSkinCompletedFile, 2)}
	for i, ref := range []v2input.Ref{p.root, p.pixels} {
		role := "person_root"
		if i == 1 {
			role = "person_pixels"
		}
		obj := p.objects[i]
		if obj.SHA256 != ref.SHA256 {
			return zero, v2input.ErrConflict
		}
		m.Files[i] = nativeSkinCompletedFile{Role: role, Ref: ref, Bucket: p.bucket, Key: obj.Key, Version: obj.VersionID, Bytes: obj.Bytes, Media: obj.ContentType}
	}
	raw, err := canonical.Marshal(m)
	if err != nil {
		return zero, err
	}
	return decodeNativeDefectPersonRecovery(raw)
}

func decodeNativeDefectPersonRecovery(raw json.RawMessage) (nativeDefectPersonRecovery, error) {
	var zero nativeDefectPersonRecovery
	if len(raw) < 2 || len(raw) > 32768 {
		return zero, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var m nativeDefectPersonRecovery
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil || decoder.Decode(new(any)) != io.EOF || m.Schema != 1 || len(m.Files) != 2 {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(m.AuthoritySHA); err != nil {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{m.Root, m.Pixels, m.Report} {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	for i, ref := range []v2input.Ref{m.Root, m.Pixels} {
		f := m.Files[i]
		role := "person_root"
		if i == 1 {
			role = "person_pixels"
		}
		if f.Role != role || f.Ref != ref || f.Bucket == "" || len(f.Bucket) > 255 || f.Bucket != m.Files[0].Bucket || f.Key == "" || len(f.Key) > 1024 || f.Version == "" || f.Version == "null" || len(f.Version) > 8192 || f.Bytes < 1 || f.Bytes > 9007199254740991 {
			return zero, v2input.ErrInvalid
		}
		if i == 0 && (f.Media != "application/json" || f.Bytes < 2 || f.Bytes > 1<<20) {
			return zero, v2input.ErrInvalid
		}
		if i == 1 && f.Media != "application/x-npy" && f.Media != "application/octet-stream" {
			return zero, v2input.ErrInvalid
		}
	}
	return m, nil
}

func matchNativeDefectPersonRecovery(ctx context.Context, m nativeDefectPersonRecovery, p nativePersonInputAuthority) error {
	actual, err := nativeDefectPersonRecoveryFromProof(ctx, p)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, m) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) restoreNativeDefectPersonInput(ctx context.Context, who auth.Identity, request Request, m nativeDefectPersonRecovery) (nativePersonInputAuthority, error) {
	var zero nativePersonInputAuthority
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	raw, err := canonical.Marshal(m)
	if err != nil {
		return zero, err
	}
	if _, err := decodeNativeDefectPersonRecovery(raw); err != nil {
		return zero, err
	}
	p, err := s.readNativePersonInputAuthorityCandidate(ctx, who, request, m.Report, &personCandidate{root: m.Root, pixels: m.Pixels})
	if err != nil {
		return zero, err
	}
	if err := matchNativeDefectPersonRecovery(ctx, m, p); err != nil {
		return zero, err
	}
	return p, nil
}
