package main

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	migrations "photoeditor/backend/db"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/collection"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/contactsheet"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/gateway"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/interchange"
	"photoeditor/backend/internal/mail"
	"photoeditor/backend/internal/maintenance"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/portal"
	"photoeditor/backend/internal/queue"
	"photoeditor/backend/internal/rawdevelopment"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/upload"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2optical"
	"photoeditor/backend/internal/v2region"
	"photoeditor/backend/internal/v2render"
	"photoeditor/backend/internal/workflow"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err.Error())
		os.Exit(1)
	}
}
func run() error { return runWithAPIDependencies(apiProcessDependencies{}) }
func runWithAPIDependencies(dependencies apiProcessDependencies) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Role == "migrate" {
		db, err := sql.Open("pgx", cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer db.Close()
		goose.SetBaseFS(migrations.FS)
		if err = goose.SetDialect("postgres"); err != nil {
			return err
		}
		return goose.Up(db, "migrations")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.Role == "api" || cfg.Role == "scheduler" {
		dependencies, err = installInterchangeExportOperator(ctx,
			os.Getenv("V30_INTERCHANGE_EXPORT_OPERATOR_FILE"),
			os.Getenv("V30_INTERCHANGE_EXPORT_OPERATOR_SHA"), dependencies)
		if err != nil {
			return err
		}
	}
	if cfg.Role == "bootstrap-queue" {
		if cfg.AMQPURL == "" {
			return errors.New("AMQP_URL required")
		}
		if os.Getenv("PHOTO_WORKER_ID") != "" || os.Getenv("PHOTO_WORKER_CLASS") != "" {
			worker, err := uuid.Parse(os.Getenv("PHOTO_WORKER_ID"))
			if err != nil {
				return errors.New("invalid PHOTO_WORKER_ID")
			}
			return queue.BootstrapWorker(cfg.AMQPURL, worker, os.Getenv("PHOTO_WORKER_CLASS"))
		}
		return queue.Bootstrap(cfg.AMQPURL)
	}
	if cfg.Role == "bootstrap-storage" {
		storageConfig := objectstore.ConfigFromEnv(cfg.Environment == "development")
		storageConfig.CORSOrigins = []string{cfg.AppOrigin, cfg.PortalOrigin}
		store, err := objectstore.New(storageConfig)
		if err != nil {
			return err
		}
		bootstrap, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return store.Bootstrap(bootstrap)
	}
	if cfg.Role == "object-gateway" {
		gatewayConfig, err := gateway.ConfigFromEnv(cfg.DatabaseURL, cfg.Listen, cfg.Environment == "development")
		if err != nil {
			return err
		}
		return gateway.Run(ctx, gatewayConfig)
	}
	if cfg.Role == "maintenance" {
		return maintenance.Run(ctx, cfg.DatabaseURL, cfg.Listen, objectstore.ConfigFromEnv(cfg.Environment == "development"))
	}
	if cfg.Role == "scheduler" {
		var nativeFactory workflow.SchedulerNativeRecoveryFactory
		if path := os.Getenv("PHOTO_V27_NATIVE_RECOVERY_CONFIG"); path != "" {
			sourceDSN := os.Getenv("V27_NATIVE_SOURCE_DATABASE_URL")
			if sourceDSN == "" {
				sourceDSN = cfg.DatabaseURL
			}
			nativeFactory, err = workflow.LoadNativeRecoveryFactory(path, sourceDSN, os.Getenv("PHOTO_ICC_ROOT"))
			if err != nil {
				return err
			}
		}
		signingPath, signingSHA := os.Getenv("PHOTO_SIGNING_DEPLOYMENT_FILE"), os.Getenv("PHOTO_SIGNING_DEPLOYMENT_SHA")
		if err := validateAPISigningDeclaration(signingPath, signingSHA, dependencies); err != nil {
			return err
		}
		var signingFactory workflow.SchedulerDeliverySigningFactory
		if signingPath != "" {
			signingFactory = schedulerSigningFactory(signingPath, signingSHA, dependencies)
		}
		importFactories, err := schedulerImportFactories(ctx, dependencies)
		if err != nil {
			return err
		}
		exportFactory, err := prepareSchedulerInterchangeExport(ctx,
			os.Getenv("V30_INTERCHANGE_EXPORT_DEPLOYMENT_FILE"),
			os.Getenv("V30_INTERCHANGE_EXPORT_DEPLOYMENT_SHA"), dependencies)
		if err != nil {
			return err
		}
		return workflow.RunSchedulerWithInterchangeExport(ctx, cfg.DatabaseURL, cfg.Listen,
			objectstore.ConfigFromEnv(cfg.Environment == "development"),
			os.Getenv("PHOTO_ICC_ROOT"), nativeFactory,
			archiveCompletionFactory{}, signingFactory, exportFactory, importFactories...)
	}
	if cfg.Role == "restore-replay" {
		if dependencies.RestoreReplay == nil || dependencies.RestoreReplayReceipt == nil {
			return errors.New("restore replay requires installed isolation authority")
		}
		result, receipt, replayErr := queue.RunRestoreReplayAndReport(ctx, cfg.DatabaseURL, cfg.AMQPURL, *dependencies.RestoreReplay, *dependencies.RestoreReplayReceipt)
		if replayErr != nil {
			return replayErr
		}
		slog.Info("isolated restore replay completed", "database", result.Database,
			"initially_pending", result.InitiallyPending, "confirmed", result.Confirmed, "receipt_sha256", receipt.SHA256)
		return nil
	}
	if cfg.Role == "publisher" {
		if cfg.AMQPURL == "" {
			return errors.New("AMQP_URL required")
		}
		return queue.RunPublisher(ctx, cfg.DatabaseURL, cfg.AMQPURL, cfg.Listen)
	}
	if cfg.Role == "dead-letter-worker" {
		if cfg.AMQPURL == "" {
			return errors.New("AMQP_URL required")
		}
		return queue.RunDeadLetterCollector(ctx, cfg.DatabaseURL, cfg.AMQPURL, cfg.Listen)
	}
	if cfg.Role == "mail-worker" {
		if cfg.AMQPURL == "" {
			return errors.New("AMQP_URL required")
		}
		keys, err := auth.LoadMailKeys(os.Getenv("MAIL_KEYS_FILE"))
		if err != nil {
			return err
		}
		smtpConfig, err := mail.ConfigFromEnv(cfg.Environment == "development")
		if err != nil {
			return err
		}
		return mail.Run(ctx, cfg.DatabaseURL, cfg.AMQPURL, cfg.Listen, keys, mail.SMTP{Config: smtpConfig})
	}
	if cfg.Role == "collection-worker" {
		if cfg.AMQPURL == "" {
			return errors.New("AMQP_URL required")
		}
		return collection.RunWorker(ctx, cfg.DatabaseURL, cfg.AMQPURL, cfg.Listen)
	}
	if cfg.Role != "api" {
		return errors.New("role is not yet installed; refusing to start an incomplete service")
	}

	interchangeExport, err := prepareAPIInterchangeExport(ctx, os.Getenv("V30_INTERCHANGE_EXPORT_DEPLOYMENT_FILE"), os.Getenv("V30_INTERCHANGE_EXPORT_DEPLOYMENT_SHA"), dependencies)
	if err != nil {
		return err
	}
	signingPath, signingSHA := os.Getenv("PHOTO_SIGNING_DEPLOYMENT_FILE"), os.Getenv("PHOTO_SIGNING_DEPLOYMENT_SHA")
	if err := validateAPISigningDeclaration(signingPath, signingSHA, dependencies); err != nil {
		return err
	}
	var renderCalibrations v2render.RenderCalibrationCatalog
	if cfg.RenderCalibrationFile != "" {
		catalog, err := v2render.LoadPinnedRenderCalibrationCatalog(cfg.RenderCalibrationFile, cfg.RenderCalibrationSHA)
		if err != nil {
			return errors.New("render calibration catalog unavailable or invalid")
		}
		renderCalibrations = catalog
	}
	var regionCalibrations *v2region.PinnedRegionCalibrationCatalog
	if cfg.RegionCalibrationFile != "" {
		regionCalibrations, err = v2region.LoadPinnedRegionCalibrationCatalog(cfg.RegionCalibrationFile, cfg.RegionCalibrationSHA)
		if err != nil {
			return errors.New("region calibration catalog unavailable or invalid")
		}
	}
	if cfg.AuthDatabaseURL == "" || cfg.AuthKeysFile == "" {
		return errors.New("AUTH_DATABASE_URL and AUTH_KEYS_FILE are required")
	}
	signer, mailKeys, pepper, err := auth.LoadKeys(cfg.AuthKeysFile, cfg.JWTIssuer, cfg.JWTAudience)
	if err != nil {
		return err
	}
	apiConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return errors.New("database configuration invalid")
	}
	apiConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
	pool, err := pgxpool.NewWithConfig(ctx, apiConfig)
	if err != nil {
		return errors.New("database configuration invalid")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("database unavailable")
	}
	if err = requireRunPresentationSchema(ctx, pool); err != nil {
		return err
	}
	var draftReplayInstalled bool
	err = pool.QueryRow(ctx, `SELECT current_user='photo_api' AND p.prosecdef
	 AND owner.rolname='photo_fn_owner'
	 AND EXISTS(SELECT FROM unnest(p.proconfig) cfg WHERE cfg LIKE 'search_path=pg_catalog,%' AND cfg LIKE '%photo%')
	 AND has_function_privilege(current_user,p.oid,'EXECUTE')
	 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl
	  WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
	 FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner
	 WHERE p.oid=to_regprocedure('photo.v21_ready_draft_object(uuid,uuid,uuid,uuid,uuid,text,text)')`).Scan(&draftReplayInstalled)
	if err != nil || !draftReplayInstalled {
		return errors.New("V21 ready draft replay migration or API role unavailable")
	}
	portalConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return errors.New("portal database configuration invalid")
	}
	portalConfig.ConnConfig.RuntimeParams["role"] = "photo_portal"
	portalPool, err := pgxpool.NewWithConfig(ctx, portalConfig)
	if err != nil {
		return errors.New("portal database unavailable")
	}
	defer portalPool.Close()
	var portalInstalled bool
	if err = portalPool.QueryRow(ctx, `SELECT to_regprocedure('photo.exchange_share_session(uuid,bytea,bytea,bytea,text,timestamptz)') IS NOT NULL
		AND to_regprocedure('photo.authorize_portal_session(bytea,bytea,text)') IS NOT NULL
		AND to_regprocedure('photo.get_portal_collection(bytea)') IS NOT NULL
		AND to_regprocedure('photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz)') IS NOT NULL
		AND to_regprocedure('photo.get_portal_upload(bytea,uuid,boolean)') IS NOT NULL
		AND to_regprocedure('photo.attach_portal_multipart(bytea,bytea,uuid,text)') IS NOT NULL
		AND to_regprocedure('photo.abort_portal_upload(bytea,bytea,uuid)') IS NOT NULL
		AND to_regprocedure('photo.complete_portal_upload(bytea,bytea,text,text,uuid,bigint,jsonb,uuid,uuid,uuid)') IS NOT NULL
		AND to_regprocedure('photo.list_portal_items(bytea,uuid,bigint,uuid,integer)') IS NOT NULL
		AND to_regprocedure('photo.resolve_portal_media(bytea,text,uuid[],text[])') IS NOT NULL
		AND to_regprocedure('photo.create_portal_selection(bytea,bytea,text,text,uuid,bigint,text,uuid[],uuid,timestamptz)') IS NOT NULL
		AND to_regprocedure('photo.start_portal_download(bytea,bytea,text,text,uuid,uuid,uuid,uuid)') IS NOT NULL
		AND to_regprocedure('photo.get_portal_download(bytea,uuid)') IS NOT NULL
		AND to_regprocedure('photo.cancel_portal_download(bytea,bytea,uuid)') IS NOT NULL
		AND to_regprocedure('photo.create_portal_download_intent(bytea,bytea,text,text,uuid,integer,uuid,bytea,timestamptz)') IS NOT NULL
		AND to_regprocedure('photo.create_portal_item_download_intent(bytea,bytea,text,text,uuid,uuid,uuid,bytea,timestamptz)') IS NOT NULL
		AND to_regprocedure('photo.redeem_download_intent(bytea,text)') IS NOT NULL`).Scan(&portalInstalled); err != nil || !portalInstalled {
		return errors.New("portal database functions unavailable")
	}
	storage, err := objectstore.New(objectstore.ConfigFromEnv(cfg.Environment == "development"))
	if err != nil {
		return errors.New("object storage configuration invalid")
	}
	storageCheck, finishStorageCheck := context.WithTimeout(ctx, 10*time.Second)
	err = storage.Ready(storageCheck)
	finishStorageCheck()
	if err != nil {
		return errors.New("object storage unavailable")
	}
	profileSHAs, err := colorprofile.LoadSHAs(os.Getenv("PHOTO_ICC_ROOT"))
	if err != nil {
		return err
	}
	authConfig, err := pgxpool.ParseConfig(cfg.AuthDatabaseURL)
	if err != nil {
		return errors.New("auth database configuration invalid")
	}
	authConfig.ConnConfig.RuntimeParams["role"] = "photo_auth"
	authPool, err := pgxpool.NewWithConfig(ctx, authConfig)
	if err != nil {
		return errors.New("auth database unavailable")
	}
	defer authPool.Close()
	if err = authPool.Ping(ctx); err != nil {
		return errors.New("auth database unavailable")
	}
	authService := &auth.Service{DB: authPool, Pepper: pepper, MailKeys: mailKeys, Signer: signer, Registration: cfg.RegistrationEnabled}
	uploadService := &upload.Service{DB: pool, Objects: storage}
	if path := os.Getenv("PHOTO_RAW_QUALIFICATION_FILE"); path != "" {
		source, loadErr := rawdevelopment.LoadSourceConfig(ctx, path)
		if loadErr != nil {
			return errors.New("RAW qualification configuration invalid")
		}
		var installed bool
		if checkErr := pool.QueryRow(ctx, `SELECT COALESCE(has_function_privilege(current_user,to_regprocedure('photo.v25_raw_qualification_object(uuid,uuid,uuid,uuid,uuid,text,jsonb)'),'EXECUTE'),false) AND COALESCE(has_function_privilege(current_user,to_regprocedure('photo.v25_raw_device_qualification_object(uuid,uuid,uuid,bigint,uuid,uuid,uuid,text,jsonb)'),'EXECUTE'),false)`).Scan(&installed); checkErr != nil || !installed {
			return errors.New("RAW qualification database function unavailable")
		}
		source.Objects = storage
		source.Bucket = objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket
		uploadService.RAWAdmission = source
	}

	portalService := &portal.Service{DB: portalPool, Pepper: pepper, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}
	reviewService := &review.Service{DB: pool, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, Gate: workflow.ReviewExpansionGate{}, Cursors: pagination.Codec{Key: auth.MAC(pepper, "cursor-key")}}
	mediaService := &media.Service{DB: pool, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, SRGBSHA256: profileSHAs["srgb_v1"]}
	contactSheetService := &contactsheet.Service{DB: pool, Resources: contactsheet.ResourceManager{DB: pool, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}, SRGBSHA256: profileSHAs["srgb_v1"]}
	analysisAdmission, closeAnalysis, err := buildAnalysisAdmission(ctx, pool, cfg.DatabaseURL,
		storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeAnalysis()
	trainingAdmission, closeTraining, err := buildTrainingAdmission(ctx, pool,
		cfg.DatabaseURL, storage,
		objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeTraining()
	renderAdmission, closeRender, err := buildRenderAdmission(ctx, pool, cfg.DatabaseURL, storage,
		objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, renderCalibrations)
	if err != nil {
		return err
	}
	defer closeRender()
	regionAdmission, closeRegion, err := buildRegionAdmission(ctx, pool, cfg.DatabaseURL, storage,
		objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, regionCalibrations, cfg.RegionEvidenceSHA)
	if err != nil {
		return err
	}
	defer closeRegion()
	semanticReviewApply, closeSemanticAdmission, err := buildSemanticExposureApply(ctx, pool, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeSemanticAdmission()
	semanticPropose, closeSemanticCreate, err := buildSemanticCreate(ctx, pool, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeSemanticCreate()
	if semanticReviewApply == nil {
		semanticReviewApply, err = buildSemanticReviewApply(pool, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
		if err != nil {
			return err
		}
	}
	var semanticReviewClose httpapi.SemanticReviewClose
	if tenantService, ok := semanticReviewApply.(httpapi.SemanticReviewClose); ok {
		semanticReviewClose = tenantService
	} else {
		semanticReviewClose, err = buildSemanticReviewClose(pool, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
		if err != nil {
			return err
		}
	}
	relightSubmit, closeRelight, err := buildRelightCreate(ctx, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeRelight()
	glareDonorSubmit, closeGlareDonor, err := buildGlareDonorCreate(ctx, pool, storage,
		objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeGlareDonor()
	openEyesPrepare, closeOpenEyesPrepare, err := buildOpenEyesPrepare(ctx, pool,
		storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeOpenEyesPrepare()
	reflectionSubmit, closeReflection, err := buildReflectionCreate(ctx, pool, storage,
		objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeReflection()
	localPortraitPrepare, closeLocalPortraitPrepare, err := buildLocalPortraitPrepare(ctx, pool, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeLocalPortraitPrepare()
	relightAnalysis, closeAnalysis, err := buildRelightAnalysis(ctx, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeAnalysis()
	perspectiveSubmit, closePerspective, err := buildPerspective(ctx, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closePerspective()
	negativeSubmit, closeNegative, err := buildNegative(ctx, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeNegative()
	linesSubmit, closeLines, err := buildPerspectiveLines(ctx, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket)
	if err != nil {
		return err
	}
	defer closeLines()
	panoramaSubmit, closePanorama, err := buildMulti(ctx, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, "optical.panorama")
	if err != nil {
		return err
	}
	defer closePanorama()
	focusSubmit, closeFocus, err := buildMulti(ctx, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, "optical.focus_stack")
	if err != nil {
		return err
	}
	defer closeFocus()
	interchangeCreator, closeInterchange, err := buildInterchangeCreateWithFactory(ctx, pool, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, interchangeExport)
	if err != nil {
		return err
	}
	defer closeInterchange()
	importStyleFactory, err := buildInterchangeReviewStyle(profileSHAs["work_rec2020_gamma24_v1"])
	if err != nil {
		return err
	}
	signing, err := installAPISigning(ctx, signingPath, signingSHA, cfg.DatabaseURL, pool, storage, objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, profileSHAs["work_rec2020_gamma24_v1"], dependencies)
	if err != nil {
		return err
	}
	defer signing.Close()
	var exportSigner httpapi.ExportSigner
	if signing != nil {
		exportSigner = signing.Router
	}
	var deliveryPolicyChecker delivery.DeliverySigningPolicyChecker
	if signing != nil {
		deliveryPolicyChecker = signing.Router
	}
	deliveryCreator, closeDelivery, err := buildDeliveryCreate(ctx, pool, storage,
		objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket,
		deliveryPolicyChecker)
	if err != nil {
		return err
	}
	defer closeDelivery()
	snapshotBucket := objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket
	for _, signature := range []string{
		"photo.start_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text)",
		"photo.read_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text)",
		"photo.v30_archive_begin_generation_status(uuid,uuid,uuid,uuid,uuid,uuid,text)",
		"photo.finish_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text)",
		"photo.bind_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text)",
		"photo.finish_v30_archive_begin_not_dispatched(uuid,uuid,uuid,uuid,uuid,uuid,uuid)",
		"photo.v30_archive_open_begin_for_status(uuid,uuid,uuid,uuid,uuid,uuid)",
		"photo.v30_archive_cross_key_reconciliation_required(uuid,uuid,uuid,uuid,uuid)",
	} {
		var installed bool
		if err := pool.QueryRow(ctx, `SELECT coalesce(has_function_privilege(current_user,to_regprocedure($1),'EXECUTE'),false)`, signature).Scan(&installed); err != nil {
			return err
		}
		if !installed {
			return errors.New("archive generation Begin migration/role not installed: " + signature)
		}
	}
	for _, signature := range []string{
		"photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid)",
		"photo.read_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid)",
	} {
		var executable bool
		if err := pool.QueryRow(ctx, `SELECT coalesce(has_function_privilege('photo_api',to_regprocedure($1),'EXECUTE'),false)`, signature).Scan(&executable); err != nil {
			return err
		}
		if executable {
			return errors.New("archive generation unchecked API entrypoint remains executable: " + signature)
		}
	}
	var archiveReserveDefinition string
	if err := pool.QueryRow(ctx, `SELECT pg_get_functiondef('photo.reserve_v30_archive_upload(uuid,uuid,uuid,text,text,text)'::regprocedure)`).Scan(&archiveReserveDefinition); err != nil {
		return err
	}
	if !strings.Contains(archiveReserveDefinition, "v30_archive_cross_key_begin_fence") || !strings.Contains(archiveReserveDefinition, "v30_archive_begin_generations") || !strings.Contains(archiveReserveDefinition, "v30_archive_writers") {
		return errors.New("archive cross-key Begin fence migration not installed")
	}
	for _, signature := range []string{"photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid)", "photo.start_v30_archive_begin(uuid,uuid,uuid,uuid,uuid)"} {
		var definition string
		if err := pool.QueryRow(ctx, `SELECT pg_get_functiondef($1::regprocedure)`, signature).Scan(&definition); err != nil {
			return err
		}
		if !strings.Contains(definition, "v30_archive_start_cross_key_fence") || !strings.Contains(definition, "FOR UPDATE") || !strings.Contains(definition, "prior.expected_sha=d.expected_sha") {
			return errors.New("archive Start cross-key fence migration not installed: " + signature)
		}
	}
	snapshotRepository := interchange.SQLExportSnapshotObjectRepository{DB: pool, Bucket: snapshotBucket}
	snapshotPreparation := interchange.ExportSnapshotPreparation{
		Authority:   interchange.SQLExportSnapshotAuthority{DB: pool, Bucket: snapshotBucket, Objects: storage},
		Publication: interchange.ExportSnapshotObjectPublisher{Repository: snapshotRepository, Objects: storage},
		Attachment:  snapshotRepository,
	}
	archiveBegin := interchange.ArchiveUploadBeginService{Repository: interchange.SQLArchiveUploadRepository{DB: pool, Bucket: snapshotBucket}, Objects: storage, Writers: interchange.SQLArchiveBeginWriters{DB: pool, Bucket: snapshotBucket}, Inspector: interchange.SQLArchiveBeginInspector{DB: pool, Bucket: snapshotBucket, Objects: storage}, Generations: interchange.SQLArchiveBeginGenerations{DB: pool, Bucket: snapshotBucket}, CrossKey: interchange.SQLArchiveCrossKeyStatus{DB: pool}}
	archiveActions := interchange.ArchiveUploadActions{Repository: interchange.SQLArchiveUploadRepository{DB: pool, Bucket: snapshotBucket}, Authority: interchange.SQLArchivePartsAuthority{DB: pool, Bucket: snapshotBucket}, Signer: storage, BeginGenerations: interchange.SQLArchiveBeginGenerations{DB: pool, Bucket: snapshotBucket}, GenerationStatus: interchange.SQLArchiveBeginGenerations{DB: pool, Bucket: snapshotBucket}, OpenBegin: interchange.SQLArchiveOpenBeginStatus{DB: pool}}
	server := (&httpapi.Server{OpenEyesPrepare: openEyesPrepare, OpenEyesSubmit: openEyesPrepare, OpenEyesSourceCandidates: openEyesPrepare, OpenEyesDonorCandidates: openEyesPrepare, OpenEyesMaskPairs: openEyesPrepare, LocalPortraitPrepare: localPortraitPrepare, LocalPortraitSubmit: localPortraitPrepare, ReflectionPrepare: reflectionSubmit, ReflectionSubmit: reflectionSubmit, GlareDonorSubmit: glareDonorSubmit, GlareDonorPrepare: glareDonorSubmit, GlareDonorCandidates: glareDonorSubmit, GlareSourceCandidates: glareDonorSubmit, GlareMaskPairs: manualMaskPairProjects{glare: glareDonorSubmit, reflection: reflectionSubmit, local: localPortraitPrepare}, InterchangeArchiveStatus: archiveActions, InterchangeArchiveActions: archiveActions, InterchangeArchiveBegin: archiveBegin, InterchangeSnapshots: snapshotPreparation, ExportSigner: exportSigner, InterchangeCreator: interchangeCreator, InterchangeMaskCandidates: interchange.ImportPersonMaskCandidateService{DB: pool, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}, InterchangeImportReviewDecider: interchange.ImportReviewDecisionService{DB: pool, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}, InterchangeImportReviewProposals: interchange.ImportReviewProposalListService{DB: pool, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}, InterchangeReviewDecider: httpapi.InterchangeReviewDecideService{Service: interchange.Service{DB: pool, ApprovalStyleFactory: importStyleFactory}, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, Drafts: v2draft.Service{DB: pool, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}}, InterchangeReviewCloser: httpapi.InterchangeReviewCloseService{Service: interchange.Service{DB: pool}, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}, DeliveryCreator: deliveryCreator, MultiProposals: &v2render.MultiProposalService{DB: pool, PreviewSigner: storage, SRGBSHA: profileSHAs["srgb_v1"], Source: v2render.SQLAdoptedMultiSource{DB: pool}, Objects: storage, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}, PanoramaSubmit: panoramaSubmit, FocusStackSubmit: focusSubmit, PerspectiveLinesSubmit: linesSubmit, PerspectiveDetectedLines: &v2optical.PerspectiveDetectedLinesService{DB: pool}, PerspectiveProposals: &v2render.PerspectiveProposalService{DB: pool, Objects: storage, PreviewSigner: storage, SRGBSHA: profileSHAs["srgb_v1"], Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket}, NegativeSubmit: negativeSubmit, PerspectiveSubmit: perspectiveSubmit, RelightAnalysis: relightAnalysis, RelightSubmit: relightSubmit, Config: cfg, DB: pool, Auth: authService, Uploads: uploadService, Portal: portalService, Reviews: reviewService, SelectedPreviewKey: auth.MAC(pepper, "v26-selected-preview-capability"), ReviewPreviews: &review.CandidatePreviewService{Catalog: review.SQLCandidatePreviewCatalog{DB: pool, Bucket: objectstore.ConfigFromEnv(cfg.Environment == "development").Bucket, SRGBSHA: profileSHAs["srgb_v1"]}, Source: reviewService, Selection: reviewService, RestorationSelection: reviewService, Objects: storage, Signer: storage, SRGBSHA: profileSHAs["srgb_v1"]}, Media: mediaService, ContactSheets: contactSheetService, AnalysisAdmission: analysisAdmission, TrainingAdmission: trainingAdmission, RenderCalibrations: renderCalibrations, RenderQueue: renderAdmission, RegionQueue: regionAdmission, SemanticPropose: semanticPropose, SemanticReviewApply: semanticReviewApply, SemanticReviewClose: semanticReviewClose}).HTTPServer()
	failure := make(chan error, 1)
	go func() { failure <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case err := <-failure:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
