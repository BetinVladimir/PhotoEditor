package v2input

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"photoeditor/backend/internal/idempotency"
)

var stagePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,119}$`)

// RunID binds one idempotency key to one run across process crashes. The
// request hash is checked separately by begin_v2_admission.
func RunID(tenant, actor uuid.UUID, key string) (uuid.UUID, error) {
	if tenant == uuid.Nil || actor == uuid.Nil || !idempotency.ValidKey(key) {
		return uuid.Nil, ErrInvalid
	}
	return uuid.NewSHA1(tenant, []byte("V2.Admit:"+actor.String()+":"+key)), nil
}

func WorkflowID(run uuid.UUID) (uuid.UUID, error) {
	if run == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	return uuid.NewSHA1(run, []byte("workflow")), nil
}

// NodeKey and TaskID implement the documented UUIDv5(workflow,stage:ordinal)
// rule. The same node key is persisted on photo.tasks for audit/replay.
func NodeKey(stage string, ordinal int) (string, error) {
	if !stagePattern.MatchString(stage) || ordinal < 0 || ordinal >= 5000 {
		return "", ErrInvalid
	}
	return fmt.Sprintf("%s:%d", stage, ordinal), nil
}

func TaskID(workflow uuid.UUID, stage string, ordinal int) (uuid.UUID, error) {
	if workflow == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	key, err := NodeKey(stage, ordinal)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.NewSHA1(workflow, []byte(key)), nil
}

func validTaskIdentity(workflow, id uuid.UUID, key string) bool {
	separator := strings.LastIndexByte(key, ':')
	if separator < 1 {
		return false
	}
	stage, number := key[:separator], key[separator+1:]
	ordinal, err := strconv.Atoi(number)
	if err != nil {
		return false
	}
	expectedKey, err := NodeKey(stage, ordinal)
	if err != nil || key != expectedKey {
		return false
	}
	expected, err := TaskID(workflow, stage, ordinal)
	return err == nil && id == expected
}
