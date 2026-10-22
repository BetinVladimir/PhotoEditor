package main

import (
 "photoeditor/backend/internal/database"
	"context"
	"crypto"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/queue"
	"photoeditor/backend/internal/workflow"
)

// Trusted process bootstrap supplies actual native/root/TSA and mTLS handles.
// No environment-selected module/plugin or synthetic readiness fallback exists.
type apiProcessDependencies struct {
	RestoreReplayReceipt         *queue.RestoreReplayReceiptInstallation
	RestoreReplay                *queue.RestoreReplayInstallation
	InterchangeExport            interchangeExportFactory
	InterchangeExportAuthority   interchange.ExportNativeAuthorityFactory
	InterchangeExportBase        *interchange.ExportDocumentValidatorInstallation
	InterchangeBackgroundFactory interchange.ExportStyleBackgroundAuthorityFactory
	InterchangePersonOrigins     interchange.ExportPersonOriginCandidateFactory
	InterchangePersonOriginTrust map[interchange.PortablePersonOriginKey]ed25519.PublicKey
	InterchangePersonOriginKey   crypto.Signer
	InterchangePersonIssuerID    uuid.UUID
	InterchangePersonProcess     *interchange.ExportPersonOriginProcessInstallation
	InterchangeImport            workflow.SchedulerInterchangeImportFactory
	ImportOperatorFiles          *interchange.ImportSchedulerOperatorFiles
	ImportEntryBase              *interchange.SQLImportEntryRecipeAuthorityFactory
	ImportPersonAcceptance       interchange.ImportMaskQualifier
	SigningTenants               map[uuid.UUID]provenance.SigningAPITenantRuntime
}

func verifySigningRuntimeSQL(ctx context.Context, api *pgxpool.Pool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if api == nil {
		return errors.New("signing runtime database unavailable")
	}
	var ready bool
	err := api.QueryRow(ctx, `SELECT current_user='photo_api' AND count(*)=4 AND coalesce(bool_and(
  p.prosecdef AND owner.rolname='photo_fn_owner'
  AND 'search_path=pg_catalog,photo'=ANY(p.proconfig)
  AND has_function_privilege('photo_api',p.oid,'EXECUTE')
  AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
  AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
    WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')),false)
 FROM (VALUES
  ('photo.v31_ready_signing_policy_version(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text)'::regprocedure),
  ('photo.v31_ready_signing_lineage_version(uuid,uuid,uuid,uuid,jsonb,uuid,text,bigint,text)'::regprocedure),
  ('photo.v31_signing_adopter_runtime_ready(text,text)'::regprocedure),
  ('photo.v31_signing_adopter_unavailable(uuid,uuid,uuid,text)'::regprocedure)
 ) required(oid)
 JOIN pg_proc p ON p.oid=required.oid JOIN pg_roles owner ON owner.oid=p.proowner`).Scan(&ready)
	if err != nil || !ready {
		return errors.New("signing runtime SQL installation unavailable")
	}
	var admissionInstalled bool
	err = api.QueryRow(ctx, `SELECT
  NOT has_function_privilege('photo_api','photo.v31_signing_admission_without_runtime(uuid,uuid,uuid,uuid,jsonb,text,text)'::regprocedure,'EXECUTE')
  AND NOT has_function_privilege('photo_scheduler','photo.v31_signing_admission_without_runtime(uuid,uuid,uuid,uuid,jsonb,text,text)'::regprocedure,'EXECUTE')
  AND position('v31_signing_adopter_runtime_ready' IN pg_get_functiondef('photo.v31_signing_admission_ready(uuid,uuid,uuid,uuid,jsonb,text,text)'::regprocedure))>0
  AND position('SIGNING_ADOPTER_UNAVAILABLE' IN pg_get_functiondef('photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb)'::regprocedure))
    >position('v31_signing_transport_ready' IN pg_get_functiondef('photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb)'::regprocedure))
  AND position('v31_signing_transport_ready' IN pg_get_functiondef('photo.activate_v31_signing_pre_session(uuid,uuid,uuid,text,text,jsonb,jsonb)'::regprocedure))>0`).Scan(&admissionInstalled)
	if err != nil || !admissionInstalled {
		return errors.New("signing admission SQL installation unavailable")
	}
	return nil
}

