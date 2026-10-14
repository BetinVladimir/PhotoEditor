package provenance

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type signingInstallationNoIO struct{}

type signingWriteOnly struct{ artifactjson.Objects }

func (signingInstallationNoIO) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	panic("constructor performed object IO")
}

func TestPresentationSigningServiceInstallationRequiresPinnedDependencies(t *testing.T) {
	for name, constructor := range map[string]func(PresentationSigningServiceInstallation) (*SQLSignExportService, error){"presentation": NewSQLPresentationSigningService, "render_presentation": NewSQLRenderPresentationSigningService} {
		t.Run(name, func(t *testing.T) {
			testSigningServicePinnedDependencies(t, constructor, name == "render_presentation")
		})
	}
}
func testSigningServicePinnedDependencies(t *testing.T, constructor func(PresentationSigningServiceInstallation) (*SQLSignExportService, error), expectRender bool) {
	for _, mode := range []string{"valid", "api", "scheduler", "same_pool", "reads", "writes", "write_only", "clock", "bucket", "icc", "authority", "report", "sdk_mismatch", "policy"} {
		t.Run(mode, func(t *testing.T) {
			policies, _, now := signingRegistryFixture(t)
			installed := SigningResourceInstallation{RuntimeSHA: strings.Repeat("b", 64), SDKLockSHA: policies.SDKLockSHA, ContractsSHA: strings.Repeat("c", 64), HardwareSHA: strings.Repeat("d", 64)}
			report := SigningResourceMeasurements{Schema: "signing-resource-measurements/v1", Runtime: installed.RuntimeSHA, SDK: installed.SDKLockSHA, Contracts: installed.ContractsSHA, Hardware: installed.HardwareSHA, NotBefore: now.Add(-time.Minute).Format(time.RFC3339Nano), NotAfter: now.Add(time.Minute).Format(time.RFC3339Nano), Measurements: []SigningResourceMeasurement{{WorkloadSHA: strings.Repeat("e", 64), RAM: 64 << 20, Scratch: 1 << 30, Output: (512 << 20) + (16 << 20), InputJSON: 16 << 20}}}
			raw, err := canonical.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			installed.ReportSHA = bodyDigest(raw)
			authority := &signingAuthorityStub{}
			config := PresentationSigningServiceInstallation{API: &pgxpool.Pool{}, Scheduler: &pgxpool.Pool{}, Bucket: "photos", WorkICCSHA: strings.Repeat("f", 64), Reads: signingInstallationNoIO{}, Writes: &signingLineageObjectsStub{}, Policies: policies, Authority: authority, ResourceReport: raw, Resources: installed, Clock: func() time.Time { return now }}
			switch mode {
			case "api":
				config.API = nil
			case "scheduler":
				config.Scheduler = nil
			case "same_pool":
				config.Scheduler = config.API
			case "reads":
				config.Reads = nil
			case "writes":
				config.Writes = nil
			case "write_only":
				config.Writes = signingWriteOnly{Objects: &signingLineageObjectsStub{}}
			case "clock":
				config.Clock = nil
			case "bucket":
				config.Bucket = "x"
			case "icc":
				config.WorkICCSHA = "invalid"
			case "authority":
				config.Authority = nil
			case "report":
				config.ResourceReport = []byte(`{}`)
			case "sdk_mismatch":
				config.Resources.SDKLockSHA = strings.Repeat("f", 64)
			case "policy":
				config.Policies.Policies = nil
			}
			service, err := constructor(config)
			if mode != "valid" {
				if err == nil || service != nil {
					t.Fatal("invalid installation accepted")
				}
				return
			}
			if err != nil || service == nil {
				t.Fatal("installation rejected", err)
			}
			if service.API != config.API || service.Scheduler != config.Scheduler || service.Documents == nil || service.Preparation.Source == nil || service.Preparation.Resources == nil || service.Preparation.Registry == nil {
				t.Fatal("incomplete composition")
			}
			source, ok := service.Preparation.Source.(BoundSigningExportSource)
			if !ok {
				t.Fatal("wrong export source")
			}
			lineage, ok := source.Lineages.(CompiledExportLineageSource)
			if !ok {
				t.Fatal("wrong lineage publisher composition")
			}
			execution, ok := lineage.Execution.(BoundSigningExecutionSource)
			if !ok {
				t.Fatal("wrong execution source")
			}
			_, hasRender := execution.Claims.(RenderPresentationSigningTaskResolver)
			if hasRender != expectRender {
				t.Fatal("wrong operation router installation", hasRender)
			}
			if service.Preparation.Registry.tenant != uuid.Nil {
				t.Fatal("factory globally bound registry to tenant")
			}
			if authority.calls != 0 {
				t.Fatal("constructor invoked native authority")
			}
			raw[0] = 'x'
			policies.Policies[0].Body[0] = 'x'
			for id, installed := range service.Preparation.Registry.policies {
				if _, err := DecodeSigningPolicy(installed.body, installed.ref, id); err != nil {
					t.Fatal("factory retained caller policy bytes", err)
				}
			}
		})
	}
}
