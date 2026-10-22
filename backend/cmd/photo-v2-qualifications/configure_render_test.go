package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2render"
)

func configurationFixture(t *testing.T) (configureRenderArgs, renderInstallationDescriptor, []string) {
	t.Helper()
	args := configureRenderArgs{tenant: uuid.New(), project: uuid.New(), qualification: uuid.New(), report: uuid.New(), sha: strings.Repeat("f", 64), descriptor: filepath.Join(t.TempDir(), "descriptor.json"), enabled: false}
	descriptor := renderInstallationDescriptor{SchemaVersion: 1, Tenant: args.tenant, Project: args.project, Operation: "render.compose", Pin: v2render.RenderQualificationPin{RenderQualification: v2render.RenderQualification{ModelSHA: strings.Repeat("a", 64), RuntimeSHA: strings.Repeat("b", 64), Backend: "cpu", DeviceSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64)}, CalibrationSHA: strings.Repeat("e", 64)}, ICC: strings.Repeat("0", 64)}
	writeConfiguration(t, &args, descriptor)
	command := []string{"configure-render", "--tenant", args.tenant.String(), "--project", args.project.String(), "--qualification", args.qualification.String(), "--report-artifact", args.report.String(), "--sha256", args.sha, "--descriptor", args.descriptor, "--descriptor-sha256", args.descriptorSHA, "--enabled", "false"}
	return args, descriptor, command
}
func writeConfiguration(t *testing.T, args *configureRenderArgs, descriptor any) {
	t.Helper()
	raw, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = canonical.Bytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	args.descriptorSHA = hex.EncodeToString(sum[:])
	if err = os.WriteFile(args.descriptor, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestConfigureRenderRequiresExplicitEnableAndClosedArguments(t *testing.T) {
	_, _, command := configurationFixture(t)
	for _, enabled := range []string{"false", "true"} {
		args := append([]string{}, command...)
		args[len(args)-1] = enabled
		parsed, err := parseConfigureRenderArgs(args)
		if err != nil || parsed.enabled != (enabled == "true") {
			t.Fatal("explicit enable changed", err)
		}
	}
	if _, err := parseConfigureRenderArgs(command[:len(command)-2]); err == nil {
		t.Fatal("activation default accepted")
	}
	for flag, value := range map[string]string{"--tenant": uuid.Nil.String(), "--project": "invalid", "--qualification": "", "--report-artifact": uuid.Nil.String(), "--sha256": "bad", "--descriptor": "relative", "--descriptor-sha256": strings.Repeat("A", 64), "--enabled": "1"} {
		args := append([]string{}, command...)
		for i := range args {
			if args[i] == flag {
				args[i+1] = value
				break
			}
		}
		if _, err := parseConfigureRenderArgs(args); err == nil {
			t.Fatal("invalid flag accepted", flag)
		}
	}
	for _, extra := range [][]string{{"--skip-qualification"}, {"--passed"}, {"--backend", "cuda"}, {"trailing"}} {
		if _, err := parseConfigureRenderArgs(append(append([]string{}, command...), extra...)); err == nil {
			t.Fatal("override accepted", extra)
		}
	}
}
func TestConfigureRenderDescriptorPinsScopeAndCanonicalBytes(t *testing.T) {
	args, descriptor, _ := configurationFixture(t)
	observed, err := readRenderInstallationDescriptor(args)
	if err != nil || !reflect.DeepEqual(observed, descriptor) {
		t.Fatal("descriptor changed", err)
	}
	for _, damage := range []string{"foreigntenant", "foreignproject", "operation", "pin", "icc", "backend", "precomposebackend", "extra", "nestedextra"} {
		t.Run(damage, func(t *testing.T) {
			mutated := descriptor
			switch damage {
			case "foreigntenant":
				mutated.Tenant = uuid.New()
			case "foreignproject":
				mutated.Project = uuid.New()
			case "operation":
				mutated.Operation = "unknown"
			case "pin":
				mutated.Pin.RuntimeSHA = "malformed"
			case "icc":
				mutated.ICC = ""
			case "backend":
				mutated.Pin.Backend = "auto"
			case "precomposebackend":
				mutated.Operation = "editor.layers_precompose"
				mutated.Pin.Backend = "cuda"
			}
			if damage == "extra" || damage == "nestedextra" {
				raw, _ := json.Marshal(mutated)
				var object map[string]any
				if err = json.Unmarshal(raw, &object); err != nil {
					t.Fatal(err)
				}
				if damage == "extra" {
					object["enabled"] = true
				} else {
					object["qualification_pin"].(map[string]any)["passed"] = true
				}
				writeConfiguration(t, &args, object)
			} else {
				writeConfiguration(t, &args, mutated)
			}
			if _, err := readRenderInstallationDescriptor(args); err == nil {
				t.Fatal("descriptor damage accepted", damage)
			}
		})
	}
	writeConfiguration(t, &args, descriptor)
	raw, err := os.ReadFile(args.descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(args.descriptor, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append(raw, ' '))
	args.descriptorSHA = hex.EncodeToString(sum[:])
	if _, err := readRenderInstallationDescriptor(args); err == nil {
		t.Fatal("noncanonical file accepted")
	}
}
func TestConfigureRenderDescriptorRejectsSymlinkAndBounds(t *testing.T) {
	args, _, _ := configurationFixture(t)
	original := args.descriptor
	args.descriptor = filepath.Join(filepath.Dir(original), "link.json")
	if err := os.Symlink(original, args.descriptor); err != nil {
		t.Fatal(err)
	}
	if _, err := readRenderInstallationDescriptor(args); err == nil {
		t.Fatal("symlink accepted")
	}
	args.descriptor = original
	for _, body := range [][]byte{{}, []byte("x"), bytes.Repeat([]byte("x"), 65537)} {
		if err := os.WriteFile(original, body, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readRenderInstallationDescriptor(args); err == nil {
			t.Fatal("file bounds accepted")
		}
	}
}

type configurationRow struct {
	accepted bool
	err      error
}

func (row configurationRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*bool) = row.accepted
	return nil
}

type configurationDB struct {
	row   configurationRow
	query string
	args  []any
	calls int
}

func (db *configurationDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.query = query
	db.args = args
	db.calls++
	return db.row
}
func TestConfigureRenderBindsExistingBoundaryAndRefusesFalseNull(t *testing.T) {
	args, descriptor, _ := configurationFixture(t)
	for _, enabled := range []bool{false, true} {
		args.enabled = enabled
		db := &configurationDB{row: configurationRow{accepted: true}}
		if err := configureRenderInstallation(context.Background(), db, args, descriptor); err != nil {
			t.Fatal(err)
		}
		if db.calls != 1 || db.query != "SELECT photo.configure_native_render_installation($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9)" {
			t.Fatal("wrong authority", db.query)
		}
		if !reflect.DeepEqual(db.args[:3], []any{args.tenant, args.project, "render.compose"}) || !reflect.DeepEqual(db.args[4:], []any{descriptor.ICC, args.qualification, args.report, args.sha, enabled}) {
			t.Fatal("scope changed", db.args)
		}
		raw, _ := json.Marshal(descriptor.Pin)
		pin, _ := canonical.Bytes(raw)
		if db.args[3] != string(pin) {
			t.Fatal("pin changed")
		}
	}
	for _, row := range []configurationRow{{accepted: false}, {err: errors.New("cannot scan NULL into bool")}} {
		if err := configureRenderInstallation(context.Background(), &configurationDB{row: row}, args, descriptor); err == nil {
			t.Fatal("refusal accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := &configurationDB{}
	if err := configureRenderInstallation(ctx, db, args, descriptor); err == nil || db.calls != 0 {
		t.Fatal("cancelled operation reached database")
	}
}
func TestConfigureRenderValidatesDescriptorBeforeDatabase(t *testing.T) {
	_, _, command := configurationFixture(t)
	t.Setenv("DATABASE_URL", "")
	if err := run(context.Background(), command, &bytes.Buffer{}); err == nil || err.Error() != "DATABASE_URL required" {
		t.Fatal("valid descriptor did not reach guarded DB boundary", err)
	}
	command[len(command)-3] = strings.Repeat("9", 64)
	if err := run(context.Background(), command, &bytes.Buffer{}); err == nil || err.Error() == "DATABASE_URL required" {
		t.Fatal("bad descriptor reached database", err)
	}
}
