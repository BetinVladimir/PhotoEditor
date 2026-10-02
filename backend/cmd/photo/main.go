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
	"photoeditor/backend/internal/gateway"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/mail"
	"photoeditor/backend/internal/maintenance"
	"photoeditor/backend/internal/media"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/pagination"
	"photoeditor/backend/internal/portal"
	"photoeditor/backend/internal/queue"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/upload"
	"photoeditor/backend/internal/v2render"
	"photoeditor/backend/internal/workflow"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err.Error())
		os.Exit(1)
	}
}
func run() error {
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
		store, err := objectstore.New(objectstore.ConfigFromEnv(cfg.Environment == "development"))
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
		return workflow.RunScheduler(ctx, cfg.DatabaseURL, cfg.Listen, objectstore.ConfigFromEnv(cfg.Environment == "development"), os.Getenv("PHOTO_ICC_ROOT"))
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

	var renderCalibrations v2render.RenderCalibrationCatalog
	if cfg.RenderCalibrationFile != "" {
		catalog, err := v2render.LoadPinnedRenderCalibrationCatalog(cfg.RenderCalibrationFile, cfg.RenderCalibrationSHA)
		if err != nil {
			return errors.New("render calibration catalog unavailable or invalid")
		}
		renderCalibrations = catalog
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
	server := (&httpapi.Server{Config: cfg, DB: pool, Auth: authService, Uploads: uploadService, Portal: portalService, Reviews: reviewService, Media: mediaService, ContactSheets: contactSheetService, AnalysisAdmission: analysisAdmission, TrainingAdmission: trainingAdmission, RenderCalibrations: renderCalibrations}).HTTPServer()
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
