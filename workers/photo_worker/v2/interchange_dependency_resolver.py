"""Typed dependency adapter with explicit image/model/binary qualification."""
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.style import StyleV2
from photo_worker.contracts.override import OverrideV1
from photo_worker.contracts.images import MaskRef
from photo_worker.operations.session.contracts import CorrectionPlan
from .render_layers import RenderLayerStack
from .interchange_remap import BundleRemapper,UnsupportedDependency
from .interchange_recipe import ImportedRecipeDependency

_MODELS={'StyleV2':StyleV2,'Override':OverrideV1,'CorrectionPlan':CorrectionPlan,
         'LayerStack':RenderLayerStack,'MaskRef':MaskRef}


def _validator(model):
    return lambda value:model.model_validate_json(canonical_bytes(value))


class BundleRecipeDependencyResolver:
    """Qualification ports perform installed-runtime and canonical-source checks.

    No binary is allowed merely because the archive labels it as NPY/mask.
    Every publication uses deterministic remapped refs and dependency-first order.
    Storage/publication exceptions are not converted to unsupported outcomes.
    """
    def __init__(self,*,parsed,tenant,project,job,read_document,publish_document,
                 qualify_binding,qualify_binary,qualify_document,binary_contracts=(),
                 extra_validators=None,check_active=lambda:None):
        if not all(callable(port) for port in (publish_document,qualify_binding,qualify_binary,qualify_document)):
            raise ValueError('INTERCHANGE_DEPENDENCY_INSTALLATION')
        validators={name:_validator(model) for name,model in _MODELS.items()}
        extra=dict(extra_validators or {})
        if validators.keys() & extra.keys():
            raise ValueError('INTERCHANGE_DEPENDENCY_VALIDATOR_OVERRIDE')
        validators.update(extra)
        self.remapper=BundleRemapper(parsed,tenant,project,job,read_document,validators,binary_contracts,
            binding_rewriter=qualify_binding,binary_qualifier=qualify_binary,check_active=check_active)
        self.publish=publish_document
        self.qualify=qualify_document
        self.active=check_active
        self.published={}

    def __call__(self,original,contract,binding):
        self.active()
        evidence=self.remapper.inventory.get(original.artifact_id)
        if evidence is None or evidence[0].contract!=contract or evidence[0].sha256!=original.sha256:
            raise UnsupportedDependency('INTERCHANGE_DEPENDENCY_TYPE_OR_HASH')
        mapped=self.remapper.remap(original.artifact_id,original.sha256)
        root=self.remapper.documents.get(original.artifact_id)
        if root is None:
            raise UnsupportedDependency('INTERCHANGE_DEPENDENCY_DOCUMENT_REQUIRED')
        model=_MODELS.get(contract)
        typed=None if model is None else model.model_validate_json(root.body)
        if (contract=='LayerStack' and typed.source!=binding
                or contract=='CorrectionPlan' and not any(item.binding==binding for item in typed.items)
                or contract=='MaskRef' and canonical_hash(typed.canvas)!=binding.canvas_hash):
            raise UnsupportedDependency('INTERCHANGE_DEPENDENCY_CANONICAL_BINDING')
        self.qualify(root,binding.model_copy(deep=True))
        # Qualifications precede all publication for this resolved closure.
        pending=[doc for key,doc in self.remapper.documents.items() if key not in self.published]
        for document in pending:
            self.active()
            self.qualify(document,binding.model_copy(deep=True))
        for document in pending:
            self.active()
            expected=ArtifactRef(artifact_id=document.artifact_id,sha256=document.sha256)
            actual=self.publish(document)
            if not isinstance(actual,ArtifactRef) or actual!=expected:
                raise ValueError('INTERCHANGE_DEPENDENCY_PUBLICATION_MISMATCH')
            self.published[document.original_id]=actual.model_copy(deep=True)
        self.active()
        replacement=ArtifactRef.model_validate_json(canonical_bytes(mapped))
        return ImportedRecipeDependency(original.model_copy(deep=True),replacement,contract,
            None if contract=='StyleV2' else binding.model_copy(deep=True))
