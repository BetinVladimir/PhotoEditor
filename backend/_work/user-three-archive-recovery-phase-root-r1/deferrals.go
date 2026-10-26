package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"sort"
	"time"
)

const sixDeferralPath = "/Users/freelancer/Documents/PhotoEditor/work/rtx-initial-media-components-r2/six-normal-defer-scope.json"
const sixDeferralSHA = "33ccfa89fbe67393a977f17420e23f3db2d3ada7067d6a706f8b1f17ef17b600"

func (s *scope) loadDeferrals() error {
	raw, e := os.ReadFile(sixDeferralPath)
	if e != nil {
		return e
	}
	s.proof = &deferralReadback{}
	return s.withDeferrals(raw, sixDeferralSHA)
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
		overlap := false
		for _, t := range s.Tasks {
			if t.ID.String() == b.Task {
				overlap = true
			}
		}
		if overlap {
			return errors.New("deferral overlaps media task")
		}
		if _, ok := s.deferred[b.Task]; ok {
			return errors.New("duplicate deferral")
		}
		s.deferred[b.Task] = b
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

// At most six exact public deferrals followed by one own selection. Own
// waiting_resource still ends this invocation; no subsequent own task is picked.
func dispatchBounded(s scope, selected map[string]bool, pick func(func(uuid.UUID, string, []byte) error) (bool, bool, error)) (int, error) {
	count := 0
	for i := 0; i < 7; i++ {
		ownSelected := false
		picked, changed, e := pick(func(id uuid.UUID, class string, raw []byte) error {
			if selected[id.String()] {
				return errors.New("duplicate bounded selection")
			}
			h := sha256.Sum256(raw)
			for _, t := range s.Tasks {
				if t.ID == id && class == t.Class && t.ProfileSHA == hex.EncodeToString(h[:]) {
					selected[id.String()] = true
					ownSelected = true
					return nil
				}
			}
			if _, ok := s.deferred[id.String()]; !ok || class != "media" || !json.Valid(raw) {
				return errors.New("outside exact six/Apply selection")
			}
			selected[id.String()] = true
			return nil
		})
		if e != nil {
			return 0, e
		}
		if changed {
			count++
		}
		if ownSelected || !picked {
			break
		}
	}
	return count, nil
}
