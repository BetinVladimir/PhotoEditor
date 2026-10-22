// Operator-only transaction guard. Never an alternative scheduler/admission algorithm.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sort"
)

type binding struct {
	Event   string            `json:"event_id"`
	Tenant  string            `json:"tenant_id"`
	Version string            `json:"version_id"`
	Run     string            `json:"derived_run_id"`
	Tasks   map[string]string `json:"tasks"`
}
type scope struct {
	Jobs     []binding `json:"jobs"`
	tasks    map[string]binding
	runs     map[string]binding
	events   map[string]binding
	versions map[string]binding
}

func parseScope(raw []byte, pin string) (scope, error) {
	var s scope
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != pin {
		return s, errors.New("scope digest drift")
	}
	if json.Unmarshal(raw, &s) != nil || len(s.Jobs) != 17 {
		return s, errors.New("exact seventeen jobs required")
	}
	s.tasks = map[string]binding{}
	s.runs = map[string]binding{}
	s.events = map[string]binding{}
	s.versions = map[string]binding{}
	for _, b := range s.Jobs {
		e, err := uuid.Parse(b.Event)
		if err != nil || b.Run != uuid.NewSHA1(e, []byte("media-retry-run")).String() || len(b.Tasks) != 4 {
			return s, errors.New("invalid deterministic run")
		}
		if _, err = uuid.Parse(b.Tenant); err != nil {
			return s, err
		}
		if _, err = uuid.Parse(b.Version); err != nil {
			return s, err
		}
		if _, ok := s.events[b.Event]; ok {
			return s, errors.New("duplicate event")
		}
		s.events[b.Event] = b
		s.runs[b.Run] = b
		s.versions[b.Version] = b
		for _, v := range []string{"thumb320", "thumb640", "preview1600", "preview2560"} {
			id := b.Tasks[v]
			if id != uuid.NewSHA1(e, []byte(v)).String() {
				return s, errors.New("invalid deterministic task")
			}
			if _, ok := s.tasks[id]; ok {
				return s, errors.New("duplicate task")
			}
			s.tasks[id] = b
		}
	}
	if len(s.runs) != 17 || len(s.tasks) != 68 {
		return s, errors.New("scope cardinality")
	}
	return s, nil
}
func text(row map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(row[key], &s)
	return s
}
func (s scope) selected(id uuid.UUID, class string, profile []byte) error {
	if _, ok := s.tasks[id.String()]; !ok || class != "media" {
		return errors.New("foreign dispatch selection")
	}
	if !json.Valid(profile) {
		return errors.New("invalid sealed profile")
	}
	return nil
}

// Only these explicit row bindings may change. Unknown tables and unknown rows
// remain protected, even when that conservatively refuses a legitimate own effect.
func (s scope) own(table string, row map[string]json.RawMessage) bool {
	tenant := text(row, "tenant_id")
	match := func(b binding, ok bool) bool { return ok && tenant == b.Tenant }
	switch table {
	case "tasks":
		b, ok := s.tasks[text(row, "id")]
		return match(b, ok) && text(row, "run_id") == b.Run && (text(row, "operation") == "media.thumbnail" || text(row, "operation") == "media.preview") && text(row, "resource_class") == "media"
	case "runs":
		b, ok := s.runs[text(row, "id")]
		return match(b, ok)
	case "media_retry_jobs":
		b, ok := s.events[text(row, "event_id")]
		return match(b, ok) && text(row, "asset_version_id") == b.Version
	case "derivatives":
		b, ok := s.versions[text(row, "asset_version_id")]
		return match(b, ok)
	case "task_attempts", "resource_reservations", "task_results":
		b, ok := s.tasks[text(row, "task_id")]
		return match(b, ok)
	}
	if table == "outbox" {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(row["payload"], &envelope) != nil {
			return false
		}
		b, ok := s.tasks[text(envelope, "task_id")]
		return match(b, ok) && text(envelope, "tenant_id") == b.Tenant && text(envelope, "run_id") == b.Run
	}
	return false
}

