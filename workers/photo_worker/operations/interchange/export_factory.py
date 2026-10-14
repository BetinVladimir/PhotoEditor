"""Explicit format selection for installed concrete export preparers."""
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_plan import InterchangePlan
from .export_bundle_factory import BundleExportPreparationFactory
from .export_sidecar_factory import SidecarExportPreparationFactory
from .recipe_factory import PreparedImportRecipeFactory


class ExportPreparationFactory:
    def __init__(self, recipe_factory, *, occurrence_factory=None, person_origin_factory=None):
        if not isinstance(recipe_factory,PreparedImportRecipeFactory):
            raise ValueError('INTERCHANGE_EXPORT_INSTALLATION_INVALID')
        self.bundle=BundleExportPreparationFactory(recipe_factory,
            occurrence_factory=occurrence_factory,person_origin_factory=person_origin_factory)
        self.sidecars=SidecarExportPreparationFactory(recipe_factory.icc)

    def __call__(self,ctx,artifacts,authority,plan,reader):
        ctx.check_active()
        plan=InterchangePlan.model_validate_json(canonical_bytes(plan))
        factory=self.bundle if plan.format=='photoeditor_bundle' else self.sidecars
        return factory(ctx,artifacts,authority,plan,reader)
