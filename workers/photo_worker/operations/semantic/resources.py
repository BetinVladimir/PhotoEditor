"""Transfer task costs including simultaneous mask and manifest publication."""
from .prepare_transfer import planar_transfer_resources

DOCUMENT_LIMIT=8*1024*1024
MASK_ARRAY_OVERHEAD=4096
MAX_OUTPUT_BYTES=825169920


def semantic_transfer_resources(reference_shape,target_shape):
    """Native stage plus one live manifest and complete output role closure.

    RGB downloads/conversion, qualified analysis/model peaks and input-index
    materialization remain additive installer costs. Three documents upload
    sequentially, so one document reservation coexists with the mask owner.
    """
    costs=planar_transfer_resources(reference_shape,target_shape)
    costs['scratch_bytes']+=DOCUMENT_LIMIT
    costs['output_bytes']=4*target_shape[0]*target_shape[1]+MASK_ARRAY_OVERHEAD+3*DOCUMENT_LIMIT
    if costs['output_bytes']>MAX_OUTPUT_BYTES:raise ValueError('SEMANTIC_STAGED_OUTPUT_LIMIT')
    return costs
