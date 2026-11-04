// Command video-demo runs the video subsystem DEMO: the real video API, the real portal viewer session and an
// in-process worker, with ONE built-in dev user instead of the production login. It listens on loopback only and
// connects as a database superuser that it narrows with SET ROLE per pool, so every call still runs under the
// production role. Never deploy it; production serves the same routes from cmd/photo behind real authentication.
//
//	go run ./cmd/video-demo -dsn 'host=/tmp port=55433 user=photo_test dbname=scratch_v' -store /tmp/video-demo
package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/portal"
	videoapi "photoeditor/backend/internal/video/api"
	"photoeditor/backend/internal/video/worker"
)

//go:embed ui
var uiFiles embed.FS

const email = "video-demo@example.invalid"

func main() {
	dsn := flag.String("dsn", "", "superuser DSN of the database with migrations up to 02706")
	store := flag.String("store", "", "directory for objects and temp files")
	addr := flag.String("addr", "127.0.0.1:8088", "loopback listen address")
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "ffmpeg binary")
	ffprobe := flag.String("ffprobe", "ffprobe", "ffprobe binary")
	encoder := flag.String("encoder", "libx264", "default encoder: libx264 or h264_nvenc")
	flag.Parse()
	if *dsn == "" || *store == "" {
		flag.Usage()
		os.Exit(2)
	}
	if host, _, err := net.SplitHostPort(*addr); err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		log.Fatal("video-demo has no real authentication: -addr must be a loopback address")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *dsn, *store, *addr, *ffmpeg, *ffprobe, *encoder); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func rolePool(ctx context.Context, dsn, role string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if role != "" {
		cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
			_, err := c.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
			return err
		}
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

func seed(ctx context.Context, admin *pgxpool.Pool) (user, tenant, project uuid.UUID, err error) {
	tx, err := admin.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `INSERT INTO photo.users(email_normalized) VALUES($1) ON CONFLICT(email_normalized) DO UPDATE SET email_normalized=EXCLUDED.email_normalized RETURNING id`, email).Scan(&user); err != nil {
		return
	}
	err = tx.QueryRow(ctx, `SELECT id FROM photo.tenants WHERE owner_user_id=$1 AND name='Video demo'`, user).Scan(&tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.QueryRow(ctx, `INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Video demo',$1,'{"storage_bytes":100000000000}','{}') RETURNING id`, user).Scan(&tenant); err != nil {
			return
		}
		if _, err = tx.Exec(ctx, `INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'owner','active')`, tenant, user); err != nil {
			return
		}
	} else if err != nil {
		return
	}
	if _, err = tx.Exec(ctx, `INSERT INTO photo.tenant_usage(tenant_id) VALUES($1) ON CONFLICT DO NOTHING`, tenant); err != nil {
		return
	}
	err = tx.QueryRow(ctx, `SELECT id FROM photo.projects WHERE tenant_id=$1 AND name='Demo film'`, tenant).Scan(&project)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Demo film') RETURNING id`, tenant).Scan(&project)
	}
	if err != nil {
		return
	}
	err = tx.Commit(ctx)
	return
}

func run(ctx context.Context, dsn, store, addr, ffmpeg, ffprobe, encoder string) error {
	pools := map[string]*pgxpool.Pool{}
	for _, role := range []string{"", "photo_api", "photo_scheduler", "photo_worker", "photo_portal", "photo_maintenance"} {
		p, err := rolePool(ctx, dsn, role)
		if err != nil {
			return err
		}
		defer p.Close()
		pools[role] = p
	}
	user, tenant, project, err := seed(ctx, pools[""])
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	for _, d := range []string{"objects", "tmp", "work"} {
		if err := os.MkdirAll(filepath.Join(store, d), 0o750); err != nil {
			return err
		}
	}
	objects := worker.LocalStore{Root: filepath.Join(store, "objects")}
	// The MAC key of share secrets must survive restarts or every issued link dies with the process.
	pepper, err := os.ReadFile(filepath.Join(store, "pepper"))
	if err != nil || len(pepper) != 32 {
		pepper = make([]byte, 32)
		if _, err := rand.Read(pepper); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(store, "pepper"), pepper, 0o600); err != nil {
			return err
		}
	}
	const bucket = "photo-video"
	svc := &videoapi.Service{Pool: pools["photo_api"], Scheduler: pools["photo_scheduler"], Portal: pools["photo_portal"], Pepper: pepper,
		Store: objects, Bucket: bucket, TempDir: filepath.Join(store, "tmp"),
		Authz: func(_ context.Context, _ pgx.Tx, who auth.Identity, t uuid.UUID, _ domain.Role) (domain.Role, error) {
			if t != tenant || who.UserID != user {
				return "", domain.NewError("NOT_FOUND", 404)
			}
			return domain.Owner, nil
		}}
	portalSvc := portal.Service{DB: pools["photo_portal"], Pepper: pepper}

	r := chi.NewRouter()
	r.Get("/api/v1/demo/context", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tenant_id": tenant, "project_id": project, "encoder": encoder})
	})
	videoapi.Routes(r, svc, func(req *http.Request, _ bool) (auth.Identity, uuid.UUID, error) {
		t, err := uuid.Parse(chi.URLParam(req, "t"))
		if err != nil {
			return auth.Identity{}, uuid.Nil, domain.NewError("NOT_FOUND", 404)
		}
		return auth.Identity{UserID: user, SessionID: uuid.New()}, t, nil
	})
	videoapi.PortalRoutes(r, svc, func(req *http.Request, permission string) (videoapi.Viewer, error) {
		ck, err := req.Cookie("portal_session")
		if err != nil || ck.Value == "" {
			return videoapi.Viewer{}, domain.NewError("INVALID_GUEST_SESSION", 401)
		}
		who, err := portalSvc.Authorize(req.Context(), ck.Value, "", permission, false)
		if err != nil {
			return videoapi.Viewer{}, err
		}
		return videoapi.Viewer{Tenant: who.TenantID, Share: who.ShareID, Collection: who.CollectionID, Session: who.SessionID}, nil
	})
	r.Post("/api/v1/portal/sessions", func(w http.ResponseWriter, req *http.Request) {
		var in portal.ExchangeRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096)).Decode(&in); err != nil {
			http.Error(w, `{"code":"INVALID_JSON"}`, 400)
			return
		}
		if in.Locale == "" {
			in.Locale = "en"
		}
		out, err := portalSvc.Exchange(req.Context(), in)
		if err != nil {
			e := domain.AsError(err)
			w.WriteHeader(e.HTTPStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": e.Code})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "portal_session", Value: out.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: out.ExpiresAt})
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"expires_at": out.ExpiresAt, "capabilities": out.Capabilities})
	})
	sub, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		return err
	}
	r.Handle("/*", http.FileServerFS(sub))

	wk := &worker.Worker{Pool: pools["photo_worker"], Store: objects, FFmpeg: ffmpeg, FFprobe: ffprobe, Encoder: encoder, Bucket: bucket, WorkDir: filepath.Join(store, "work")}
	go func() {
		for ctx.Err() == nil {
			if err := wk.Loop(ctx, 500*time.Millisecond); err != nil && ctx.Err() == nil {
				log.Printf("worker: %v (retrying)", err)
				time.Sleep(2 * time.Second)
			}
		}
	}()

	sw := &worker.Sweeper{Pool: pools["photo_maintenance"], Store: objects}
	go sw.Loop(ctx, 30*time.Second)

	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sh, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(sh)
	}()
	log.Printf("video-demo on http://%s  tenant=%s project=%s encoder=%s store=%s", addr, tenant, project, encoder, store)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
