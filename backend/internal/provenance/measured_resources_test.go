package provenance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

func TestMeasuredSigningResourcesRequireExactWorkloadAndInstallation(t *testing.T) {
	config, params, now := signingRegistryFixture(t)
	authority := &signingAuthorityStub{}
	registry, err := NewSigningPolicyRegistry(config, authority)
	if err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	template := registry
	registry, err = registry.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	body, lineage := contractBytes(t, SigningLineage{Schema: 1, SourceHashes: []string{params.Export.SHA256}, Actions: []SigningAction{}})
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	export := uuid.New()
	evidence := SigningExportEvidence{Project: uuid.New(), ExportID: export, Export: params.Export, MIME: "image/jpeg", Lineage: lineage, LineageBody: body}
	intent, err := PrepareSigningIntent(context.Background(), who, tenant, export, params.PolicyID, "measured", &signingSourceStub{evidence: evidence}, registry, now)
	if err != nil {
		t.Fatal(err)
	}
	workload, err := SigningResourceWorkloadSHA(intent, evidence)
	if err != nil {
		t.Fatal(err)
	}
	install := SigningResourceInstallation{RuntimeSHA: strings.Repeat("b", 64), SDKLockSHA: config.SDKLockSHA, ContractsSHA: strings.Repeat("c", 64), HardwareSHA: strings.Repeat("d", 64)}
	measurement := SigningResourceMeasurement{WorkloadSHA: workload, RAM: 64 << 20, Scratch: 1 << 30, Output: (512 << 20) + (16 << 20), InputJSON: 16 << 20}
	report := SigningResourceMeasurements{Schema: "signing-resource-measurements/v1", Runtime: install.RuntimeSHA, SDK: install.SDKLockSHA, Contracts: install.ContractsSHA, Hardware: install.HardwareSHA, NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(time.Minute).Format(time.RFC3339Nano), Measurements: []SigningResourceMeasurement{measurement}}
	raw, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	install.ReportSHA = bodyDigest(raw)
	clock := now
	qualifier, err := NewMeasuredSigningResourceQualifier(raw, install, template, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	resources, err := qualifier.QualifySigningResources(context.Background(), who, intent, evidence)
	if err != nil || resources.RAMBytes != measurement.RAM || resources.ContractsSHA != install.ContractsSHA {
		t.Fatal("exact measurement rejected", err)
	}
	delegated := cloneSigningIntent(intent)
	delegated.delegated = true
	delegated.session = uuid.Nil
	ready := ReadySigningExport{Tenant: tenant, Project: evidence.Project, ExportID: export,
		Version: uuid.New(), Task: uuid.New(), Run: uuid.New(), Ref: evidence.Export,
		Object: objectstore.Object{SHA256: evidence.Export.SHA256, Key: "export.jpg",
			VersionID: "immutable-v1", Bytes: 4096, ContentType: evidence.MIME}}
	if got, err := qualifier.QualifyDelegatedSigningResources(context.Background(), who.UserID, delegated, ready); err != nil || got != resources {
		t.Fatal("exact delegated workload rejected", err)
	}
	for _, mode := range []string{"actor", "session", "tenant", "export", "version", "digest", "mime", "policy", "operator", "cancelled"} {
		t.Run("delegated_"+mode, func(t *testing.T) {
			i, r, actor, ctx := cloneSigningIntent(delegated), ready, who.UserID, context.Background()
			authority.err = nil
			switch mode {
			case "actor": actor = uuid.New()
			case "session": i.session = who.SessionID
			case "tenant": r.Tenant = uuid.New()
			case "export": r.ExportID = uuid.New()
			case "version": r.Object.VersionID = "null"
			case "digest": r.Object.SHA256 = strings.Repeat("f", 64)
			case "mime": r.Object.ContentType = "image/png"
			case "policy": i.request = []byte(`{}`)
			case "operator": authority.err = ErrInvalid
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := qualifier.QualifyDelegatedSigningResources(ctx, actor, i, r); err == nil {
				t.Fatal("invalid delegated workload accepted")
			}
			authority.err = nil
		})
	}
	foreignRegistry, err := template.ForTenant(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	foreignQualifier := *qualifier
	foreignQualifier.registry = foreignRegistry
	before := authority.calls
	if _, err := foreignQualifier.QualifySigningResources(context.Background(), who, intent, evidence); err == nil {
		t.Fatal("foreign tenant resource registry accepted")
	}
	if authority.calls != before {
		t.Fatal("foreign tenant reached operator")
	}
	raw[0] = 'x'
	if _, err := qualifier.QualifySigningResources(context.Background(), who, intent, evidence); err != nil {
		t.Fatal("retained caller report", err)
	}
	for _, mode := range []string{"actor", "session", "tenant", "mime", "export", "lineage", "lineage_body", "request", "expired", "future", "operator", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			w, i, e, ctx := who, cloneSigningIntent(intent), evidence, context.Background()
			clock = now
			authority.err = nil
			switch mode {
			case "actor":
				w.UserID = uuid.New()
			case "session":
				w.SessionID = uuid.New()
			case "tenant":
				i.tenant = uuid.New()
			case "mime":
				e.MIME = "image/png"
			case "export":
				e.Export.ArtifactID = uuid.New()
			case "lineage":
				e.Lineage.ArtifactID = uuid.New()
			case "lineage_body":
				e.LineageBody = []byte(`{}`)
			case "request":
				i.request = []byte(`{}`)
			case "expired":
				clock = now.Add(time.Minute)
			case "future":
				clock = now.Add(-2 * time.Minute)
			case "operator":
				authority.err = ErrInvalid
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := qualifier.QualifySigningResources(ctx, w, i, e); err == nil {
				t.Fatal("unqualified workload accepted")
			}
		})
	}
	clock = now
	authority.err = nil
	for _, mode := range []string{"pin", "runtime", "sdk", "contracts", "hardware", "duplicate", "empty", "schema", "window", "unsafe", "scratch", "output", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			r := report
			r.Measurements = append([]SigningResourceMeasurement(nil), report.Measurements...)
			installed := install
			switch mode {
			case "runtime":
				installed.RuntimeSHA = strings.Repeat("f", 64)
			case "sdk":
				installed.SDKLockSHA = strings.Repeat("f", 64)
			case "contracts":
				installed.ContractsSHA = strings.Repeat("f", 64)
			case "hardware":
				installed.HardwareSHA = strings.Repeat("f", 64)
			case "duplicate":
				r.Measurements = append(r.Measurements, measurement)
			case "empty":
				r.Measurements = nil
			case "schema":
				r.Schema = "unknown"
			case "window":
				r.NotAfter = r.NotBefore
			case "unsafe":
				r.Measurements[0].RAM = 9007199254740992
			case "scratch":
				r.Measurements[0].Scratch = 1
			case "output":
				r.Measurements[0].Output = 1
			}
			data, err := canonical.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "unknown" {
				data = append([]byte(`{"unknown":true,`), data[1:]...)
				data, err = canonical.Bytes(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			installed.ReportSHA = bodyDigest(data)
			if mode == "pin" {
				installed.ReportSHA = strings.Repeat("f", 64)
			}
			if _, err := NewMeasuredSigningResourceQualifier(data, installed, registry, func() time.Time { return now }); err == nil {
				t.Fatal("invalid deployment report accepted")
			}
		})
	}
}
