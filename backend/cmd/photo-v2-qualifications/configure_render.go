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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2render"
)

type configureRenderArgs struct {
	tenant, project, qualification, report uuid.UUID
	sha, descriptor, descriptorSHA         string
	enabled                                bool
}
type renderInstallationDescriptor struct {
	SchemaVersion int                             `json:"schema_version"`
	Tenant        uuid.UUID                       `json:"tenant_id"`
	Project       uuid.UUID                       `json:"project_id"`
	Operation     string                          `json:"operation"`
	Pin           v2render.RenderQualificationPin `json:"qualification_pin"`
	ICC           string                          `json:"icc_lock_sha"`
}

func parseConfigureRenderArgs(args []string) (configureRenderArgs, error) {
	var out configureRenderArgs
	if len(args) == 0 || args[0] != "configure-render" {
		return out, errors.New("configure-render command required")
	}
	flags := flag.NewFlagSet("configure-render", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var tenant, project, qualification, report, enabled string
	flags.StringVar(&tenant, "tenant", "", "tenant UUID")
	flags.StringVar(&project, "project", "", "project UUID")
	flags.StringVar(&qualification, "qualification", "", "imported qualification UUID")
	flags.StringVar(&report, "report-artifact", "", "ready imported report artifact UUID")
	flags.StringVar(&out.sha, "sha256", "", "report SHA256")
	flags.StringVar(&out.descriptor, "descriptor", "", "canonical independent render installation descriptor")
	flags.StringVar(&out.descriptorSHA, "descriptor-sha256", "", "independently pinned descriptor SHA256")
	flags.StringVar(&enabled, "enabled", "", "required explicit true or false")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || !filepath.IsAbs(out.descriptor) || (enabled != "true" && enabled != "false") {
		return configureRenderArgs{}, errors.New("invalid configure-render arguments")
	}
	var err error
	for _, entry := range []struct {
		raw    string
		target *uuid.UUID
	}{{tenant, &out.tenant}, {project, &out.project}, {qualification, &out.qualification}, {report, &out.report}} {
		if *entry.target, err = uuid.Parse(entry.raw); err != nil || *entry.target == uuid.Nil {
			return configureRenderArgs{}, errors.New("invalid configure-render identity")
		}
	}
	for _, pin := range []string{out.sha, out.descriptorSHA} {
		if _, err := domain.ParseDigest(pin); err != nil {
			return configureRenderArgs{}, errors.New("invalid configure-render digest")
		}
	}
	out.enabled = enabled == "true"
	return out, nil
}
func readRenderInstallationDescriptor(args configureRenderArgs) (renderInstallationDescriptor, error) {
	var descriptor renderInstallationDescriptor
	fd, err := syscall.Open(args.descriptor, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return descriptor, errors.New("render descriptor unavailable")
	}
	file := os.NewFile(uintptr(fd), args.descriptor)
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 2 || before.Size() > 65536 {
		return descriptor, errors.New("render descriptor bounds invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || int64(len(raw)) != before.Size() {
		return descriptor, errors.New("render descriptor read failed")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return descriptor, errors.New("render descriptor changed")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != args.descriptorSHA {
		return descriptor, errors.New("render descriptor hash mismatch")
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, normalized) {
		return descriptor, errors.New("render descriptor not canonical")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&descriptor) != nil || descriptor.SchemaVersion != 1 || descriptor.Tenant != args.tenant || descriptor.Project != args.project {
		return descriptor, errors.New("render descriptor scope invalid")
	}
	if descriptor.Operation != "render.compose" && descriptor.Operation != "editor.layers_precompose" {
		return descriptor, errors.New("render descriptor operation invalid")
	}
	if descriptor.Pin.Backend != "cpu" && descriptor.Pin.Backend != "cuda" && descriptor.Pin.Backend != "mps" || descriptor.Operation == "editor.layers_precompose" && descriptor.Pin.Backend != "cpu" {
		return descriptor, errors.New("render descriptor backend invalid")
	}
	for _, pin := range []string{descriptor.Pin.ModelSHA, descriptor.Pin.RuntimeSHA, descriptor.Pin.DeviceSHA, descriptor.Pin.DatasetSHA, descriptor.Pin.CalibrationSHA, descriptor.ICC} {
		if _, err := domain.ParseDigest(pin); err != nil {
			return descriptor, errors.New("render descriptor pin invalid")
		}
	}
	return descriptor, nil
}

type renderInstallationDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func configureRenderInstallation(ctx context.Context, db renderInstallationDatabase, args configureRenderArgs, descriptor renderInstallationDescriptor) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(descriptor.Pin)
	if err != nil {
		return err
	}
	pin, err := canonical.Bytes(raw)
	if err != nil {
		return err
	}
	var accepted bool
	err = db.QueryRow(ctx, `SELECT photo.configure_native_render_installation($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9)`, args.tenant, args.project, descriptor.Operation, string(pin), descriptor.ICC, args.qualification, args.report, args.sha, args.enabled).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return errors.New("native render installation refused")
	}
	return nil
}
func runConfigureRender(ctx context.Context, args []string, output io.Writer) error {
	selected, err := parseConfigureRenderArgs(args)
	if err != nil {
		return err
	}
	descriptor, err := readRenderInstallationDescriptor(selected)
	if err != nil {
		return err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL required")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	database.GuardApplicationRole(config)
	limit, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	db, err := pgxpool.NewWithConfig(limit, config)
	if err != nil {
		return errors.New("database unavailable")
	}
	defer db.Close()
	if err = database.RequireApplicationRole(limit, db, "photo_maintenance"); err != nil {
		return err
	}
	if err = configureRenderInstallation(limit, db, selected, descriptor); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(struct {
		Tenant        uuid.UUID `json:"tenant_id"`
		Project       uuid.UUID `json:"project_id"`
		Operation     string    `json:"operation"`
		Enabled       bool      `json:"enabled"`
		Qualification uuid.UUID `json:"qualification_id"`
		Report        uuid.UUID `json:"report_artifact_id"`
		ReportSHA     string    `json:"report_sha256"`
		DescriptorSHA string    `json:"descriptor_sha256"`
	}{selected.tenant, selected.project, descriptor.Operation, selected.enabled, selected.qualification, selected.report, selected.sha, selected.descriptorSHA})
}
