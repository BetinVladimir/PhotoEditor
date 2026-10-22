package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// SQLAuthoredFinalReplayPreparation owns deployment-selected policy, not an API
// callback/report-derived Expected. Zero configuration cannot qualify a GPU child.
type SQLAuthoredFinalReplayPreparation struct {
	Native   SQLAuthoredNativeReplayService
	policies []authoredFinalTrustedReplayPolicy
}
type authoredFinalTrustedReplayPolicy struct {
	expected v2qualification.Expected
	report   v2input.Ref
}

// NewSQLAuthoredFinalReplayPreparation selects an operator-pinned canonical
// deployment file; current DB policy AND actual physical report are still checked
// independently by Prepare on every new admission.
func NewSQLAuthoredFinalReplayPreparation(native SQLAuthoredNativeReplayService, path, pin string) (*SQLAuthoredFinalReplayPreparation, error) {
	if native.Scheduler == nil || native.Objects == nil || native.Bucket == "" || !validReplayDigest(pin) {
		return nil, v2qualification.ErrUnqualified
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Mode().Perm()&022 != 0 || before.Size() < 2 || before.Size() > 1<<20 {
		return nil, v2qualification.ErrUnqualified
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	stat, err := file.Stat()
	if err != nil || !os.SameFile(before, stat) {
		file.Close()
		return nil, v2qualification.ErrUnqualified
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	closed := file.Close()
	if err != nil {
		return nil, err
	}
	if closed != nil {
		return nil, closed
	}
	hash := sha256.Sum256(raw)
	wire, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(wire, raw) || hex.EncodeToString(hash[:]) != pin || len(raw) > 1<<20 {
		return nil, v2qualification.ErrUnqualified
	}
	var document struct {
		SchemaVersion int `json:"schema_version"`
		Policies      []struct {
			Locks  v2qualification.DeploymentLocks `json:"locks"`
			Report v2input.Ref                     `json:"report"`
		} `json:"policies"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || document.SchemaVersion != 1 || len(document.Policies) == 0 || len(document.Policies) > 128 {
		return nil, v2qualification.ErrUnqualified
	}
	out := &SQLAuthoredFinalReplayPreparation{Native: native}
	seen := map[string]bool{}
	for _, entry := range document.Policies {
		expected, e := entry.Locks.Expected()
		if e != nil {
			return nil, e
		}
		owned, _, e := freezeNativeReplayExpected(expected)
		if e != nil || entry.Report.ArtifactID == uuid.Nil || !validReplayDigest(entry.Report.SHA256) {
			return nil, v2qualification.ErrUnqualified
		}
		key := owned.Backend + ":" + owned.ModelLockSHA
		if seen[key] {
			return nil, v2qualification.ErrUnqualified
		}
		seen[key] = true
		out.policies = append(out.policies, authoredFinalTrustedReplayPolicy{owned, entry.Report})
	}
	return out, nil
}
func (s *SQLAuthoredFinalReplayPreparation) selectPolicy(request v2input.AuthoredNativeReplayRequest) (v2qualification.Expected, v2input.Ref, error) {
	if s == nil || request.Original.Task.Operation != "image.upscale" || request.Original.Task.Version != "1" {
		return v2qualification.Expected{}, v2input.Ref{}, v2qualification.ErrUnqualified
	}
	var profile struct {
		Backend string `json:"backend"`
		Model   string `json:"model_lock_id"`
	}
	if json.Unmarshal([]byte(request.Original.Task.ProfileJCS), &profile) != nil {
		return v2qualification.Expected{}, v2input.Ref{}, v2qualification.ErrUnqualified
	}
	for _, p := range s.policies {
		if p.expected.Backend == profile.Backend && p.expected.ModelLockSHA == profile.Model {
			owned, _, err := freezeNativeReplayExpected(p.expected)
			return owned, p.report, err
		}
	}
	return v2qualification.Expected{}, v2input.Ref{}, v2qualification.ErrUnqualified
}

// PrepareRequiredFinalReplays consumes only the protected SQL-selected immutable
// blocked final task. No consumer result is required and no future roots are sealed.
func (s *SQLAuthoredFinalReplayPreparation) PrepareRequiredFinalReplays(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int, consumer uuid.UUID) (bool, bool, error) {
	if s == nil || s.Native.Scheduler == nil || tenant == uuid.Nil || parent == uuid.Nil || consumer == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return false, false, v2qualification.ErrUnqualified
	}
	read := func() ([]byte, error) {
		var raw []byte
		err := database.TenantTransaction(ctx, s.Native.Scheduler, tenant, func(tx pgx.Tx) error {
			var role string
			if e := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); e != nil {
				return e
			}
			if role != "photo_scheduler" {
				return v2input.ErrInvalid
			}
			return tx.QueryRow(ctx, "SELECT photo.read_authored_native_replay_initial_final_requests($1,$2,$3,$4,$5,$6)", tenant, parent, ordinal, consumer, who.UserID, who.SessionID).Scan(&raw)
		})
		return raw, err
	}
	before, err := read()
	if err != nil {
		return false, false, err
	}
	if len(before) == 0 || bytes.Equal(before, []byte("null")) {
		return false, false, v2input.ErrInvalid
	}
	var requests []string
	if json.Unmarshal(before, &requests) != nil || requests == nil || len(requests) > 4999 {
		return false, false, v2input.ErrInvalid
	}
	if len(requests) == 0 {
		return false, true, nil
	}
	allReady := true
	for _, raw := range requests {
		if err = ctx.Err(); err != nil {
			return true, false, err
		}
		request, e := v2input.DecodeAuthoredNativeReplayRequest([]byte(raw))
		if e != nil {
			return true, false, e
		}
		expected, report, e := s.selectPolicy(request)
		if e != nil {
			return true, false, e
		}
		qualifier := SQLAuthoredNativeReplayQualification{Scheduler: s.Native.Scheduler, Objects: s.Native.Objects, Bucket: s.Native.Bucket}
		proof, e := qualifier.Read(ctx, tenant, []byte(raw), true, expected, report)
		if e != nil {
			return true, false, e
		}
		_, policyRaw, e := freezeNativeReplayExpected(expected)
		if e != nil {
			return true, false, e
		}
		var progress *string
		e = database.TenantTransaction(ctx, s.Native.Scheduler, tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT photo.read_authored_native_replay_preparation_progress($1,$2,$3,$4,$5,$6,$7)", tenant, raw, who.UserID, who.SessionID, policyRaw, report.ArtifactID, report.SHA256).Scan(&progress)
		})
		if e != nil {
			return true, false, e
		}
		if progress == nil {
			return true, false, v2input.ErrConflict
		}
		switch *progress {
		case "absent", "staged":
			if _, e = s.Native.Prepare(ctx, who, []byte(raw), expected, report); e != nil {
				return true, false, e
			}
		case "sealed":
		default:
			return true, false, v2input.ErrInvalid
		}
		// Never restage an already sealed native child. Poll actual canonical adoption
		// and the real physical comparison; missing result is pending, never ready.
		_, ready, e := s.Native.ResumeEvidence(ctx, []byte(raw))
		if e != nil {
			return true, false, e
		}
		if e = qualifier.Recheck(ctx, proof); e != nil {
			return true, false, e
		}
		allReady = allReady && ready
	}
	after, err := read()
	if err != nil {
		return true, false, err
	}
	a, e := canonical.Bytes(before)
	b, f := canonical.Bytes(after)
	if e != nil || f != nil || !bytes.Equal(a, b) {
		return true, false, v2input.ErrConflict
	}
	return true, allReady, ctx.Err()
}
