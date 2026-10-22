// photo-render-measure is an offline operator-only lower-compute tool. It has
// no SQL DSN, qualification issuer, admission bypass or deployment installer.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/workflow"
	"strings"
	"syscall"
	"time"
)

func readPinned(root, path, pin string, dst any) error {
	if len(pin) != 64 || strings.ToLower(pin) != pin {
		return errors.New("lowercase source pin required")
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	relative, e := filepath.Rel(filepath.Join(root, "work"), resolved)
	if e != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("input outside repository work")
	}
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 2 || info.Size() > 32<<20 {
		return errors.New("private bounded input required")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin {
		return errors.New("source pin changed")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(dst); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing source data")
	}
	return nil
}
func run() error {
	repo := flag.String("repository", "", "repository root")
	specPath := flag.String("case", "", "private approved case")
	specSHA := flag.String("case-sha256", "", "approved case SHA")
	runtimePath := flag.String("runtime", "", "private runtime descriptor")
	runtimeSHA := flag.String("runtime-sha256", "", "runtime descriptor SHA")
	output := flag.String("output", "", "exclusive private output")
	seconds := flag.Int("seconds", 0, "whole-case deadline including human approvals")
	flag.Parse()
	if flag.NArg() != 0 || *seconds < 1 || *seconds > 10800 {
		return errors.New("bounded measurement arguments required")
	}
	root, e := filepath.EvalSymlinks(*repo)
	if e != nil {
		return e
	}
	var spec workflow.OperatorCaseSpec
	var runtime workflow.OperatorCaseRuntime
	if e = readPinned(root, *specPath, *specSHA, &spec); e != nil {
		return e
	}
	if e = readPinned(root, *runtimePath, *runtimeSHA, &runtime); e != nil {
		return e
	}
	if runtime.Repository != root {
		return errors.New("runtime repository differs")
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(*output))
	if e != nil {
		return e
	}
	owned, e := filepath.EvalSymlinks(runtime.Directory)
	if e != nil || parent != owned {
		return errors.New("output not case-owned")
	}
	info, e := os.Stat(parent)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private output directory required")
	}
	parentContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(parentContext, time.Duration(*seconds)*time.Second)
	defer cancel()
	measured, e := workflow.RunOperatorRendererCase(ctx, spec, runtime)
	if e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	raw, e := canonical.Marshal(measured)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "renderer measurement refused:", e)
		os.Exit(1)
	}
}
