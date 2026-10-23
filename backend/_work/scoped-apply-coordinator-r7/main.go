// Reviewed three-item production ApplyCoordinator tick; no automatic scheduling loop.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/workflow"
)

var project = uuid.MustParse("3f5e6a21-40ad-4b06-a9d9-370c021ba814")
var collection = uuid.MustParse("6829a61f-ba04-4474-87e1-118873df5920")
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type itemBinding struct {
	Item      uuid.UUID `json:"item_id"`
	Asset     uuid.UUID `json:"asset_id"`
	Version   uuid.UUID `json:"asset_version_id"`
	Master    uuid.UUID `json:"master_artifact_id"`
	MasterSHA string    `json:"master_sha256"`
}
type taskBinding struct {
	Class      string    `json:"resource_class"`
	ID         uuid.UUID `json:"task_id"`
	Operation  string    `json:"operation"`
	ProfileSHA string    `json:"execution_profile_sha256"`
}
type scope struct {
	terminal           bool
	Main2655InstallSHA string `json:"main2655_install_receipt_sha256"`
	deferred           map[string]deferBinding
	proof              *deferralReadback
	Items              []itemBinding `json:"items"`
	Tasks              []taskBinding `json:"tasks"`
	Nodes              []string      `json:"resource_node_ids"`
	Output             uuid.UUID     `json:"output_collection_id"`
	ReadbackSHA        string        `json:"database_readback_sha256"`
	Schema             int           `json:"schema"`
	ObservedAt         time.Time     `json:"observed_at"`
	Head               int64         `json:"head"`
	Tenant             uuid.UUID     `json:"tenant_id"`
	Project            uuid.UUID     `json:"project_id"`
	Collection         uuid.UUID     `json:"collection_id"`
	Style              uuid.UUID     `json:"style_version_id"`
	StyleSHA           string        `json:"style_config_sha256"`
	Run                uuid.UUID     `json:"run_id"`
	ConfigSHA          string        `json:"run_config_sha256"`
	Phase              string        `json:"phase"`
	Graph              int           `json:"graph_version"`
	CPUReceiptSHA      string        `json:"cpu_ready_receipt_sha256"`
	HumanReviewSHA     string        `json:"human_approval_sha256"`
	APIReceiptSHA      string        `json:"apply_api_receipt_sha256"`
	SourceManifestSHA  string        `json:"source_manifest_sha256"`
	LocatorInstallSHA  string        `json:"apply_locator_2654_install_receipt_sha256"`
	HeadInstallSHA     string        `json:"head2653_install_receipt_sha256"`
	GatewayReadySHA    string        `json:"intrinsic_gateway_ready_receipt_sha256"`
	PreviewDeliverySHA string        `json:"user3_previews_delivery_receipt_sha256"`
}

func (s scope) validate(now time.Time) error {
	if s.Schema != 2 || s.Main2655InstallSHA != main2655ReceiptSHA || s.Head != 2655 || s.Project != project || s.Collection != collection || s.ObservedAt.IsZero() || now.Before(s.ObservedAt) || now.Sub(s.ObservedAt) > 120*time.Second {
		return errors.New("scope identity/head/freshness refused")
	}
	for _, id := range []uuid.UUID{s.Tenant, s.Style, s.Run, s.Output} {
		if id == uuid.Nil {
			return errors.New("scope UUID missing")
		}
	}
	for _, h := range []string{s.ReadbackSHA, s.StyleSHA, s.ConfigSHA, s.CPUReceiptSHA, s.HumanReviewSHA, s.APIReceiptSHA, s.SourceManifestSHA, s.HeadInstallSHA, s.LocatorInstallSHA, s.GatewayReadySHA, s.PreviewDeliverySHA} {
		if !digestPattern.MatchString(h) {
			return errors.New("scope provenance missing")
		}
	}
	if s.Graph != 4 {
		return errors.New("scope graph refused")
	}
	switch s.Phase {
	case "resolve", "color", "local", "quality", "adopt":
	default:
		return errors.New("scope phase refused")
	}
	return validateThree(s)
}

