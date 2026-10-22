package provenance

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
)

func TestDeploymentRuntimeResolutionBeforeIO(t *testing.T) {
	for _, mode := range []string{"valid", "tenant", "worker", "qualification", "identity_id", "identity_key", "native", "base_authority", "same_pool", "cancelled", "tsa_valid", "tsa_missing", "tsa_conflict"} {
		t.Run(mode, func(t *testing.T) {
			server, identity, _, serverPin := operatorMTLSFixture(t)
			chain := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate[0]})
			roots := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate[1]})
			config, _, now := signingRegistryFixture(t)
			var policy SigningPolicy
			if err := json.Unmarshal(config.Policies[0].Body, &policy); err != nil {
				t.Fatal(err)
			}
			policy.CertificateChainSHA = bodyDigest(chain)
			policy.TrustBundleSHA = bodyDigest(roots)
			policy.TimestampMode = "off"
			policy.TimestampServiceID = nil
			policy.RevocationMode = "off"
			if mode == "tsa_valid" || mode == "tsa_missing" || mode == "tsa_conflict" {
				id := "installed-tsa"
				policy.TimestampMode = "required"
				policy.TimestampServiceID = &id
			}
			body, err := canonical.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			report, err := canonical.Marshal(SigningResourceMeasurements{Schema: "signing-resource-measurements/v1", SDK: policy.SDKLockSHA, Runtime: serverPin, Contracts: serverPin, Hardware: serverPin, NotBefore: now.Add(-time.Hour).Format(time.RFC3339Nano), NotAfter: now.Add(time.Hour).Format(time.RFC3339Nano), Measurements: []SigningResourceMeasurement{{WorkloadSHA: serverPin, RAM: 64 << 20, Scratch: 1 << 30, Output: (512 << 20) + (16 << 20), InputJSON: 16 << 20}}})
			if err != nil {
				t.Fatal(err)
			}
			entry := SigningAPITenantDocument{TenantID: uuid.New(), WorkerID: uuid.New(), QualificationSHA: serverPin, CertificateChain: string(chain), TrustBundle: string(roots), CertificateUsages: []int{int(x509.ExtKeyUsageCodeSigning)}, ChallengeOrigin: "https://signer.example", ChallengeServerSHA: serverPin, ChallengeRoots: string(roots), IdentityID: "identity", TimeoutSeconds: 10, CRLEndpoints: []SigningAPICRLDocument{}, Policies: []SigningAPIPolicyDocument{{Body: body, NotBefore: now.Add(-time.Hour).Format(time.RFC3339Nano), NotAfter: now.Add(time.Hour).Format(time.RFC3339Nano)}}, ResourceReport: report, ResourceReportSHA: bodyDigest(report), NegativePolicies: json.RawMessage("null")}
			if mode == "tsa_valid" || mode == "tsa_missing" || mode == "tsa_conflict" {
 entry.TimestampServices = []SigningAPITimestampDocument{{ID:"installed-tsa",URL:"https://tsa.example/issue",ServerSHA:serverPin,HTTPRoots:string(roots),TrustRoots:string(roots),PolicyOID:"1.2.3.4",TimeoutSeconds:10,MaxSkewSeconds:60,CRLRoots:string(roots),CRLMaxAgeSeconds:3600,CRLEndpoints:[]SigningAPICRLDocument{{IssuerSHA:serverPin,URL:"https://crl.example/list",ServerSHA:serverPin}}}}
 }
 raw, err := canonical.Marshal(SigningAPIDeploymentDocument{Schema: "signing-api-deployment/v1", Tenants: []SigningAPITenantDocument{entry}})
			if err != nil {
				t.Fatal(err)
			}
			runtime := SigningAPITenantRuntime{WorkerID: entry.WorkerID, QualificationSHA: entry.QualificationSHA, IdentityID: entry.IdentityID, Identity: identity, Native: func(context.Context, SigningPolicy, []byte) error { t.Fatal("preparation invoked native"); return nil }}
			if mode == "tsa_valid" || mode == "tsa_missing" || mode == "tsa_conflict" {
				runtime.TimestampServices = map[string]SigningTrustCheck{"installed-tsa": func(context.Context, SigningPolicy, []byte) error { t.Fatal("preparation invoked TSA"); return nil }}
				if mode == "tsa_missing" {
					delete(runtime.TimestampServices, "installed-tsa")
				}
				if mode == "tsa_conflict" {
					runtime.Timestamp = runtime.TimestampServices["installed-tsa"]
				}
			}
			base := PresentationSigningServiceInstallation{API: &pgxpool.Pool{}, Scheduler: &pgxpool.Pool{}, Bucket: "photos", WorkICCSHA: serverPin, Reads: signingInstallationNoIO{}, Writes: &signingLineageObjectsStub{}, Clock: func() time.Time { return now }}
			switch mode {
			case "worker":
				runtime.WorkerID = uuid.New()
			case "qualification":
				runtime.QualificationSHA = policy.SDKLockSHA
			case "identity_id":
				runtime.IdentityID = "other"
			case "identity_key":
				runtime.Identity.PrivateKey = nil
			case "native":
				runtime.Native = nil
			case "base_authority":
				base.Authority = &signingAuthorityStub{}
			case "same_pool":
				base.Scheduler = base.API
			}
			handles := map[uuid.UUID]SigningAPITenantRuntime{entry.TenantID: runtime}
			if mode == "tenant" {
				handles = map[uuid.UUID]SigningAPITenantRuntime{}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			prepared, err := prepareSigningAPIDeployment(ctx, raw, bodyDigest(raw), SigningAPIDeploymentRuntime{Base: base, Tenants: handles})
			if (err == nil) != (mode == "valid" || mode == "tsa_valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if mode == "valid" || mode == "tsa_valid" {
				if len(prepared) != 1 || prepared[0].Operator.Registration.TenantID != entry.TenantID || prepared[0].Service.Policies.CertificateChainSHA != policy.CertificateChainSHA || prepared[0].Service.Resources.ReportSHA != entry.ResourceReportSHA {
					t.Fatal("incomplete runtime resolution")
				}
				identity.Certificate[0][0] = '!'
				raw[0] = '!'
				if prepared[0].Operator.ChallengeIdentity.Certificate[0][0] == '!' || string(prepared[0].Service.Policies.Policies[0].Body) != string(body) {
					t.Fatal("runtime/document ownership")
				}
			}
		})
	}
}
