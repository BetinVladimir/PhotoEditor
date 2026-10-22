package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"
)

// OperatorProcessCompute calls actual lower production kernels in a bounded
// process. A successful exit alone is never a measured output proof.
type OperatorProcessCompute struct {
	Repository, Python, Script, Directory, Scratch string
	Installation, Policy                           json.RawMessage
	InventorySHA, PythonSHA, ScriptSHA             string
	Objects                                        *OperatorPhysicalObjects
	Ledger                                         *OperatorApplyLedger
	InputContracts                                 map[uuid.UUID]string
	sequence                                       int
}
type operatorObjectDescriptor struct {
	ID       uuid.UUID  `json:"artifact_id"`
	SHA      string     `json:"sha256"`
	Bytes    int64      `json:"bytes"`
	Media    string     `json:"media_type"`
	Role     string     `json:"role"`
	Producer *uuid.UUID `json:"producer"`
	Contract *string    `json:"contract"`
}
type operatorBoundedLog struct {
	file      *os.File
	remaining int64
}

func (w *operatorBoundedLog) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("operator child log limit")
	}
	n, e := w.file.Write(p)
	w.remaining -= int64(n)
	return n, e
}
func operatorPrivateDirectory(repository, path string) error {
	root, e := filepath.EvalSymlinks(repository)
	if e != nil {
		return e
	}
	dir, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(filepath.Join(root, "work"), dir)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("operator directory outside repository work")
	}
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("operator private directory required")
	}
	return nil
}
func writeOperatorExclusive(path string, raw []byte) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
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
func (p *OperatorProcessCompute) descriptors(task PreparedTask) ([]operatorObjectDescriptor, error) {
	// Send the entire retained ledger so the physical byte budget remains
	// cumulative across stages; Artifact port still only opens declared refs.
	ids := make([]uuid.UUID, 0, len(p.Ledger.objects))
	for id := range p.Ledger.objects {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	refs := append([]review.ArtifactRef(nil), task.Inputs...)
	if task.Params.ID != uuid.Nil {
		refs = append(refs, task.Params)
	}
	for _, ref := range refs {
		a, ok := p.Ledger.objects[ref.ID]
		if !ok || a.SHA256 != ref.SHA256 {
			return nil, errors.New("operator task input absent from retained ledger")
		}
	}
	seen := map[uuid.UUID]bool{}
	out := []operatorObjectDescriptor{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		a, ok := p.Ledger.objects[id]
		if !ok || a.SHA256 == "" {
			return nil, errors.New("operator process input absent from measured ledger")
		}
		d := operatorObjectDescriptor{ID: a.ArtifactID, SHA: a.SHA256, Bytes: a.Bytes, Media: a.MediaType, Role: "input"}
		if contract := p.InputContracts[id]; contract != "" {
			c := contract
			d.Contract = &c
		}
		for producer, proofs := range p.Ledger.outputs {
			for _, proof := range proofs {
				if proof.Artifact.ArtifactID == id {
					owner := producer
					d.Producer = &owner
					d.Role = proof.Role
					if proof.Contract != "" {
						contract := proof.Contract
						d.Contract = &contract
					}
				}
			}
		}
		out = append(out, d)
	}
	return out, nil
}
func (p *OperatorProcessCompute) Execute(ctx context.Context, task PreparedTask) (result OperatorMeasuredTask, err error) {
	if _, ok := ctx.Deadline(); !ok {
		return result, errors.New("operator process deadline required")
	}
	if p.Objects == nil || p.Ledger == nil || len(p.InventorySHA) != 64 {
		return result, errors.New("operator process configuration invalid")
	}
	for _, dir := range []string{p.Directory, p.Scratch, p.Objects.root} {
		if e := operatorPrivateDirectory(p.Repository, dir); e != nil {
			return result, e
		}
	}
	// Pin actual interpreter and script bytes. A repo virtualenv may point to
	// an approved external interpreter; its resolved bytes remain mandatory.
	for _, pin := range []struct {
		path, digest string
		script       bool
	}{{p.Python, p.PythonSHA, false}, {p.Script, p.ScriptSHA, true}} {
		path, absErr := filepath.Abs(pin.path)
		if absErr != nil {
			return result, absErr
		}
		relative, relErr := filepath.Rel(p.Repository, path)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return result, errors.New("operator executable entry outside repository")
		}
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			return result, resolveErr
		}
		if pin.script {
			relative, relErr = filepath.Rel(p.Repository, resolved)
			if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				return result, errors.New("operator script escaped repository")
			}
		}
		file, openErr := os.Open(resolved)
		if openErr != nil {
			return result, openErr
		}
		digest := sha256.New()
		_, readErr := io.Copy(digest, file)
		closeErr := file.Close()
		if readErr != nil {
			return result, readErr
		}
		if closeErr != nil {
			return result, closeErr
		}
		if len(pin.digest) != 64 || hex.EncodeToString(digest.Sum(nil)) != pin.digest {
			return result, errors.New("operator executable bytes changed")
		}
	}
	if p.Ledger.lease.TenantID == uuid.Nil || p.Ledger.lease.RunID == uuid.Nil {
		return result, errors.New("operator measured ledger identity invalid")
	}
	bound, exists := p.Ledger.tasks[task.ID]
	if !exists || !reflect.DeepEqual(bound, task) {
		return result, errors.New("operator task is not the exact measured-ledger materialization")
	}
	descriptors, e := p.descriptors(task)
	if e != nil {
		return result, e
	}
	p.sequence++
	stem := fmt.Sprintf("stage-%06d-%s", p.sequence, task.ID)
	requestPath := filepath.Join(p.Directory, stem+".request.json")
	output := filepath.Join(p.Directory, stem+".observation.json")
	request := map[string]any{"repository": p.Repository, "objects_root": p.Objects.root, "scratch_root": p.Scratch, "output": output, "task": task, "objects": descriptors, "installation": p.Installation, "inventory_sha256": p.InventorySHA, "operator_policy": p.Policy, "bound_identity": operatorBoundIdentity{p.Ledger.lease.TenantID, p.Ledger.lease.RunID}}
	raw, e := canonical.Marshal(request)
	if e != nil {
		return result, e
	}
	if e = writeOperatorExclusive(requestPath, raw); e != nil {
		return result, e
	}
	log, e := os.OpenFile(filepath.Join(p.Directory, stem+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return result, e
	}
	defer func() {
		if closeErr := log.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	command := exec.CommandContext(ctx, p.Python, p.Script, "--request", requestPath)
	command.Dir = p.Repository
	command.Env = []string{"PATH=/usr/bin:/bin", "PYTHONDONTWRITEBYTECODE=1", "PYTHONNOUSERSITE=1", "PYTHONPATH=" + filepath.Join(p.Repository, "workers"), "HOME=" + p.Directory, "TMPDIR=" + p.Scratch, "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "OMP_NUM_THREADS=1", "OPENBLAS_NUM_THREADS=1", "MKL_NUM_THREADS=1", "PYTORCH_ENABLE_MPS_FALLBACK=0"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second
	command.Stdout = &operatorBoundedLog{log, 8 << 20}
	command.Stderr = command.Stdout
	if e = command.Start(); e != nil {
		return result, e
	}
	// Always clean the group, including leader-exited descendants. Deliberately
	// escaping descendants and process OOM remain external containment limits.
	defer func() {
		killErr := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if killErr != nil && !errors.Is(killErr, syscall.ESRCH) && err == nil {
			err = killErr
		}
	}()
	if e = command.Wait(); e != nil {
		return result, e
	}
	if e = ctx.Err(); e != nil {
		return result, e
	}
	info, e := os.Lstat(output)
	if e != nil {
		return result, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16<<20 {
		return result, errors.New("operator observation file invalid")
	}
	body, e := os.ReadFile(output)
	if e != nil {
		return result, e
	}
	var envelope struct {
		Identity    operatorBoundIdentity `json:"bound_identity"`
		Schema      int                   `json:"schema_version"`
		Observation json.RawMessage       `json:"observation"`
		SHA         string                `json:"observation_sha256"`
	}
	if e = decodeOperatorClosed(body, &envelope); e != nil {
		return result, e
	}
	if envelope.Schema != 1 || !operatorIdentityMatches(envelope.Identity, p.Ledger.lease) {
		return result, errors.New("operator observation envelope invalid")
	}
	var observed operatorObservation
	if e = decodeOperatorClosed(envelope.Observation, &observed); e != nil {
		return result, e
	}
	result = OperatorMeasuredTask{Task: task, ObservationRaw: envelope.Observation, ObservationSHA256: envelope.SHA}
	seenOutputs := map[uuid.UUID]bool{}
	for _, entry := range observed.Outputs {
		id, parseErr := uuid.Parse(entry.ArtifactID)
		if parseErr != nil || id == uuid.Nil || seenOutputs[id] || entry.Bytes < 1 || entry.Bytes > p.Objects.maxObjectBytes {
			return OperatorMeasuredTask{}, errors.New("operator observed object invalid")
		}
		seenOutputs[id] = true
		contract := ""
		if entry.Contract != nil {
			contract = *entry.Contract
		}
		a := styleArtifact{ArtifactID: id, SHA256: entry.SHA256, Bytes: entry.Bytes, MediaType: entry.MediaType, Bucket: "operator-measurement", Key: id.String(), VersionID: entry.SHA256}
		proof := OperatorMeasuredOutput{Artifact: a, Role: entry.Role, Contract: contract, Producer: task.ID, Verifier: p.Objects}
		if e = p.Objects.VerifyMeasuredObject(ctx, task, proof); e != nil {
			return OperatorMeasuredTask{}, e
		}
		result.Outputs = append(result.Outputs, proof)
		result.OutputBytes += entry.Bytes
	}
	if e = validateOperatorObservation(task, result); e != nil {
		return OperatorMeasuredTask{}, e
	}
	return result, nil
}
func decodeOperatorClosed(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(target); e != nil {
		return e
	}
	var extra any
	if e := decoder.Decode(&extra); e != io.EOF {
		return errors.New("operator trailing JSON")
	}
	return nil
}

var _ OperatorApplyCompute = (*OperatorProcessCompute)(nil)

type operatorBoundIdentity struct {
	Tenant uuid.UUID `json:"tenant_id"`
	Run    uuid.UUID `json:"run_id"`
}

func operatorIdentityMatches(got operatorBoundIdentity, lease ApplyLease) bool {
	return lease.TenantID != uuid.Nil && lease.RunID != uuid.Nil && got == (operatorBoundIdentity{lease.TenantID, lease.RunID})
}