// Review consistency is a local gate, not a substitute for independent human
// review. The operator supplies this artifact; this program never issues it.
type approval struct {
	Component         string `json:"component"`
	Approved          bool   `json:"approved_for_bounded_apply_component"`
	ScopeSHA          string `json:"scope_sha256"`
	SourceManifestSHA string `json:"source_manifest_sha256"`
	Reviewer          string `json:"reviewer"`
	BinarySHA         string `json:"binary_sha256"`
}

func privateJSON(path, expected string, out any) error {
	if !digestPattern.MatchString(expected) {
		return errors.New("private reference digest")
	}
	f, e := os.OpenFile(path, os.O_RDONLY, 0)
	if e != nil {
		return errors.New("private reference unavailable")
	}
	defer f.Close()
	st, e := f.Stat()
	ls, le := os.Lstat(path)
	if e != nil || le != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || ls.Mode()&os.ModeSymlink != 0 || !os.SameFile(st, ls) || st.Size() < 2 || st.Size() > 1<<20 {
		return errors.New("private reference identity/mode")
	}
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return e
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != expected {
		return errors.New("private reference CAS")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(out); e != nil {
		return errors.New("private reference schema")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("private reference trailing JSON")
	}
	return nil
}
func checkedClaim(ctx context.Context, begin func(context.Context) (pgx.Tx, error), s scope) (lease *workflow.ApplyLease, err error) {
	tx, e := begin(ctx)
	if e != nil {
		return nil, e
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		re := tx.Rollback(cleanup)
		if re != nil && !errors.Is(re, pgx.ErrTxClosed) {
			err = errors.Join(err, errors.New("claim rollback failed"))
			lease = nil
		}
	}()
	deferBefore, e := s.deferRows(ctx, tx)
	if e != nil {
		return nil, e
	}
	before, e := capture(ctx, tx, s)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); e != nil {
		return nil, e
	}
	commit := func() error {
		if _, e := tx.Exec(ctx, "RESET ROLE"); e != nil {
			return e
		}
		deferAfter, e := s.deferRows(ctx, tx)
		if e != nil {
			return e
		}
		if e = validateDeferrals(deferBefore, deferAfter, map[string]bool{}, time.Now().UTC(), time.Now().UTC()); e != nil {
			return e
		}
		after, e := capture(ctx, tx, s)
		if e != nil {
			return e
		}
		if !equalSnapshots(before, after) {
			return errors.New("outside state changed; claim rolled back")
		}
		return tx.Commit(ctx)
	}
	store := workflow.ApplyStore{DB: tx}
	l, e := store.Claim(ctx)
	if e != nil || l == nil {
		return nil, e
	}
	if l.TenantID != s.Tenant || l.RunID != s.Run || l.SourceCollectionID != s.Collection || l.StyleVersionID == nil || *l.StyleVersionID != s.Style || l.ItemCount != 3 || l.GraphVersion != s.Graph || l.Phase != s.Phase {
		return nil, errors.New("claim outside exact approved scope")
	}
	if l.GraphVersion == 4 {
		if len(l.FrozenPlan) != 4 {
			return nil, errors.New("unexpected fixed-style stage count")
		}
		for i, p := range []string{"resolve", "color", "local", "quality"} {
			expectedOps := []string{"style.resolve", "image.color", "image.local", "quality.check"}
			expectedRoles := []string{"resolved_style", "working_image", "working_image", "quality_report"}
			stage := l.FrozenPlan[i]
			if stage.Phase != p || stage.Operation != expectedOps[i] || stage.OutputRole != expectedRoles[i] || stage.Cardinality != "item" {
				return nil, errors.New("unexpected fixed-style stages")
			}
		}
	}
	// Legacy apply_inputs excludes adoption. Exact lease identity and fresh
	// outer immutable snapshot proof remain; ProcessOne checks real quality.
	if l.Phase == "adopt" && l.GraphVersion == 3 {
		if e = commit(); e != nil {
			return nil, e
		}
		return l, nil
	}
	if s.terminal {
		if _, e = tx.Exec(ctx, "RESET ROLE"); e != nil {
			return nil, e
		}
		if e = oldFailureReady(ctx, tx, s); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); e != nil {
			return nil, e
		}
		if e = commit(); e != nil {
			return nil, e
		}
		return l, nil
	}
	inputs, e := store.Inputs(ctx, *l)
	if e != nil {
		return nil, e
	}
	if len(inputs) != 3 {
		return nil, errors.New("exact three inputs required")
	}
	seen := map[uuid.UUID]bool{}
	for _, item := range inputs {
		var expected *itemBinding
		for i := range s.Items {
			if s.Items[i].Item == item.ItemID {
				expected = &s.Items[i]
			}
		}
		if expected == nil || seen[item.ItemID] || item.AssetID != expected.Asset || item.VersionID != expected.Version || item.Image.ArtifactID != expected.Master || item.Image.SHA256 != expected.MasterSHA || item.Style == nil || item.Style.SHA256 != s.StyleSHA {
			return nil, errors.New("actual claimed source/style mismatch")
		}
		seen[item.ItemID] = true
		raw, e := canonical.Bytes(item.Config)
		if e != nil {
			return nil, e
		}
		h := sha256.Sum256(raw)
		if hex.EncodeToString(h[:]) != s.ConfigSHA {
			return nil, errors.New("actual run config mismatch")
		}
	}
	if e = commit(); e != nil {
		return nil, e
	}
	return l, nil
}

