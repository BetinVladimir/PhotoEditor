package v2qualification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/canonical"
	"sort"
)

// MixedChild identifies a separately imported operation report. It is not a
// projection of the full-image parent report into a kernel qualification.
type MixedEnvelope struct {
	RAM     int64 `json:"ram_bytes"`
	VRAM    int64 `json:"vram_bytes"`
	Scratch int64 `json:"scratch_bytes"`
	P95MS   int64 `json:"p95_ms"`
}
type MixedChild struct {
	Qualification uuid.UUID      `json:"qualification_id"`
	Report        uuid.UUID      `json:"report_id"`
	ReportSHA     string         `json:"report_sha256"`
	Operation     string         `json:"operation"`
	Version       string         `json:"version"`
	Backend       string         `json:"backend"`
	Model         string         `json:"model_lock_sha"`
	Runtime       string         `json:"runtime_lock_sha"`
	Device        string         `json:"device_profile_sha"`
	Dataset       string         `json:"dataset_sha"`
	PolicySHA     string         `json:"policy_sha256"`
	Resources     *MixedEnvelope `json:"resource_envelope"`
}
type MixedAuthority struct {
	Schema          int               `json:"schema_version"`
	Scope           string            `json:"scope"`
	Tenant          uuid.UUID         `json:"tenant_id"`
	Project         uuid.UUID         `json:"project_id"`
	Parent          uuid.UUID         `json:"parent_id"`
	Report          uuid.UUID         `json:"parent_report_id"`
	ReportSHA       string            `json:"parent_report_sha256"`
	DependenciesSHA string            `json:"dependencies_sha256"`
	Routes          map[string]string `json:"stage_routes"`
	Children        []MixedChild      `json:"children"`
}

func childKey(c MixedChild) string { return c.Operation + "\x00" + c.Version + "\x00" + c.Backend }
func (a MixedAuthority) Validate(tenant, project uuid.UUID) error {
	if a.Schema != 1 || a.Scope != "renderer.staged.mixed" || tenant == uuid.Nil || project == uuid.Nil || a.Tenant != tenant || a.Project != project || a.Parent == uuid.Nil || a.Report == uuid.Nil || !digest(a.ReportSHA) || !digest(a.DependenciesSHA) || len(a.Children) < 2 || len(a.Children) > 64 || len(a.Routes) < 2 || len(a.Routes) > 128 {
		return ErrUnqualified
	}
	return a.validateDependencies()
}

// Pure dependency validation does not invent tenant/run/parent authority.
func (a MixedAuthority) validateDependencies() error {
	if !digest(a.DependenciesSHA) || len(a.Children) < 2 || len(a.Children) > 64 || len(a.Routes) < 2 || len(a.Routes) > 128 {
		return ErrUnqualified
	}
	seen := map[uuid.UUID]bool{}
	last := ""
	cpu, gpu := false, false
	for _, c := range a.Children {
		key := childKey(c)
		if c.Qualification == uuid.Nil || c.Report == uuid.Nil || seen[c.Qualification] || key <= last || c.Version != "1" || (c.Backend != "cpu" && c.Backend != "mps") || c.Operation == "" || len(c.Operation) > 80 || !digest(c.ReportSHA) || !digest(c.Model) || !digest(c.Runtime) || !digest(c.Device) || !digest(c.Dataset) || !digest(c.PolicySHA) || a.Routes[c.Operation] != c.Backend {
			return ErrUnqualified
		}
		if c.Backend == "mps" {
			if !MPSOperation(c.Operation) || c.Resources == nil {
				return ErrUnqualified
			}
			for _, v := range []int64{c.Resources.RAM, c.Resources.VRAM, c.Resources.Scratch, c.Resources.P95MS} {
				if v < 1 || v > 9007199254740991 {
					return ErrUnqualified
				}
			}
			gpu = true
		} else if c.Resources != nil {
			return ErrUnqualified
		}
		if c.Operation == "render.compose" {
			if c.Backend != "cpu" {
				return ErrUnqualified
			}
			cpu = true
		}
		seen[c.Qualification] = true
		last = key
	}
	for operation, backend := range a.Routes {
		if operation == "" || len(operation) > 80 || (backend != "cpu" && backend != "mps") {
			return ErrUnqualified
		}
		if backend == "mps" {
			found := false
			for _, c := range a.Children {
				if c.Operation == operation && c.Backend == backend {
					found = true
				}
			}
			if !found {
				return ErrUnqualified
			}
		}
	}
	raw, e := canonical.Marshal(a.Children)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	if !cpu || !gpu || hex.EncodeToString(sum[:]) != a.DependenciesSHA {
		return ErrUnqualified
	}
	return nil
}
func DecodeMixedAuthority(raw []byte, tenant, project uuid.UUID) (MixedAuthority, error) {
	var a MixedAuthority
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil {
		return a, ErrUnqualified
	}
	// A second JSON value is never accepted, including whitespace-separated data.
	var extra any
	if d.Decode(&extra) != io.EOF {
		return a, ErrUnqualified
	}
	return a, a.Validate(tenant, project)
}
func (a MixedAuthority) Child(operation, version, backend, model string) (MixedChild, error) {
	for _, c := range a.Children {
		if c.Operation == operation && c.Version == version && c.Backend == backend && c.Model == model {
			return c, nil
		}
	}
	return MixedChild{}, ErrUnqualified
}

