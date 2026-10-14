package workflow

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// DeliveryContinuation describes the next real task from a verified prefix.
// SQL must independently prove that each supplied presentation output was adopted
// from the expected task/fence and physical arrays before publishing this task.
type DeliveryContinuation struct {
	Node             CompiledDownloadNode
	SourceJCS        []byte
	ExecutionProfile json.RawMessage
	Predecessor      uuid.UUID
	Stage            int
}

func CompileDeliveryContinuation(plan delivery.PreparePlan, parent uuid.UUID, child delivery.ExportChildIntent, source []byte, results []DeliveryPresentationOutput, targetICC string, registry Registry, limits DeliveryDerivativeLimits) (DeliveryContinuation, error) {
	zero := DeliveryContinuation{}
	if len(results) > 2 {
		return zero, v2input.ErrInvalid
	}
	var node CompiledDownloadNode
	var err error
	if len(results) == 0 {
		node, err = CompileDeliveryInitialNode(plan, parent, child, source, targetICC, registry)
	} else {
		first, firstErr := CompileDeliveryPresentation(plan, parent, child, source, registry)
		if firstErr != nil {
			return zero, firstErr
		}
		if first == nil {
			return zero, v2input.ErrInvalid
		}
		if len(results) == 1 && first.Operation == "derivative.resize" {
			result := results[0]
			pending, pendingErr := CompileDeliveryAfterResize(plan, parent, child, source, result.ImageRef, result.ImageJSON, result.ReportRef, result.ReportJSON, registry)
			if pendingErr != nil {
				return zero, pendingErr
			}
			if pending != nil {
				node = *pending
			} else {
				node, err = CompileDeliveryFinalEncode(plan, parent, child, source, results, targetICC, registry)
			}
		} else {
			node, err = CompileDeliveryFinalEncode(plan, parent, child, source, results, targetICC, registry)
		}
	}
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(child.RunID)
	if err != nil {
		return zero, err
	}
	node.ID, err = v2input.TaskID(workflow, node.Operation, 0)
	if err != nil {
		return zero, err
	}
	node.Key, err = v2input.NodeKey(node.Operation, 0)
	if err != nil {
		return zero, err
	}
	current := source
	predecessor := uuid.Nil
	if len(results) > 0 {
		current = results[len(results)-1].ImageJSON
		first, err := CompileDeliveryPresentation(plan, parent, child, source, registry)
		if err != nil || first == nil {
			return zero, v2input.ErrInvalid
		}
		previousOperation := first.Operation
		if len(results) == 2 {
			previousOperation = "derivative.sharpen"
		}
		predecessor, err = v2input.TaskID(workflow, previousOperation, 0)
		if err != nil {
			return zero, err
		}
	}
	var profile json.RawMessage
	if node.Operation == "export.encode" {
		profile, err = CompileDeliveryEncodeProfile(node, current, limits)
	} else {
		profile, err = CompileDeliveryDerivativeProfile(node, current, limits)
	}
	if err != nil {
		return zero, err
	}
	return DeliveryContinuation{Node: node, SourceJCS: append([]byte(nil), current...), ExecutionProfile: append(json.RawMessage(nil), profile...), Predecessor: predecessor, Stage: len(results)}, nil
}