func (s scope) normalize(table string, row map[string]json.RawMessage) (bool, error) {
	if table == "worker_registrations" || table == "resource_nodes" {
		for _, k := range []string{"heartbeat_at", "last_heartbeat_at", "last_seen_at", "updated_at", "resident_models", "resident_model_ids", "available_ram_bytes", "available_vram_bytes", "capacity_report", "capacity_reported_at", "capacity_sequence", "report_sequence", "loaded_models", "last_seen", "capacity_policy_reported_at", "model_residency", "model_residency_reported_at", "reported_at", "ram_available_bytes", "vram_available_bytes", "scratch_available_bytes"} {
			delete(row, k)
		}
	}
	// Only this media node's own lease fields may move; slot authority
	// (identity/class/capacity) remains in the protected digest.
	if table == "resource_slots" && text(row, "node_id") == "rtx-beeloyai-media-eabf3c5add7d" {
		id := text(row, "task_id")
		if id != "" {
			b, ok := s.tasks[id]
			if !ok || text(row, "tenant_id") != b.Tenant {
				return false, errors.New("foreign media slot")
			}
		}
		for _, key := range []string{"task_id", "tenant_id", "lease_until", "fencing_token"} {
			delete(row, key)
		}
	}
	// record_dispatch() updates exactly these fairness counters for the
	// selected media tenant; all other tenant rows/authority stay hashed.
	if table == "scheduler_tenants" && text(row, "tenant_id") == s.Jobs[0].Tenant {
		for _, key := range []string{"virtual_finish", "last_dispatch_at", "preview_streak"} {
			delete(row, key)
		}
	}
	if s.own(table, row) {
		return true, nil
	}

	return false, nil
}

type snapshot map[string]string

// Scan protected rows in memory only; emit digests, never row payloads. All photo
// tables are protected except liveness-only fields on registered workers/nodes.
// Bounds reject large databases; the root must review actual deployment inventory.
func capture(ctx context.Context, tx pgx.Tx, s scope) (snapshot, error) {
	rows, e := tx.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo' AND c.relkind IN ('r','p') AND NOT c.relispartition ORDER BY c.relname`)
	if e != nil {
		return nil, e
	}
	var tables []string
	for rows.Next() {
		var n string
		if e = rows.Scan(&n); e != nil {
			rows.Close()
			return nil, e
		}
		tables = append(tables, n)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	result := snapshot{}
	total := 0
	bytesSeen := 0
	for _, table := range tables {
		q := fmt.Sprintf("SELECT to_jsonb(t) FROM %s t LIMIT 10001", pgx.Identifier{"photo", table}.Sanitize())
		r, err := tx.Query(ctx, q)
		if err != nil {
			return nil, err
		}
		var hashes []string
		n := 0
		for r.Next() {
			var raw []byte
			if err = r.Scan(&raw); err != nil {
				r.Close()
				return nil, err
			}
			n++
			total++
			bytesSeen += len(raw)
			if n > 10000 || total > 100000 || bytesSeen > 64<<20 {
				r.Close()
				return nil, errors.New("protected snapshot bounded limit")
			}
			var row map[string]json.RawMessage
			if json.Unmarshal(raw, &row) != nil {
				r.Close()
				return nil, errors.New("invalid database row")
			}
			skip, err := s.normalize(table, row)
			if err != nil {
				r.Close()
				return nil, err
			}
			if skip {
				continue
			}
			canonical, err := json.Marshal(row)
			if err != nil {
				r.Close()
				return nil, err
			}
			h := sha256.Sum256(canonical)
			hashes = append(hashes, hex.EncodeToString(h[:]))
		}
		err = r.Err()
		r.Close()
		if err != nil {
			return nil, err
		}
		sort.Strings(hashes)
		h := sha256.Sum256([]byte(fmt.Sprint(hashes)))
		result[table] = hex.EncodeToString(h[:])
	}
	return result, nil
}
func equalSnapshots(a, b snapshot) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(aa, bb)
}
func outsideZero(ctx context.Context, tx pgx.Tx, s scope) error {
	ids := make([]string, 0, 68)
	for id := range s.tasks {
		ids = append(ids, id)
	}
	events := make([]string, 0, 17)
	for id := range s.events {
		events = append(events, id)
	}
	var n int
	if e := tx.QueryRow(ctx, `SELECT count(*) FROM photo.tasks WHERE NOT(id::text=ANY($1::text[])) AND (state NOT IN ('blocked','succeeded','failed','cancelled') OR cancel_requested AND state NOT IN ('succeeded','failed','cancelled'))`, ids).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return errors.New("outside active tasks require root review")
	}
	if e := tx.QueryRow(ctx, `SELECT count(*) FROM photo.media_retry_jobs WHERE NOT(event_id::text=ANY($1::text[])) AND status IN ('queued','materializing','running')`, events).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return errors.New("outside active media jobs require root review")
	}
	return nil
}
