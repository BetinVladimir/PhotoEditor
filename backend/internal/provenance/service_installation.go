package provenance

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
)

// Trusted deployment inputs, never HTTP/request fields. This constructor does
// not provision keys, qualify SDKs or invent resource reports. The installation
// authority must independently verify the live operator on every invocation.
type PresentationSigningServiceInstallation struct {
	API, Scheduler   *pgxpool.Pool
	Bucket           string
	WorkICCSHA       string
	Reads            SigningObjects
	Writes           artifactjson.Objects
	Policies         SigningInstallation
	NegativePolicies []InstalledSigningNegativePolicy
	Authority        SigningOperatorAuthority
	ResourceReport   []byte
	Resources        SigningResourceInstallation
	Clock            func() time.Time
}

// NewSQLPresentationSigningService composes the restricted presentation-only
// ancestry path with measured resources, tenant policy/lineage publication,
// durable signing admission and transport. Unsupported render/generative/optics
// ancestry remains refused until its own authoritative claims reader exists.
// Callers must explicitly install the returned service into HTTP Server.
// Pool credentials/DB migrations/native operator readiness require deployment
// checks; distinct pool pointers alone cannot prove their PostgreSQL roles.
func NewSQLPresentationSigningService(config PresentationSigningServiceInstallation) (*SQLSignExportService, error) {
	return newSQLSigningService(config, NewSQLPresentationSigningExportSource)
}

// NewSQLRenderPresentationSigningService retains all operator/resource gates
// while using render-capable lineage. HTTP/native deployment remains explicit.
func NewSQLRenderPresentationSigningService(config PresentationSigningServiceInstallation) (*SQLSignExportService, error) {
	if config.NegativePolicies != nil {
		return NewSQLNegativeRenderPresentationSigningService(config, config.NegativePolicies)
	}
	return newSQLSigningService(config, NewSQLRenderPresentationSigningExportSource)
}

type signingExportSourceFactory func(*pgxpool.Pool, string, SigningObjects, artifactjson.Objects, string) (SigningExportSource, error)

func newSQLSigningService(config PresentationSigningServiceInstallation, sourceFactory signingExportSourceFactory) (*SQLSignExportService, error) {
	if sourceFactory == nil || config.API == nil || config.Scheduler == nil || config.API == config.Scheduler || config.Reads == nil || config.Writes == nil || config.Clock == nil || len(config.Bucket) < 3 || !text(config.Bucket, 63) || !digest(config.WorkICCSHA) || config.Resources.SDKLockSHA != config.Policies.SDKLockSHA {
		return nil, ErrInvalid
	}
	if _, ok := config.Writes.(signingPolicyVersionReader); !ok {
		return nil, ErrInvalid
	}
	registry, err := NewSigningPolicyRegistry(config.Policies, config.Authority)
	if err != nil {
		return nil, err
	}
	resources, err := NewMeasuredSigningResourceQualifier(config.ResourceReport, config.Resources, registry, config.Clock)
	if err != nil {
		return nil, err
	}
	source, err := sourceFactory(config.API, config.Bucket, config.Reads, config.Writes, config.WorkICCSHA)
	if err != nil {
		return nil, err
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: config.Scheduler, Bucket: config.Bucket}, Objects: config.Writes}
	return &SQLSignExportService{
		API: config.API, Scheduler: config.Scheduler,
		Preparation: SigningActivationPreparation{Source: source, Registry: registry, Resources: resources, Clock: config.Clock},
		Catalog:     SQLSigningExportCatalog{DB: config.API, Bucket: config.Bucket}, Objects: config.Writes, Documents: documents, Bucket: config.Bucket, Clock: config.Clock,
	}, nil
}
