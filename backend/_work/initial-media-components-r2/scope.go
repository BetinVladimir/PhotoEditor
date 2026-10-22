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
	"photoeditor/backend/internal/taskrecovery"
	"sort"
	"time"
)

type binding struct {
	Event   string            `json:"event_id"`
	Tenant  string            `json:"tenant_id"`
	Version string            `json:"version_id"`
	Run     string            `json:"derived_run_id"`
	Tasks   map[string]string `json:"tasks"`
}
type deferBinding struct {
	Immutable string `json:"immutable_task_sha256"`
	Task      string `json:"task_id"`
	Tenant    string `json:"tenant_id"`
	Run       string `json:"run_id"`
	Upload    string `json:"upload_id"`
	SHA       string `json:"expected_sha256"`
	Bytes     int64  `json:"expected_bytes"`
}
type scope struct {
	deferred map[string]deferBinding
	proof    *deferralReadback
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
func (s *scope) withDeferrals(raw []byte, pin string) error {
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != pin {
		return errors.New("six deferral scope digest drift")
	}
	var v struct {
		Bindings []deferBinding `json:"bindings"`
	}
	if json.Unmarshal(raw, &v) != nil || len(v.Bindings) != 6 {
		return errors.New("exact six original public deferrals required")
	}
	s.deferred = map[string]deferBinding{}
	for _, b := range v.Bindings {
		if b.Tenant != "0c43c2b6-58bb-4bd0-b184-83b8c51a108c" || b.Task == "" || b.Run == "" || b.Upload == "" || len(b.SHA) != 64 || len(b.Immutable) != 64 || b.Bytes <= 0 {
			return errors.New("invalid public deferral binding")
		}
		if _, ok := s.tasks[b.Task]; ok {
			return errors.New("deferral overlaps media task")
		}
		if _, ok := s.deferred[b.Task]; ok {
			return errors.New("duplicate deferral")
		}
		s.deferred[b.Task] = b
	}
	return nil
}
func (s scope) selected(id uuid.UUID, class string, profile []byte) error {
	_, own := s.tasks[id.String()]
	_, deferOnly := s.deferred[id.String()]
	if (!own && !deferOnly) || class != "media" {
		return errors.New("foreign dispatch selection")
	}
	if !json.Valid(profile) {
		return errors.New("invalid sealed profile")
	}
	return nil
}

type deferralRow struct {
	Task      string     `json:"task_id"`
	Immutable string     `json:"immutable_task_sha256"`
	When      *time.Time `json:"admission_after"`
	Reason    *string    `json:"terminal_reason"`
}
type deferralReadback struct {
	Selected   []string      `json:"selected_tasks"`
	Before     []deferralRow `json:"before"`
	After      []deferralRow `json:"after"`
	Completion bool          `json:"six_ingest_completed"`
}

func (s scope) deferRows(ctx context.Context, tx pgx.Tx) ([]deferralRow, error) {
	if len(s.deferred) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, 6)
	for id := range s.deferred {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows, e := tx.Query(ctx, `SELECT t.id::text,(to_jsonb(t)-'admission_after'-'terminal_reason')::text,t.admission_after,t.terminal_reason,(SELECT count(*) FROM photo.eligible_dispatch_nodes(t.id)) FROM photo.tasks t WHERE t.id::text=ANY($1::text[]) ORDER BY t.id`, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []deferralRow
	for rows.Next() {
		var row deferralRow
		var raw string
		var eligible int
		if e = rows.Scan(&row.Task, &raw, &row.When, &row.Reason, &eligible); e != nil {
			return nil, e
		}
		h := sha256.Sum256([]byte(raw))
		row.Immutable = hex.EncodeToString(h[:])
		b, ok := s.deferred[row.Task]
		if !ok || row.Immutable != b.Immutable || eligible != 0 {
			return nil, errors.New("six original public task CAS or no-worker condition changed")
		}
		if row.Reason != nil && *row.Reason != "waiting_worker" {
			return nil, errors.New("unexpected existing six deferral reason")
		}
		result = append(result, row)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(result) != 6 {
		return nil, errors.New("missing original public task")
	}
	return result, nil
}
func validateDeferrals(before, after []deferralRow, selected map[string]bool, start, end time.Time) error {
	if len(before) != len(after) {
		return errors.New("six deferral readback membership changed")
	}
	for i, a := range before {
		b := after[i]
		if a.Task != b.Task || a.Immutable != b.Immutable {
			return errors.New("six immutable metadata changed")
		}
		aa, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		if !selected[a.Task] {
			if string(aa) != string(bb) {
				return errors.New("nonselected public deferral changed")
			}
			continue
		}
		if b.Reason == nil || *b.Reason != "waiting_worker" || b.When == nil || b.When.Before(start) || b.When.After(end.Add(3*time.Second)) {
			return errors.New("selected public task lacks bounded normal waiting_worker deferral")
		}
	}
	return nil
}
func terminalStates() []string {
	return []string{string(taskrecovery.Succeeded), string(taskrecovery.Failed), string(taskrecovery.Cancelled), string(taskrecovery.Skipped)}
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
	if table == "tasks" {
		if b, ok := s.deferred[text(row, "id")]; ok {
			if text(row, "tenant_id") != b.Tenant || text(row, "run_id") != b.Run || text(row, "operation") != "asset.verify" || text(row, "resource_class") != "media" || text(row, "state") != "ready" {
				return false, errors.New("public deferral immutable scope/state changed")
			}
			reason := text(row, "terminal_reason")
			if reason != "" && reason != "waiting_worker" {
				return false, errors.New("public deferral reason not waiting_worker")
			}
			delete(row, "admission_after")
			delete(row, "terminal_reason")
		}
	}

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
	for id := range s.deferred {
		ids = append(ids, id)
	}
	events := make([]string, 0, 17)
	for id := range s.events {
		events = append(events, id)
	}
	var n int
	if e := tx.QueryRow(ctx, `SELECT count(*) FROM photo.tasks WHERE NOT(id::text=ANY($1::text[])) AND ((state<>'blocked' AND state<>ALL($2::text[])) OR cancel_requested AND state<>ALL($2::text[]))`, ids, terminalStates()).Scan(&n); e != nil {
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
