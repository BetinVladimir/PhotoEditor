"""Review-required negative result handoff; runtime installation is separate."""
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.runtime.executor import OperationFailure,OperationResult
from .negative_stage import StagedNegativeResult


def negative_operation_result(staged,*,declared_inputs,output_budget,check_active):
    """Validate staged closure identities and budget before final checkpoint.

    Caller must have completed qualified leased SQL/manual negative staging and
    provide its repeated attempt/source/qualification checkpoint. Artifacts
    upload/adoption and output-role/content validation remain runtime obligations.
    """
    if not callable(check_active):raise ValueError('NEGATIVE_AUTHORITY')
    check_active()
    if (not isinstance(staged,StagedNegativeResult) or staged.review_required is not True
            or type(staged.bytes_written) is not int or staged.bytes_written<1
            or type(output_budget) is not int or not 1<=output_budget<=9007199254740991
            or staged.bytes_written>output_budget or not isinstance(staged.outputs,tuple)
            or len(staged.outputs)!=6 or not isinstance(declared_inputs,(list,tuple))
            or not 1<=len(declared_inputs)<=512):
        raise OperationFailure('NEGATIVE_RESULT_INVALID')
    try:
        outputs=tuple(ArtifactRef.model_validate_json(canonical_bytes(ref)) for ref in staged.outputs)
        inputs=tuple(ArtifactRef.model_validate_json(canonical_bytes(ref)) for ref in declared_inputs)
        document=ArtifactRef.model_validate_json(canonical_bytes(staged.document))
    except ValueError as exc:raise OperationFailure('NEGATIVE_RESULT_INVALID') from exc
    ids={ref.artifact_id for ref in outputs}
    if (len(ids)!=6 or any(ref.artifact_id.int==0 for ref in outputs)
            or document!=outputs[-1] or ids & {ref.artifact_id for ref in inputs}):
        raise OperationFailure('NEGATIVE_RESULT_INVALID')
    check_active()
    return OperationResult(outputs=[document],review_required=True,bytes_written=staged.bytes_written)