// ValidateProfile does not append unknown keys to existing task profile ABIs.
// The immutable run config carries the authority; SQL independently checks it.
// QualifyProfile uses the separately imported child's conservative bounds;
// these are never called a measured VRAM peak and never charged to CPU tasks.
func (a MixedAuthority) QualifyProfile(operation, version string, raw []byte) ([]byte, error) {
	var p map[string]json.RawMessage
	if json.Unmarshal(raw, &p) != nil || p == nil {
		return nil, ErrUnqualified
	}
	var backend, runtime string
	if json.Unmarshal(p["backend"], &backend) != nil || a.Routes[operation] != backend {
		return nil, ErrUnqualified
	}
	if backend != "mps" {
		return raw, nil
	}
	if json.Unmarshal(p["model_lock_id"], &runtime) != nil {
		return nil, ErrUnqualified
	}
	c, e := a.Child(operation, version, backend, runtime)
	if e != nil || c.Resources == nil {
		return nil, ErrUnqualified
	}
	var limits map[string]int64
	if json.Unmarshal(p["resource_estimate"], &limits) != nil || len(limits) < 5 {
		return nil, ErrUnqualified
	}
	for _, v := range limits {
		if v < 0 || v > 9007199254740991 {
			return nil, ErrUnqualified
		}
	}
	for k, want := range map[string]int64{"ram_bytes": c.Resources.RAM, "vram_bytes": c.Resources.VRAM, "scratch_bytes": c.Resources.Scratch} {
		if limits[k] < want {
			limits[k] = want
		}
	}
	out, e := canonical.Marshal(limits)
	if e != nil {
		return nil, e
	}
	p["resource_estimate"] = out
	pin, e := canonical.Marshal(c)
	if e != nil {
		return nil, e
	}
	p["mixed_qualification_pin"] = pin
	return canonical.Marshal(p)
}
func (a MixedAuthority) ValidateProfile(operation, version string, raw []byte) error {
	var p struct {
		Backend string `json:"backend"`
		Runtime string `json:"model_lock_id"`
	}
	if json.Unmarshal(raw, &p) != nil || a.Routes[operation] != p.Backend {
		return ErrUnqualified
	}
	if p.Backend == "mps" {
		c, e := a.Child(operation, version, p.Backend, p.Runtime)
		if e != nil || c.Resources == nil {
			return ErrUnqualified
		}
		var limits struct {
			Estimate map[string]int64 `json:"resource_estimate"`
		}
		var pinned MixedChild
		if json.Unmarshal(raw, &limits) != nil || limits.Estimate["ram_bytes"] < c.Resources.RAM || limits.Estimate["vram_bytes"] < c.Resources.VRAM || limits.Estimate["scratch_bytes"] < c.Resources.Scratch {
			return ErrUnqualified
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if json.Unmarshal(fields["mixed_qualification_pin"], &pinned) != nil {
			return ErrUnqualified
		}
		actual, e := canonical.Marshal(pinned)
		if e != nil {
			return e
		}
		want, e := canonical.Marshal(c)
		if e != nil || !bytes.Equal(actual, want) {
			return ErrUnqualified
		}
	}
	return nil
}

type MixedQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func ResolveMixed(ctx context.Context, tx MixedQuery, tenant, project uuid.UUID) (MixedAuthority, error) {
	var raw []byte
	if e := tx.QueryRow(ctx, `SELECT photo.resolve_mixed_renderer_authority($1,$2)`, tenant, project).Scan(&raw); e != nil {
		return MixedAuthority{}, e
	}
	return DecodeMixedAuthority(raw, tenant, project)
}
func (a MixedAuthority) Canonical() ([]byte, error) {
	if e := a.Validate(a.Tenant, a.Project); e != nil {
		return nil, e
	}
	return canonical.Marshal(a)
}
func SortedChildren(children []MixedChild) []MixedChild {
	out := append([]MixedChild(nil), children...)
	sort.Slice(out, func(i, j int) bool { return childKey(out[i]) < childKey(out[j]) })
	return out
}
