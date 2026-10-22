package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	migrations "photoeditor/backend/db"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type observeLineageArgs struct{ output, references, referencesSHA string }
type lineageReferenceManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Coverage      string `json:"coverage"`
	Snapshots     []struct {
		Identity       string `json:"identity"`
		SemanticSHA256 string `json:"semantic_sha256"`
	} `json:"snapshots"`
}

func parseObserveLineageArgs(args []string) (observeLineageArgs, error) {
	var out observeLineageArgs
	if len(args) == 0 || args[0] != "observe-lineage" {
		return out, errors.New("observe-lineage command required")
	}
	flags := flag.NewFlagSet("observe-lineage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&out.output, "output", "", "new exclusive canonical observation output file")
	flags.StringVar(&out.references, "references", "", "independent trusted reference manifest")
	flags.StringVar(&out.referencesSHA, "references-sha256", "", "independently pinned reference manifest SHA256")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || !filepath.IsAbs(out.output) {
		return observeLineageArgs{}, errors.New("invalid observe-lineage arguments")
	}
	seen := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	if seen["references"] != seen["references-sha256"] {
		return observeLineageArgs{}, errors.New("references and references-sha256 must be paired")
	}
	if seen["references"] {
		if !filepath.IsAbs(out.references) {
			return observeLineageArgs{}, errors.New("absolute independent references required")
		}
		if _, err := domain.ParseDigest(out.referencesSHA); err != nil {
			return observeLineageArgs{}, errors.New("invalid references digest")
		}
	}
	if _, err := os.Lstat(out.output); err == nil || !errors.Is(err, os.ErrNotExist) {
		return observeLineageArgs{}, errors.New("observation output must not exist")
	}
	info, err := os.Lstat(filepath.Dir(out.output))
	if err != nil || !info.IsDir() {
		return observeLineageArgs{}, errors.New("observation output parent must be an existing directory")
	}
	return out, nil
}
func readLineageReferences(path, pin string) ([]migrations.LineageSnapshotAuthority, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("reference manifest unavailable")
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 2 || before.Size() > 1<<20 {
		return nil, errors.New("reference manifest bounds invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || int64(len(raw)) != before.Size() {
		return nil, errors.New("reference manifest read failed")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, errors.New("reference manifest changed")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin {
		return nil, errors.New("reference manifest digest mismatch")
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, normalized) {
		return nil, errors.New("reference manifest not canonical")
	}
	var manifest lineageReferenceManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || manifest.SchemaVersion != 1 || manifest.Coverage != migrations.LineageObservationCoverage || len(manifest.Snapshots) < 1 || len(manifest.Snapshots) > 1024 {
		return nil, errors.New("reference manifest schema invalid")
	}
	authorities := make([]migrations.LineageSnapshotAuthority, 0, len(manifest.Snapshots))
	identities, hashes := map[string]bool{}, map[string]bool{}
	for _, entry := range manifest.Snapshots {
		if entry.Identity == "" || len(entry.Identity) > 200 || identities[entry.Identity] || hashes[entry.SemanticSHA256] {
			return nil, errors.New("reference manifest identity is missing or ambiguous")
		}
		if _, err := domain.ParseDigest(entry.SemanticSHA256); err != nil {
			return nil, errors.New("invalid reference snapshot digest")
		}
		identities[entry.Identity] = true
		hashes[entry.SemanticSHA256] = true
		authorities = append(authorities, migrations.LineageSnapshotAuthority{Identity: entry.Identity, SemanticSHA256: entry.SemanticSHA256})
	}
	return authorities, nil
}
func writeExclusiveLineageObservation(path string, observation migrations.LineageObservation) error {
	raw, err := canonical.Marshal(observation)
	if err != nil {
		return err
	}
	if len(raw) > migrations.DefaultLineageObservationLimits().MaxBytes {
		return errors.New("observation output bounds exceeded")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("observation output must not exist")
	}
	owned, statErr := file.Stat()
	complete := false
	defer func() {
		_ = file.Close()
		if !complete && statErr == nil {
			current, err := os.Lstat(path)
			if err == nil && os.SameFile(owned, current) {
				_ = os.Remove(path)
			}
		}
	}()
	if statErr != nil {
		return statErr
	}
	if err = file.Chmod(0600); err != nil {
		return err
	}
	if _, err = file.Write(raw); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	current, pathErr := os.Lstat(path)
	if pathErr != nil || !os.SameFile(owned, current) || current.Mode()&os.ModeSymlink != 0 {
		return errors.New("observation output replaced")
	}
	complete = true
	return nil
}
func finishLineageObservation(output string, value migrations.LineageObservation, references []migrations.LineageSnapshotAuthority, writer io.Writer) error {
	classification := "CAPTURE_ONLY_NOT_CLASSIFIED"
	if len(references) > 0 {
		var err error
		classification, err = migrations.ClassifyLineageObservation(value, references)
		if err != nil {
			return err
		}
	}
	if err := writeExclusiveLineageObservation(output, value); err != nil {
		return err
	}
	return json.NewEncoder(writer).Encode(struct {
		SemanticSHA            string `json:"semantic_sha256"`
		ExactLedgerSHA         string `json:"exact_ledger_sha256"`
		Objects                int    `json:"object_count"`
		LedgerSteps            int    `json:"ledger_steps"`
		Classification         string `json:"classification"`
		ExternalRequired       bool   `json:"external_dependency_validation_required"`
		InstallationAuthorized bool   `json:"installation_authorized"`
	}{value.SemanticSHA256, value.ExactLedgerSHA256, len(value.Objects), len(value.Ledger), classification, true, false})
}
func runObserveLineage(ctx context.Context, args []string, output io.Writer) error {
	selected, err := parseObserveLineageArgs(args)
	if err != nil {
		return err
	}
	var references []migrations.LineageSnapshotAuthority
	if selected.references != "" {
		references, err = readLineageReferences(selected.references, selected.referencesSHA)
		if err != nil {
			return err
		}
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	config.MaxConns = 1
	config.MinConns = 0
	config.ConnConfig.ConnectTimeout = 5 * time.Second
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "1000"
	bounded, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(bounded, config)
	if err != nil {
		return errors.New("database unavailable")
	}
	defer pool.Close()
	if err = pool.Ping(bounded); err != nil {
		return err
	}
	observation, err := migrations.ObserveLineage(bounded, pool, migrations.DefaultLineageObservationLimits())
	if err != nil {
		return err
	}
	if err = bounded.Err(); err != nil {
		return err
	}
	return finishLineageObservation(selected.output, observation, references, output)
}
