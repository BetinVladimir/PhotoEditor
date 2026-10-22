package maintenance

import (
 "photoeditor/backend/internal/database"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net"
	"net/http"
	"os"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"sync/atomic"
	"time"
)

type CleanupJob interface {
	CleanOne(context.Context) (bool, error)
}
type cleanupCycleBudget interface{ CleanupCycleTimeout() time.Duration }

func cleanupCycleTimeout(job CleanupJob) time.Duration {
	if budget, ok := job.(cleanupCycleBudget); ok {
		return budget.CleanupCycleTimeout()
	}
	return 50 * time.Second
}

type LoopState struct {
	Ready             atomic.Bool
	Cleaned, Failures atomic.Uint64
}

type tenantBucketReadiness interface{ Ready(context.Context) error }

// Shared across startup and concurrent health requests. A stalled target may
// outlive its caller, but it cannot create unbounded in-flight storage probes.
var tenantReadinessSlots = make(chan struct{}, 32)

func probeTenantBucketReady(ctx context.Context, store tenantBucketReadiness) (err error) {
	defer func() {
		<-tenantReadinessSlots
		if recover() != nil {
			err = errors.New("tenant purge bucket readiness failed")
		}
	}()
	return store.Ready(ctx)
}

func readyTenantBuckets(ctx context.Context, stores []tenantBucketReadiness) error {
	if len(stores) == 0 || len(stores) > 256 {
		return errors.New("invalid tenant purge bucket readiness")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, store := range stores {
		if store == nil {
			return errors.New("missing tenant purge bucket")
		}
	}
	// Bound concurrent network probes while ensuring every configured target is
	// checked within the shared startup/health deadline.
	results := make(chan error, len(stores))
	jobs := make(chan tenantBucketReadiness, len(stores))
	for _, store := range stores {
		jobs <- store
	}
	close(jobs)
	workers := len(stores)
	if workers > cap(tenantReadinessSlots) {
		workers = cap(tenantReadinessSlots)
	}
	for range workers {
		go func() {
			for store := range jobs {
				select {
				case tenantReadinessSlots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				if err := ctx.Err(); err != nil {
					<-tenantReadinessSlots
					results <- err
					continue
				}
				err := probeTenantBucketReady(ctx, store)
				results <- err
			}
		}()
	}
	var firstErr error
	for range stores {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-results:
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return firstErr
}

// S3_TENANT_PURGE_BUCKETS lists additional buckets on the configured S3
// endpoint. Primary bucket is always included; duplicate/empty names refuse
// startup instead of silently narrowing physical deletion coverage.
func tenantPurgeBucketNames(primary, extra string) ([]string, error) {
	if primary == "" {
		return nil, errors.New("tenant purge primary bucket required")
	}
	names := []string{primary}
	seen := map[string]bool{primary: true}
	if extra == "" {
		return names, nil
	}
	for _, part := range strings.Split(extra, ",") {
		name := strings.TrimSpace(part)
		if name == "" || seen[name] || len(names) >= 256 {
			return nil, errors.New("invalid tenant purge bucket list")
		}
		seen[name] = true
		names = append(names, name)
	}
	return names, nil
}

// RunLoop preserves the single-job API used by embedders and tests.
func RunLoop(ctx context.Context, job CleanupJob, state *LoopState, idle time.Duration) error {
	return RunJobs(ctx, []CleanupJob{job}, []*LoopState{state}, idle)
}

// RunJobs executes one job per cycle, rotating even after failure or an empty
// queue. Each job retains independent readiness and counters. Jobs never overlap.
func RunJobs(ctx context.Context, jobs []CleanupJob, states []*LoopState, idle time.Duration) error {
	if len(jobs) == 0 || len(jobs) != len(states) || idle <= 0 {
		return errors.New("invalid cleanup loop")
	}
	seen := map[*LoopState]bool{}
	for i, job := range jobs {
		if job == nil || states[i] == nil || seen[states[i]] {
			return errors.New("invalid cleanup loop")
		}
		seen[states[i]] = true
	}
	defer func() {
		for _, state := range states {
			state.Ready.Store(false)
		}
	}()
	for index := 0; ; index = (index + 1) % len(jobs) {
		if ctx.Err() != nil {
			return nil
		}
		cycle, cancel := context.WithTimeout(ctx, cleanupCycleTimeout(jobs[index]))
		cleaned, err := jobs[index].CleanOne(cycle)
		// A dependency may return success after ignoring the cycle deadline.
		// Keep that result from making the job ready or increasing success counts.
		if cycleErr := cycle.Err(); cycleErr != nil && err == nil {
			err = cycleErr
		}
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		state := states[index]
		state.Ready.Store(err == nil)
		if err != nil {
			state.Failures.Add(1)
		} else if cleaned {
			state.Cleaned.Add(1)
		}
		delay := idle
		if cleaned && err == nil {
			delay = 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func Run(ctx context.Context, dsn, listen string, storageConfig objectstore.Config) error {
	appender, requireHistory, err := loadTenantDeletionHistoryAppender()
	if err != nil {
		return err
	}
	storage, err := objectstore.New(storageConfig)
	if err != nil {
		return err
	}
	targets, err := loadTenantPurgeTargets(os.Getenv("S3_TENANT_PURGE_TARGETS_FILE"))
	if err != nil {
		return err
	}
	tenantConfigs, err := tenantPurgeConfigs(storageConfig, os.Getenv("S3_TENANT_PURGE_BUCKETS"), targets)
	if err != nil {
		return err
	}
	tenantStores := make(TenantPurgeBuckets, 0, len(tenantConfigs))
	tenantReady := make([]tenantBucketReadiness, 0, len(tenantConfigs))
	for _, config := range tenantConfigs {
		current := storage
		if config.Endpoint != storageConfig.Endpoint || config.Bucket != storageConfig.Bucket || config.AccessKey != storageConfig.AccessKey || config.SecretKey != storageConfig.SecretKey || config.Region != storageConfig.Region || config.Development != storageConfig.Development {
			current, err = objectstore.New(config)
			if err != nil {
				return err
			}
		}
		tenantStores = append(tenantStores, TenantPurgeBucket{Name: config.Endpoint + "/" + config.Bucket, Objects: current})
		tenantReady = append(tenantReady, current)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid maintenance database configuration")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	database.GuardApplicationRole(config)
	config.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return errors.New("maintenance database unavailable")
	}
	defer pool.Close()
	if err = database.RequireApplicationRole(ctx, pool, "photo_maintenance"); err != nil { return err }
	if err = RequireCollectionCleanup(ctx, pool); err != nil { return err }
	if err = RequireResourceWithdrawal(ctx, pool); err != nil { return err }
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	var installed bool
	err = pool.QueryRow(startup, `SELECT to_regprocedure('photo.claim_tenant_deletion()') IS NOT NULL AND to_regprocedure('photo.checkpoint_tenant_deletion(uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.settle_v30_archive_generations_after_tenant_purge(uuid,uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.renew_tenant_deletion_lease(uuid,uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.finish_tenant_deletion(uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.claim_staging_cleanup()') IS NOT NULL AND to_regprocedure('photo.finish_staging_cleanup(uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.claim_original_cleanup()') IS NOT NULL AND to_regprocedure('photo.finish_original_cleanup(uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.claim_output_cleanup()') IS NOT NULL AND to_regprocedure('photo.finish_output_cleanup(uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.claim_document_cleanup()') IS NOT NULL AND to_regprocedure('photo.finish_document_cleanup(uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.expire_upload_session()') IS NOT NULL AND to_regprocedure('photo.claim_upload_abort_cleanup()') IS NOT NULL AND to_regprocedure('photo.finish_upload_abort_cleanup(uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.claim_artifact_gc()') IS NOT NULL AND to_regprocedure('photo.finish_artifact_gc(uuid,uuid)') IS NOT NULL AND to_regprocedure('photo.recover_artifact_gc()') IS NOT NULL`).Scan(&installed)
	if err == nil && installed {
		var ready bool
		err = pool.QueryRow(startup, `SELECT current_user='photo_maintenance' AND p.prosecdef AND owner.rolname='photo_fn_owner'
   AND EXISTS(SELECT 1 FROM unnest(p.proconfig) cfg WHERE cfg LIKE 'search_path=pg_catalog,%' AND cfg LIKE '%photo%')
   AND has_function_privilege(current_user,p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
   FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
   WHERE p.oid=to_regprocedure('photo.settle_v30_archive_generations_after_tenant_purge(uuid,uuid,uuid)')`).Scan(&ready)
		if err == nil && !ready {
			err = errors.New("archive generation tenant purge settlement unavailable")
		}
		if err == nil {
			err = pool.QueryRow(startup, `SELECT current_user='photo_maintenance' AND p.prosecdef AND owner.rolname='photo_fn_owner'
   AND EXISTS(SELECT 1 FROM unnest(p.proconfig) cfg WHERE cfg LIKE 'search_path=pg_catalog,%' AND cfg LIKE '%photo%')
   AND has_function_privilege(current_user,p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
   FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
   WHERE p.oid=to_regprocedure('photo.renew_tenant_deletion_lease(uuid,uuid,uuid)')`).Scan(&ready)
			if err == nil && !ready {
				err = errors.New("tenant deletion lease renewal unavailable")
			}
		}
	}
	if err == nil && installed {
		err = RequireEditorGCSchema(startup, pool)
		if err == nil { err = RequireArtifactGCAccounting(startup, pool) }
        if err == nil { err = RequireOutputCleanupRefund(startup, pool) }
	}
	var history TenantDeletionHistory
	if err == nil && installed && appender != nil {
		var ready bool
		err = pool.QueryRow(startup, `SELECT current_user='photo_maintenance' AND p.prosecdef AND owner.rolname='photo_fn_owner'
   AND has_function_privilege(current_user,p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
   FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
   WHERE p.oid=to_regprocedure('photo.v32_tenant_deletion_history_intent(uuid,uuid,uuid)')`).Scan(&ready)
		if err == nil && !ready {
			err = errors.New("deletion history SQL installation unavailable")
		}
		if err == nil {
			history = SQLTenantDeletionHistory{DB: pool, Appender: appender}
		}
	}
	if err == nil && installed {
		err = readyTenantBuckets(startup, tenantReady)
	}
	cancel()
	if err != nil || !installed {
		return errors.New("maintenance dependencies unavailable")
	}
	// V21 is rolled out after the baseline maintenance service. Enable draft
	// expiry only when its forward-only migration is installed.
	var v2DraftInstalled bool
	probe, probeDone := context.WithTimeout(ctx, 5*time.Second)
	err = pool.QueryRow(probe, "SELECT to_regprocedure('photo.expire_one_v2_draft()') IS NOT NULL").Scan(&v2DraftInstalled)
	probeDone()
	if err != nil {
		return errors.New("v2 draft cleanup probe failed")
	}
	var v2UploadInstalled bool
	probe, probeDone = context.WithTimeout(ctx, 5*time.Second)
	err = pool.QueryRow(probe, `SELECT to_regprocedure('photo.claim_v2_draft_upload_cleanup()') IS NOT NULL
	 AND to_regprocedure('photo.finish_v2_draft_upload_cleanup(uuid,uuid,uuid)') IS NOT NULL
	 AND to_regprocedure('photo.retire_one_v2_draft_upload()') IS NOT NULL`).Scan(&v2UploadInstalled)
	probeDone()
	if err != nil {
		return errors.New("v2 draft upload cleanup probe failed")
	}
 var collectionInstalled bool
 collectionProbe,collectionProbeDone:=context.WithTimeout(ctx,5*time.Second)
 err=pool.QueryRow(collectionProbe,`SELECT to_regprocedure('photo.claim_collection_deletion()') IS NOT NULL AND to_regprocedure('photo.finish_collection_deletion(uuid,uuid,uuid,uuid)') IS NOT NULL AND has_function_privilege(current_user,'photo.claim_collection_deletion()','EXECUTE') AND has_function_privilege(current_user,'photo.finish_collection_deletion(uuid,uuid,uuid,uuid)','EXECUTE')`).Scan(&collectionInstalled)
 collectionProbeDone()
 if err!=nil||!collectionInstalled{return errors.New("collection deletion lifecycle unavailable")}
 collectionState:=new(LoopState)
 resourceState:=new(LoopState)
	state := new(LoopState)
	documentState := new(LoopState)
	originalState := new(LoopState)
	stagingState := new(LoopState)
	uploadState := new(LoopState)
	artifactState := new(LoopState)
	tenantState := new(LoopState)
	v2DraftState := new(LoopState)
	v2UploadState := new(LoopState)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		check, done := context.WithTimeout(r.Context(), 5*time.Second)
		defer done()
		if !resourceState.Ready.Load() || !collectionState.Ready.Load() || !state.Ready.Load() || !documentState.Ready.Load() || !originalState.Ready.Load() || !stagingState.Ready.Load() || !uploadState.Ready.Load() || !artifactState.Ready.Load() || !tenantState.Ready.Load() || (v2DraftInstalled && !v2DraftState.Ready.Load()) || (v2UploadInstalled && !v2UploadState.Ready.Load()) || pool.Ping(check) != nil || readyTenantBuckets(check, tenantReady) != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
        fmt.Fprintf(w,"# TYPE photo_collection_cleanup_total counter\nphoto_collection_cleanup_total %d\n# TYPE photo_collection_cleanup_failures_total counter\nphoto_collection_cleanup_failures_total %d\n",collectionState.Cleaned.Load(),collectionState.Failures.Load())
        fmt.Fprintf(w,"# TYPE photo_resource_withdrawal_total counter\nphoto_resource_withdrawal_total %d\n# TYPE photo_resource_withdrawal_failures_total counter\nphoto_resource_withdrawal_failures_total %d\n",resourceState.Cleaned.Load(),resourceState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_output_cleanup_total counter\nphoto_output_cleanup_total %d\n# TYPE photo_output_cleanup_failures_total counter\nphoto_output_cleanup_failures_total %d\n", state.Cleaned.Load(), state.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_staging_cleanup_total counter\nphoto_staging_cleanup_total %d\n# TYPE photo_staging_cleanup_failures_total counter\nphoto_staging_cleanup_failures_total %d\n", stagingState.Cleaned.Load(), stagingState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_original_cleanup_total counter\nphoto_original_cleanup_total %d\n# TYPE photo_original_cleanup_failures_total counter\nphoto_original_cleanup_failures_total %d\n", originalState.Cleaned.Load(), originalState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_document_cleanup_total counter\nphoto_document_cleanup_total %d\n# TYPE photo_document_cleanup_failures_total counter\nphoto_document_cleanup_failures_total %d\n", documentState.Cleaned.Load(), documentState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_upload_cleanup_total counter\nphoto_upload_cleanup_total %d\n# TYPE photo_upload_cleanup_failures_total counter\nphoto_upload_cleanup_failures_total %d\n", uploadState.Cleaned.Load(), uploadState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_artifact_gc_total counter\nphoto_artifact_gc_total %d\n# TYPE photo_artifact_gc_failures_total counter\nphoto_artifact_gc_failures_total %d\n", artifactState.Cleaned.Load(), artifactState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_tenant_cleanup_total counter\nphoto_tenant_cleanup_total %d\n# TYPE photo_tenant_cleanup_failures_total counter\nphoto_tenant_cleanup_failures_total %d\n", tenantState.Cleaned.Load(), tenantState.Failures.Load())
		if v2DraftInstalled {
			fmt.Fprintf(w, "# TYPE photo_v2_draft_expiry_total counter\nphoto_v2_draft_expiry_total %d\n# TYPE photo_v2_draft_expiry_failures_total counter\nphoto_v2_draft_expiry_failures_total %d\n", v2DraftState.Cleaned.Load(), v2DraftState.Failures.Load())
		}
		if v2UploadInstalled {
			fmt.Fprintf(w, "# TYPE photo_v2_draft_upload_cleanup_total counter\nphoto_v2_draft_upload_cleanup_total %d\n# TYPE photo_v2_draft_upload_cleanup_failures_total counter\nphoto_v2_draft_upload_cleanup_failures_total %d\n", v2UploadState.Cleaned.Load(), v2UploadState.Failures.Load())
		}
	})
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return errors.New("maintenance listen failed")
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 6 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	defer server.Close()
	jobs, stop := context.WithCancel(ctx)
	defer stop()
	loopDone := make(chan error, 1)
	go func() {
		cleanupJobs := []CleanupJob{
            ResourceWithdrawalCleaner{DB:pool},
            CollectionCleaner{DB:pool},
			OutputCleaner{Repository: SQLCleanup{DB: pool}, Objects: storage, Bucket: storageConfig.Bucket},
			DocumentCleaner{Repository: SQLDocumentCleanup{DB: pool}, Objects: storage, Bucket: storageConfig.Bucket},
			OriginalCleaner{Repository: SQLOriginalCleanup{DB: pool}, Objects: storage, Bucket: storageConfig.Bucket},
			StagingCleaner{Repository: SQLStagingCleanup{DB: pool}, Objects: storage, Bucket: storageConfig.Bucket},
			UploadCleaner{Repository: SQLUploadCleanup{DB: pool}, Objects: storage},
			ArtifactCleaner{Repository: SQLArtifactGC{DB: pool}, Objects: storage, Bucket: storageConfig.Bucket},
			TenantCleaner{Repository: SQLTenantDeletion{DB: pool}, Objects: tenantStores, Batch: 256, History: history, RequireHistory: requireHistory},
		}
		cleanupStates := []*LoopState{resourceState,collectionState,state, documentState, originalState, stagingState, uploadState, artifactState, tenantState}
		if v2DraftInstalled {
			cleanupJobs = append(cleanupJobs, V2DraftCleaner{DB: pool})
			cleanupStates = append(cleanupStates, v2DraftState)
		}
		if v2UploadInstalled {
			cleanupJobs = append(cleanupJobs, V2DraftUploadCleaner{
				Repository: SQLV2DraftUploadCleanup{DB: pool}, Objects: storage,
				Bucket: storageConfig.Bucket})
			cleanupStates = append(cleanupStates, v2UploadState)
		}
		loopDone <- RunJobs(jobs, cleanupJobs, cleanupStates, 5*time.Second)
	}()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serveDone:
	case err = <-loopDone:
		stop()
		_ = server.Close()
		return err
	}
	stop()
	shutdown, done := context.WithTimeout(context.Background(), 15*time.Second)
	defer done()
	err = server.Shutdown(shutdown)
	select {
	case <-loopDone:
	case <-shutdown.Done():
		return errors.New("maintenance shutdown timeout")
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("maintenance listener stopped")
	}
	return err
}
