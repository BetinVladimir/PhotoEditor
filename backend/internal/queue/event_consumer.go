package queue

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"net"
	"net/http"
	"net/url"
	"photoeditor/backend/internal/database"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

type EventAcceptor interface {
	Accept(context.Context, OrchestratorEvent) (string, error)
}
type EventRepository struct {
	DB interface {
		Begin(context.Context) (pgx.Tx, error)
	}
}

func (r EventRepository) Accept(ctx context.Context, e OrchestratorEvent) (string, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(context.Background())
	var outcome string
	if err = tx.QueryRow(ctx, "SELECT photo.accept_orchestrator_event($1,$2,$3,$4::jsonb)", e.TenantID, e.EventID, e.Type, string(e.Payload)).Scan(&outcome); err != nil {
		return "", err
	}
	switch outcome {
	case "accepted", "duplicate", "stale", "rejected":
	default:
		return "", errors.New("invalid event acceptance outcome")
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return outcome, nil
}

// ProcessEventDelivery ACKs only committed outcomes; unknown DB outcomes leave
// the delivery unacknowledged and force a channel close/reconnect by the caller.
func ProcessEventDelivery(ctx context.Context, repo EventAcceptor, d amqp.Delivery) error {
	e, err := DecodeOrchestratorEvent(d.Body, d.RoutingKey, d.MessageId)
	if err != nil {
		return d.Reject(false)
	}
	outcome, err := repo.Accept(ctx, e)
	if err != nil {
		return err
	}
	if outcome == "rejected" {
		return d.Reject(false)
	}
	switch outcome {
	case "accepted", "duplicate", "stale":
		return d.Ack(false)
	}
	return errors.New("invalid event acceptance outcome")
}

const eventAcceptanceSourceSHA256 = "bcc70ae37283d8e2c767ef917c8057560b1c6d7b22dfa7e3d2625cbf61327af1"

const eventProgressSourceSHA256 = "172bad825604ca4aa63a3fb3148e35354bb8e832cdb907bb28c5e82bf5f6cd83"
const eventHeartbeatSourceSHA256 = "30dc07436d1cb12dd806b9ac1bce46be37ff46aed27bedf21b8a668e570bd844"

func RequireEventConsumerSchema(ctx context.Context, db database.RoleQueryer) error {
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT FROM pg_proc p
 WHERE p.oid=to_regprocedure('photo.accept_orchestrator_event(uuid,uuid,text,jsonb)')
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner'
 AND p.prokind='f' AND p.provolatile='v' AND p.proparallel='u' AND NOT p.proretset
 AND p.prorettype='text'::regtype AND p.pronargdefaults=0
 AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
 AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')=$1
 AND has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
  WHERE a.privilege_type<>'EXECUTE' OR a.is_grantable OR a.grantee NOT IN(p.proowner,(SELECT oid FROM pg_roles WHERE rolname='photo_event_consumer'))))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.emit_task_progress(uuid,integer,bigint,uuid)')
 AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner' AND p.proconfig=ARRAY['search_path=pg_catalog, photo']::text[]
 AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')=$2
 AND NOT has_function_privilege(current_user,p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner)))a WHERE a.grantee<>p.proowner OR a.is_grantable))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.heartbeat_task_pre_v23_series_device(uuid,integer,bigint,uuid,jsonb)')
 AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')=$3)
 AND to_regclass('photo.task_progress_outbox_lookup') IS NOT NULL
 AND EXISTS(SELECT FROM pg_attribute WHERE attrelid='photo.inbox'::regclass AND attname='payload_sha256' AND atttypid='text'::regtype AND NOT attnotnull AND NOT attisdropped)
 AND NOT EXISTS(SELECT FROM pg_class c WHERE c.oid IN('photo.inbox'::regclass,'photo.outbox'::regclass,'photo.run_events'::regclass,'photo.tasks'::regclass,'photo.task_attempts'::regclass,'photo.reviews'::regclass) AND (NOT c.relrowsecurity OR NOT c.relforcerowsecurity))
 AND NOT EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo' AND c.relkind IN('r','p','v','m','f') AND has_table_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,DELETE'))
 AND EXISTS(SELECT FROM pg_proc p WHERE p.oid=to_regprocedure('photo.record_event_dead_letter(text,text,jsonb)') AND p.prosecdef AND pg_get_userbyid(p.proowner)='photo_fn_owner' AND NOT has_function_privilege(current_user,p.oid,'EXECUTE'))`, eventAcceptanceSourceSHA256, eventProgressSourceSHA256, eventHeartbeatSourceSHA256).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("event consumer schema unavailable")
	}
	return nil
}
func eventSession(ctx context.Context, url string, repo EventAcceptor, ready *atomic.Bool) error {
	conn, err := dial(url)
	if err != nil {
		return errors.New("event broker unavailable")
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err = ch.Qos(1, 0, false); err != nil {
		return err
	}
	deliveries, err := ch.Consume("q.orchestrator.events", "orchestrator-events-v1", false, false, false, false, nil)
	if err != nil {
		return err
	}
	ready.Store(true)
	defer ready.Store(false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("event consumer channel closed")
			}
			call, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = ProcessEventDelivery(call, repo, d)
			cancel()
			if err != nil {
				return errors.New("event delivery interrupted")
			}
		}
	}
}
func RunEventConsumer(ctx context.Context, dsn, url, listen string) error {
	if err := validateEventConsumerConfig(dsn, url); err != nil {
		return err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("event database configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_event_consumer"
	database.GuardApplicationRole(cfg)
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("event database unavailable")
	}
	defer db.Close()
	if err = database.RequireApplicationRole(ctx, db, "photo_event_consumer"); err != nil {
		return err
	}
	if err = RequireEventConsumerSchema(ctx, db); err != nil {
		return err
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	var ready atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		probe, c := context.WithTimeout(r.Context(), time.Second)
		defer c()
		if !ready.Load() || db.Ping(probe) != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	serve := make(chan error, 1)
	go func() { serve <- server.Serve(listener) }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runEventReconnect(child, func(c context.Context) error { return eventSession(c, url, EventRepository{db}, &ready) }, eventReconnectWait)
	}()
	select {
	case <-ctx.Done():
	case err = <-serve:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	cancel()
	<-done
	shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_ = server.Shutdown(shutdown)
	return err
}

// Every failed session is closed before backoff. The wait is cancellable and
// bounded, preventing DB/broker failure from generating an immediate hot loop.
func eventReconnectWait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func runEventReconnect(ctx context.Context, session func(context.Context) error, wait func(context.Context, time.Duration) bool) {
	delay := time.Second
	for ctx.Err() == nil {
		_ = session(ctx)
		if ctx.Err() != nil {
			return
		}
		if !wait(ctx, delay) {
			return
		}
		delay *= 2
		if delay > 15*time.Second {
			delay = 15 * time.Second
		}
	}
}

// A new service must never discover ambient PG* defaults or guest-localhost
// broker targets when its project configuration was omitted.
func validateEventConsumerConfig(dsn, broker string) error {
	bad := errors.New("explicit event database and broker configuration required")
	if strings.TrimSpace(dsn) == "" || strings.TrimSpace(broker) == "" {
		return bad
	}
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Port() == "" || u.User == nil || u.User.Username() == "" || strings.Trim(u.Path, "/") == "" {
			return bad
		}
	} else {
		// Explicit keyword DSNs are supported; no host/port/user/database is inherited.
		fields := regexp.MustCompile(`(?:^|\s)(host|port|user|dbname)=('(?:[^'\\]|\\.)*'|[^\s]+)`).FindAllStringSubmatch(dsn, -1)
		seen := map[string]bool{}
		for _, f := range fields {
			seen[f[1]] = strings.Trim(f[2], "'") != ""
		}
		for _, key := range []string{"host", "port", "user", "dbname"} {
			if !seen[key] {
				return bad
			}
		}
	}
	if _, err := pgxpool.ParseConfig(dsn); err != nil {
		return bad
	}
	u, err := url.Parse(broker)
	if err != nil || (u.Scheme != "amqp" && u.Scheme != "amqps") || u.Hostname() == "" || u.User == nil || u.User.Username() == "" {
		return bad
	}
	return nil
}