type scopedStore struct {
	workflow.AuthoredBaseApplyStore
	pool      *pgxpool.Pool
	claimPool *pgxpool.Pool
	scope     scope
}

func (s scopedStore) Claim(ctx context.Context) (*workflow.ApplyLease, error) {
	return checkedClaim(ctx, s.claimPool.Begin, s.scope)
}
func execute() error {
	if e := checkedMain2655(); e != nil {
		return e
	}
	var s scope
	scopeSHA := os.Getenv("APPLY_SCOPE_SHA256")
	if e := privateJSON(os.Getenv("APPLY_SCOPE_FILE"), scopeSHA, &s); e != nil {
		return e
	}
	if e := s.validate(time.Now().UTC()); e != nil {
		return e
	}
	component := os.Getenv("APPLY_COMPONENT")
	if component == "" {
		component = "coordinator"
	}
	if component != "coordinator" && component != "dispatch" && component != "recover" && component != "terminalize" {
		return errors.New("closed component")
	}
	if e := s.loadDeferrals(); e != nil {
		return e
	}
	var a approval
	if e := privateJSON(os.Getenv("APPLY_REVIEW_FILE"), os.Getenv("APPLY_REVIEW_SHA256"), &a); e != nil {
		return e
	}
	if a.Component != component || !a.Approved || a.ScopeSHA != scopeSHA || a.SourceManifestSHA != s.SourceManifestSHA || a.Reviewer == "" || os.Getenv("CONFIRM_EXACT_APPLY_RUN") != s.Run.String() {
		return errors.New("independent exact tick review required")
	}
	exe, e := os.Executable()
	if e != nil {
		return errors.New("executable identity")
	}
	file, e := os.Open(exe)
	if e != nil {
		return errors.New("executable identity")
	}
	hasher := sha256.New()
	_, e = io.Copy(hasher, file)
	ce := file.Close()
	if e != nil || ce != nil || hex.EncodeToString(hasher.Sum(nil)) != a.BinarySHA {
		return errors.New("reviewed executable CAS")
	}
	ctx, c := context.WithTimeout(context.Background(), 90*time.Second)
	defer c()
	pool, e := restrictedPool(ctx)
	if e != nil {
		return e
	}
	defer pool.Close()
	claims, e := operatorPool(ctx)
	if e != nil {
		return e
	}
	defer claims.Close()
	if component == "terminalize" {
		s.terminal = true
		if s.Run != oldApplyRun || s.Phase != "quality" || len(s.Tasks) != 9 {
			return errors.New("exact old terminal scope required")
		}
		store := scopedStore{AuthoredBaseApplyStore: workflow.AuthoredBaseApplyStore{ApplyStore: workflow.ApplyStore{DB: pool}}, pool: pool, claimPool: claims, scope: s}
		worked, e := (workflow.ApplyCoordinator{Store: store}).ProcessOne(ctx)
		if e != nil {
			return e
		}
		if !worked {
			return errors.New("old terminal coordinator did not work")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "NORMAL_OLD_APPLY_TERMINALIZED", "run_id": s.Run, "worked": worked, "no_task_replay": true})
	}
	if component != "coordinator" {
		n, e := componentOne(ctx, claims, s, component)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "BOUNDED_NORMAL_COMPONENT_RETURNED", "component": component, "count": n, "six_public_deferral_readback": s.proof, "run_id": s.Run, "full_processing_accepted": false})
	}
	config := objectstore.ConfigFromEnv(true)
	objects, e := objectstore.New(config)
	if e != nil {
		return errors.New("objectstore configuration")
	}
	base := workflow.ApplyStore{DB: pool}
	store := scopedStore{AuthoredBaseApplyStore: workflow.AuthoredBaseApplyStore{ApplyStore: base, Objects: objects, Bucket: config.Bucket}, pool: pool, claimPool: claims, scope: s}
	coordinator := workflow.ApplyCoordinator{Store: store, Publisher: artifactjson.Publisher{Repository: workflow.ApplyDocuments{DB: pool, Bucket: config.Bucket}, Objects: objects}, Objects: objects, Bucket: config.Bucket}
	worked, e := coordinator.ProcessOne(ctx)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "BOUNDED_PRODUCTION_COORDINATOR_TICK_RETURNED", "run_id": s.Run, "phase": s.Phase, "worked": worked, "dispatched": false, "full_processing_accepted": false})
}
func main() {
	e := error(nil)
	if os.Getenv("APPLY_READBACK_ONLY") == "true" {
		e = readbackOnly()
	} else if os.Getenv("APPLY_RESTRICTED_ROLE_PREFLIGHT_ONLY") == "true" {
		e = restrictedPreflight()
	} else {
		e = execute()
	}
	if e != nil {
		out := map[string]string{"status": "COORDINATOR_TICK_REFUSED", "error_class": fmt.Sprintf("%T", e)}
		var pg *pgconn.PgError
		if errors.As(e, &pg) && regexp.MustCompile(`^[A-Z0-9]{5}$`).MatchString(pg.Code) {
			out["sqlstate"] = pg.Code
		}
		_ = json.NewEncoder(os.Stderr).Encode(out)
		os.Exit(1)
	}
}

