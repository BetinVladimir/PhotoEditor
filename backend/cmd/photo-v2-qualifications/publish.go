package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2qualification"
)

type publishArgs struct {
	importArgs
	file, key string
}

func parsePublishArgs(args []string) (publishArgs, error) {
	var out publishArgs
	if len(args) == 0 || args[0] != "publish" {
		return out, errors.New("usage: photo-v2-qualifications publish --tenant UUID --project UUID --operator UUID --report-file PATH --sha256 SHA --locks PATH --idempotency-key KEY")
	}
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var tenant, project, operator string
	flags.StringVar(&tenant, "tenant", "", "tenant UUID")
	flags.StringVar(&project, "project", "", "project UUID")
	flags.StringVar(&operator, "operator", "", "operator UUID")
	flags.StringVar(&out.file, "report-file", "", "original canonical report file")
	flags.StringVar(&out.sha, "sha256", "", "independently pinned report SHA256")
	flags.StringVar(&out.locks, "locks", "", "trusted deployment lock file")
	flags.StringVar(&out.key, "idempotency-key", "", "durable operator publication key")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || !filepath.IsAbs(out.file) || !filepath.IsAbs(out.locks) || len(out.key) < 1 || len(out.key) > 200 || strings.TrimSpace(out.key) != out.key || strings.IndexFunc(out.key, unicode.IsControl) >= 0 {
		return publishArgs{}, errors.New("invalid publish arguments")
	}
	if len(out.sha) != 64 {
		return publishArgs{}, errors.New("invalid report SHA256")
	}
	for _, c := range out.sha {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return publishArgs{}, errors.New("invalid report SHA256")
		}
	}
	var err error
	if out.tenant, err = uuid.Parse(tenant); err != nil || out.tenant == uuid.Nil {
		return publishArgs{}, errors.New("invalid tenant")
	}
	if out.project, err = uuid.Parse(project); err != nil || out.project == uuid.Nil {
		return publishArgs{}, errors.New("invalid project")
	}
	if out.operator, err = uuid.Parse(operator); err != nil || out.operator == uuid.Nil {
		return publishArgs{}, errors.New("invalid operator")
	}
	return out, nil
}

func readPublishReport(path, pin string, expected v2qualification.Expected) ([]byte, error) {
	// O_NONBLOCK prevents FIFOs/devices from blocking before the regular-file fence.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("report file unavailable")
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() < 2 || before.Size() > 8*1024*1024 {
		return nil, errors.New("report file bounds invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 8*1024*1024+1))
	if err != nil || int64(len(raw)) != before.Size() {
		return nil, errors.New("report file read failed")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, errors.New("report file changed")
	}
	// Strict canonical/hash/check validation uses independently loaded locks.
	if err := v2qualification.VerifyImportReport(raw, pin, expected); err != nil {
		return nil, err
	}
	return raw, nil
}
