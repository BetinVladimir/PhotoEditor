package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
	"strings"
	"syscall"
)

// AuthoredGeometryGenerationDeployment owns operator-selected locators only.
// It grants neither source authority nor qualification; Prepare verifies both.
type AuthoredGeometryGenerationDeployment struct {
	tenants     []uuid.UUID
	generations map[uuid.UUID]v2input.Ref
}

func (d AuthoredGeometryGenerationDeployment) Inventory() ([]uuid.UUID, map[uuid.UUID]v2input.Ref) {
	t := append([]uuid.UUID(nil), d.tenants...)
	r := make(map[uuid.UUID]v2input.Ref, len(d.generations))
	for k, v := range d.generations {
		r[k] = v
	}
	return t, r
}
func geometryDeploymentExactKeys(raw []byte, keys ...string) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil || len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	return true
}

func parseAuthoredGeometryGenerationDeployment(ctx context.Context, raw []byte) (AuthoredGeometryGenerationDeployment, error) {
	z := AuthoredGeometryGenerationDeployment{}
	if e := ctx.Err(); e != nil {
		return z, e
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return z, v2input.ErrInvalid
	}
	normalized, e := canonical.Bytes(raw)
	if e != nil || !bytes.Equal(normalized, raw) {
		return z, v2input.ErrInvalid
	}
	if !geometryDeploymentExactKeys(raw, "schema_version", "contracts_sha", "tenants") {
		return z, v2input.ErrInvalid
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return z, v2input.ErrInvalid
	}
	var entries []json.RawMessage
	if json.Unmarshal(top["tenants"], &entries) != nil {
		return z, v2input.ErrInvalid
	}
	for _, entry := range entries {
		if !geometryDeploymentExactKeys(entry, "tenant_id", "generation") {
			return z, v2input.ErrInvalid
		}
		var row map[string]json.RawMessage
		if json.Unmarshal(entry, &row) != nil || !geometryDeploymentExactKeys(row["generation"], "artifact_id", "sha256") {
			return z, v2input.ErrInvalid
		}
	}
	var w struct {
		SchemaVersion int    `json:"schema_version"`
		ContractsSHA  string `json:"contracts_sha"`
		Tenants       []struct {
			Tenant     uuid.UUID   `json:"tenant_id"`
			Generation v2input.Ref `json:"generation"`
		} `json:"tenants"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&w) != nil || decoder.Decode(new(any)) != io.EOF || w.SchemaVersion != 1 || w.ContractsSHA != v2render.AuthoredGeometrySchemasSHA || len(w.Tenants) == 0 || len(w.Tenants) > 10000 {
		return z, v2input.ErrInvalid
	}
	tenants := make([]uuid.UUID, 0, len(w.Tenants))
	refs := make(map[uuid.UUID]v2input.Ref, len(w.Tenants))
	for _, entry := range w.Tenants {
		if _, exists := refs[entry.Tenant]; exists {
			return z, v2input.ErrInvalid
		}
		tenants = append(tenants, entry.Tenant)
		refs[entry.Tenant] = entry.Generation
	}
	tenants, refs, e = ownAuthoredGeometryInventory(ctx, tenants, refs)
	if e != nil {
		return z, e
	}
	return AuthoredGeometryGenerationDeployment{tenants: tenants, generations: refs}, nil
}

// ReadAuthoredGeometryGenerationDeployment follows the real native deployment
// reader: private regular file, opened identity, bounded physical SHA and closed
// canonical JSON. No tenant or generation is inferred from a report.
func ReadAuthoredGeometryGenerationDeployment(ctx context.Context, path, pin string) (AuthoredGeometryGenerationDeployment, error) {
	z := AuthoredGeometryGenerationDeployment{}
	if e := ctx.Err(); e != nil {
		return z, e
	}
	decoded, e := hex.DecodeString(pin)
	if path == "" || e != nil || len(decoded) != 32 || len(pin) != 64 || pin != strings.ToLower(pin) {
		return z, v2input.ErrInvalid
	}
	info, e := os.Lstat(path)
	if e != nil {
		return z, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 2 || info.Size() > 1<<20 {
		return z, v2input.ErrInvalid
	}
	f, e := openAuthoredGeometryGenerationDeploymentFile(path, info)
	if e != nil {
		return z, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil {
		return z, e
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 {
		return z, v2input.ErrExpired
	}
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return z, e
	}
	after, e := f.Stat()
	if e != nil {
		return z, e
	}
	current, e := os.Lstat(path)
	if e != nil {
		return z, e
	}
	if !sameAuthoredGeometryGenerationDeploymentFile(opened, after) || !sameAuthoredGeometryGenerationDeploymentFile(opened, current) || int64(len(raw)) != opened.Size() || len(raw) > 1<<20 {
		return z, v2input.ErrExpired
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != pin {
		return z, v2input.ErrInvalid
	}
	return parseAuthoredGeometryGenerationDeployment(ctx, raw)
}

// NewAuthoredGeometryPreparationFromDeployment is concrete future startup
// assembly, not an installed-chain attestor or activation switch.
func NewAuthoredGeometryPreparationFromDeployment(ctx context.Context, scheduler, api *pgxpool.Pool, objects v2input.SessionAnalysisObjectReader, publisher artifactjson.Objects, bucket, nativePath, nativeSHA, generationPath, generationSHA string) (*AuthoredGeometryPreparationProcessor, error) {
	deployment, e := ReadAuthoredGeometryGenerationDeployment(ctx, generationPath, generationSHA)
	if e != nil {
		return nil, e
	}
	tenants, refs := deployment.Inventory()
	return NewAuthoredGeometryPreparationProcessor(ctx, scheduler, api, objects, publisher, bucket, nativePath, nativeSHA, tenants, refs)
}

// Same policy as the existing repository native export deployment loader.
// Access time is intentionally excluded: ordinary reads can update it.
func sameAuthoredGeometryGenerationDeploymentFile(before, current os.FileInfo) bool {
	return before != nil && current != nil && current.Mode().IsRegular() && current.Mode().Perm()&0077 == 0 && current.Size() >= 2 && current.Size() <= 1<<20 && os.SameFile(before, current) && current.Size() == before.Size() && current.Mode() == before.Mode() && current.ModTime().Equal(before.ModTime())
}

// This boundary is separately exercised after an actual Lstat and substitution.
// It accepts no opener callback, trust result, or caller-supplied deployment bytes.
func openAuthoredGeometryGenerationDeploymentFile(path string, before os.FileInfo) (*os.File, error) {
	file, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	opened, e := file.Stat()
	if e != nil {
		file.Close()
		return nil, e
	}
	if !sameAuthoredGeometryGenerationDeploymentFile(before, opened) {
		file.Close()
		return nil, v2input.ErrExpired
	}
	return file, nil
}