// Restricted role preflight performs reads only and needs no style decision.
// It does not invoke Claim, ProcessOne, storage writes, dispatcher or recovery.
func restrictedPool(ctx context.Context) (*pgxpool.Pool, error) {
	cfg, e := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if e != nil {
		return nil, errors.New("database configuration")
	}
	cfg.MaxConns = 2
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	database.GuardApplicationRole(cfg)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, errors.New("database connection")
	}
	if e = database.RequireApplicationRole(ctx, pool, "photo_scheduler"); e != nil {
		pool.Close()
		return nil, e
	}
	return pool, nil
}
func restrictedPreflight() error {
	ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
	defer c()
	pool, e := restrictedPool(ctx)
	if e != nil {
		return e
	}
	defer pool.Close()
	tx, e := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var ok bool
	e = tx.QueryRow(ctx, `SELECT count(*)=8 AND bool_and(has_function_privilege(current_user,f,'EXECUTE')) FROM unnest(ARRAY[
 'photo.cancel_unsupported_apply_plans()'::regprocedure,'photo.claim_apply_build_versioned()'::regprocedure,
 'photo.renew_apply_build(uuid,uuid)'::regprocedure,'photo.materialize_apply_task(uuid,uuid,uuid,jsonb)'::regprocedure,
 'photo.register_apply_node(uuid,uuid,uuid,jsonb)'::regprocedure,'photo.adopt_apply_results(uuid,uuid,uuid)'::regprocedure,
 'photo.authored_render_base_adoption_context(uuid,uuid,uuid)'::regprocedure,'photo.fail_apply_terminal_stage(uuid,uuid,uuid)'::regprocedure]) f`).Scan(&ok)
	if e != nil || !ok {
		return errors.New("restricted coordinator interfaces unavailable")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "RESTRICTED_COORDINATOR_ROLE_READ_ONLY_PREFLIGHT_PASS", "claimed": false, "dispatched": false, "style_approved": false})
}