func schedulerSigningFactory(path, pin string,
	dependencies apiProcessDependencies) workflow.SchedulerDeliverySigningFactory {
	return func(ctx context.Context, databaseURL string, scheduler *pgxpool.Pool,
		objects *objectstore.Store, bucket, workICC string,
	) (workflow.DeliverySigningRuntime, func(), error) {
		var zero workflow.DeliverySigningRuntime
		if err := ctx.Err(); err != nil {
			return zero, nil, err
		}
		if err := validateAPISigningDeclaration(path, pin, dependencies); err != nil {
			return zero, nil, err
		}
		if path == "" || scheduler == nil || objects == nil {
			return zero, nil, errors.New("signing scheduler deployment unavailable")
		}
		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			return zero, nil, err
		}
		config.ConnConfig.RuntimeParams["role"] = "photo_api"
		database.GuardApplicationRole(config)
		api, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			return zero, nil, err
		}
		success := false
		var installed *provenance.SigningSchedulerDeployment
		defer func() {
			if !success {
				installed.Close()
				api.Close()
			}
		}()
		var roleOK bool
		if err := api.QueryRow(ctx, `SELECT current_user='photo_api' AND NOT rolsuper AND NOT rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&roleOK); err != nil || !roleOK {
			return zero, nil, errors.New("signing API role unavailable to scheduler")
		}
		if err := verifySigningRuntimeSQL(ctx, api); err != nil {
			return zero, nil, err
		}
		installed, err = provenance.InstallSigningSchedulerDeploymentFile(ctx,
			path, pin, provenance.SigningAPIDeploymentRuntime{
				Base: provenance.PresentationSigningServiceInstallation{
					API: api, Scheduler: scheduler, Reads: objects, Writes: objects,
					Bucket: bucket, WorkICCSHA: workICC, Clock: time.Now,
				}, Tenants: dependencies.SigningTenants,
			})
		if err != nil {
			return zero, nil, err
		}
		runtime, err := workflow.DeliverySigningRuntimeFromDeployment(scheduler,
			objects, objects, objects, bucket, workICC, time.Now, installed)
		if err != nil {
			return zero, nil, err
		}
		success = true
		return runtime, func() { installed.Close(); api.Close() }, nil
	}
}

type apiSigningInstallation struct {
	Router    *provenance.TenantSigningRouter
	Scheduler *pgxpool.Pool
}

func (s *apiSigningInstallation) Close() {
	if s != nil {
		if s.Router != nil {
			s.Router.Close()
		}
		if s.Scheduler != nil {
			s.Scheduler.Close()
		}
	}
}
func validateAPISigningDeclaration(path, pin string, dependencies apiProcessDependencies) error {
	if path == "" && pin == "" {
		if len(dependencies.SigningTenants) > 0 {
			return errors.New("signing runtime requires pinned deployment declaration")
		}
		return nil
	}
	decoded, err := hex.DecodeString(pin)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != pin {
		return errors.New("signing deployment requires absolute file and lowercase SHA256")
	}
	if len(dependencies.SigningTenants) == 0 {
		return errors.New("signing deployment requires installed native trust/TSA and API identity handles")
	}
	return nil
}
func installAPISigning(ctx context.Context, path, pin, databaseURL string, api *pgxpool.Pool, objects *objectstore.Store, bucket, workICC string, dependencies apiProcessDependencies) (*apiSigningInstallation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateAPISigningDeclaration(path, pin, dependencies); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("signing scheduler configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	database.GuardApplicationRole(config)
	scheduler, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("signing scheduler unavailable")
	}
	installed := &apiSigningInstallation{Scheduler: scheduler}
	success := false
	defer func() {
		if !success {
			installed.Close()
		}
	}()
	var roleOK bool
	if err = scheduler.QueryRow(ctx, `SELECT current_user='photo_scheduler' AND NOT rolsuper AND NOT rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&roleOK); err != nil || !roleOK {
		return nil, errors.New("signing scheduler role invalid")
	}
	if err := verifySigningRuntimeSQL(ctx, api); err != nil {
		return nil, err
	}
	installed.Router, err = provenance.InstallSigningAPIDeploymentFile(ctx, path, pin, provenance.SigningAPIDeploymentRuntime{
		Base: provenance.PresentationSigningServiceInstallation{API: api, Scheduler: scheduler, Bucket: bucket, WorkICCSHA: workICC, Reads: objects, Writes: objects, Clock: time.Now}, Tenants: dependencies.SigningTenants})
	if err != nil {
		return nil, err
	}
	success = true
	return installed, nil
}
