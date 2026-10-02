"""Attempt-scoped source-point resolution from declared immutable transform fields."""
from contextlib import contextmanager,ExitStack
from photo_worker.contracts.images import DenseWarpTransform
from photo_worker.algorithms.dense_correspondence import chain_source_point
from .arrays import read_inverse_field


@contextmanager
def open_source_correspondence(artifacts,image,check_active=lambda:None):
    """Yield a callable; all artifact mappings stay inside this context.

    TaskArtifacts input verifies the declared ref and immutable bytes. Repeated
    fields are mapped once; conflicting shapes fail rather than reinterpret data.
    The callable rejects use after context exit, including empty chains.
    """
    image.transform_chain.validate_target(image.canvas)
    fields={};shapes={};active=True
    with ExitStack() as stack:
        for step in image.transform_chain.steps:
            check_active()
            if not isinstance(step,DenseWarpTransform):continue
            key=(step.inverse_field.artifact_id,step.inverse_field.sha256)
            shape=(step.to_canvas.height,step.to_canvas.width)
            if key in fields:
                if shapes[key]!=shape:raise ValueError('DENSE_FIELD_SHAPE_CONFLICT')
                continue
            local=stack.enter_context(artifacts.input(step.inverse_field,suffix='.npy'))
            fields[key]=stack.enter_context(read_inverse_field(local.path,shape,check_active))
            shapes[key]=shape
        def source_point(point):
            if not active:raise RuntimeError('SOURCE_CORRESPONDENCE_CLOSED')
            check_active()
            if len(point)!=2:return None
            if not (.5<=point[0]<=image.canvas.width-.5 and .5<=point[1]<=image.canvas.height-.5):return None
            return chain_source_point(image.transform_chain,point,
                lambda ref:fields[(ref.artifact_id,ref.sha256)],check_active=check_active)
        try:yield source_point
        finally:active=False
