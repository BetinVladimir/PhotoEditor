"""Standalone trusted V2 adoption and finalization process."""
from __future__ import annotations

from dataclasses import dataclass, field
from contextlib import ExitStack
from ipaddress import ip_address
from threading import Event
from typing import Mapping
from urllib.parse import urlsplit
from uuid import UUID
import os
from pathlib import Path

import psycopg
from minio import Minio

from .adoption import AdoptionInvalid
from .adoption_service import AdoptionCoordinator


@dataclass(frozen=True)
class AdopterConfig:
    database_url: str = field(repr=False)
    endpoint: str
    bucket: str
    access_key: str = field(repr=False)
    secret_key: str = field(repr=False)
    development: bool = False
    layer_scratch_root: Path | None = None
    layer_scratch_bytes: int | None = None
    region_target_icc_sha: str | None = None
    derivative_work_icc_sha: str | None = None
    exposure_enabled: bool = False
    delivery_encode_max_pixels: int | None = None
    derivative_linear_icc_sha: str | None = None
    restoration_preview_enabled: bool = False

    @classmethod
    def from_env(cls, env: Mapping[str, str] | None = None) -> "AdopterConfig":
        env = os.environ if env is None else env
        values = [env.get(key, "") for key in (
            "DATABASE_URL", "S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY")]
        if any(not value for value in values):
            raise ValueError("Missing V2 adopter configuration")
        if not 3 <= len(values[2]) <= 63:
            raise ValueError("Invalid V2 adopter bucket")
        development = env.get("APP_ENV") == "development"
        endpoint = urlsplit(values[1])
        try:
            loopback = endpoint.hostname == "localhost" or ip_address(endpoint.hostname or "").is_loopback
        except ValueError:
            loopback = False
        if (endpoint.scheme not in {"http", "https"} or not endpoint.netloc
                or endpoint.username or endpoint.password or endpoint.path or endpoint.query
                or endpoint.fragment or (endpoint.scheme == "http" and not (development and loopback))):
            raise ValueError("Invalid V2 adopter object-store endpoint")
        scratch_raw=env.get('PHOTO_ADOPTER_LAYER_SCRATCH_ROOT','')
        budget_raw=env.get('PHOTO_ADOPTER_LAYER_SCRATCH_BYTES','')
        if bool(scratch_raw)!=bool(budget_raw):
            raise ValueError('Layer adoption scratch root and budget must be paired')
        scratch=None;budget=None
        if scratch_raw:
            scratch=Path(scratch_raw)
            if not scratch.is_absolute() or scratch.resolve(strict=True)!=scratch or not scratch.is_dir():
                raise ValueError('Invalid layer adoption scratch root')
            if not budget_raw.isascii() or not budget_raw.isdigit() or len(budget_raw)>16:
                raise ValueError('Invalid layer adoption scratch budget')
            budget=int(budget_raw)
            if not 1<=budget<=2**53-1:raise ValueError('Invalid layer adoption scratch budget')
        icc_sha = None
        work_icc_sha = None
        linear_icc_sha = None
        icc_raw = env.get('PHOTO_ICC_ROOT', '')
        if icc_raw:
            from photo_worker.color.icc import ICCRegistry
            try:
                icc_root = Path(icc_raw)
                if (not icc_root.is_absolute() or icc_root.resolve(strict=True) != icc_root
                        or not icc_root.is_dir()):
                    raise ValueError('Invalid ICC root')
                registry = ICCRegistry(icc_root)
                profiles = {name: registry.resolve(name) for name in
                    ('srgb_v1', 'linear_srgb_v1', 'work_rec2020_gamma24_v1')}
                icc_sha = profiles['srgb_v1'].sha256
                work_icc_sha = profiles['work_rec2020_gamma24_v1'].sha256
                linear_icc_sha = profiles['linear_srgb_v1'].sha256
            except (OSError, ValueError, KeyError, TypeError):
                raise ValueError('Invalid adopter PHOTO_ICC_ROOT') from None
        exposure=env.get('PHOTO_ADOPTER_EXPOSURE_ENABLED','false')
        if exposure not in ('true','false'):
            raise ValueError('Invalid exposure adopter enablement')
        if exposure=='true':
            from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
            if work_icc_sha!=WORKING_PROFILE_SHA or scratch is None or budget is None:
                raise ValueError('Exposure adopter requires original ICC and scratch configuration')
        delivery=env.get('PHOTO_ADOPTER_DELIVERY_ENCODE_ENABLED','false')
        if delivery not in ('true','false'):
            raise ValueError('Invalid delivery encode adopter enablement')
        delivery_pixels=None
        if delivery=='true':
            raw=env.get('PHOTO_ADOPTER_DELIVERY_ENCODE_MAX_PIXELS','')
            if not raw.isascii() or not raw.isdigit() or len(raw)>9:
                raise ValueError('Invalid delivery encode pixel limit')
            delivery_pixels=int(raw)
            if not 1<=delivery_pixels<=200000000 or scratch is None or budget is None:
                raise ValueError('Delivery encode adopter requires bounded scratch and pixels')
        restoration_preview=env.get('PHOTO_ADOPTER_V28_RESTORATION_PREVIEW_ENABLED','false')
        if restoration_preview not in ('true','false'):
            raise ValueError('Invalid V28 restoration preview adopter enablement')
        if restoration_preview=='true' and (icc_sha is None or work_icc_sha is None):
            raise ValueError('V28 restoration preview adopter requires deployed ICC profiles')
        return cls(values[0], values[1], values[2], values[3], values[4], development,scratch,budget,icc_sha,work_icc_sha,exposure=='true',delivery_pixels,linear_icc_sha,restoration_preview=='true')


def _advisory_key(task_id: UUID) -> str:
    return "v2-adopt:" + str(task_id)


class Adopter:
    def __init__(self, read_connection, write_connection, objects, bucket: str, *, layer_scratch_root=None, layer_scratch_bytes=None,
                 render_replay=None,render_max_tile_bytes=None,region_target_icc_sha=None,derivative_work_icc_sha=None,derivative_linear_icc_sha=None,
                 semantic_authority=None,semantic_ram_bytes=None,semantic_scene_verifier=None,
                 semantic_working_icc_sha=None,exposure_working_icc_sha=None,donor_installation=None,negative_installation=None,relight_installation=None,perspective_installation=None,perspective_lines_installation=None,multi_installations=(),interchange_recipe_qualifier_factory=None,signing_operator=None,signing_sdk=None,delivery_encode_max_pixels=None,restoration_preview_enabled=False,stop: Event | None = None):
        self.stop = Event() if stop is None else stop
        self.read = read_connection
        self.write = write_connection
        from .negative_review_publication import NegativeReviewPublisher
        self.negative_review_publisher = NegativeReviewPublisher(read_connection,write_connection,objects,bucket,
            check_active=self.check_active)
        self.delivery_encode = None
        if delivery_encode_max_pixels is not None:
            from .delivery_encoded_service import DeliveryEncodeAdoptionService
            self.delivery_encode = DeliveryEncodeAdoptionService(read_connection,write_connection,objects,
                allowed_bucket=bucket,scratch_root=layer_scratch_root,max_bytes=layer_scratch_bytes,
                max_pixels=delivery_encode_max_pixels,check_active=self.check_active)
        if semantic_scene_verifier is not None:
            if semantic_authority is not None:
                raise ValueError('Conflicting semantic adoption authority installation')
            from .semantic_transfer_authority import sql_semantic_adoption_authority
            semantic_authority=sql_semantic_adoption_authority(read_connection,objects,bucket,
                working_icc_sha=semantic_working_icc_sha,verify_scene=semantic_scene_verifier,
                check_active=self.check_active)
        elif semantic_working_icc_sha is not None:
            raise ValueError('Semantic ICC pin requires scene authority installation')
        exposure_installation=None
        if exposure_working_icc_sha is not None:
            from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
            if (exposure_working_icc_sha!=WORKING_PROFILE_SHA or layer_scratch_root is None
                    or type(layer_scratch_bytes) is not int or not 1<=layer_scratch_bytes<=2**53-1):
                raise ValueError('Invalid exposure adopter ICC or scratch configuration')
            from .exposure_installation import versioned_exposure_adoption_installation
            exposure_installation=versioned_exposure_adoption_installation(read_connection,objects,bucket,
                working_icc_sha=exposure_working_icc_sha,check_active=self.check_active)
        from .semantic_snapshot_authority import semantic_snapshot_authority
        from .semantic_producer_authority import adopted_semantic_producer_authority
        snapshot=semantic_snapshot_authority(read_connection,check_active=self.check_active)
        provenance=adopted_semantic_producer_authority(read_connection,objects,bucket,check_active=self.check_active)
        self.exposure_enabled=exposure_installation is not None
        self.coordinator = AdoptionCoordinator(read_connection, write_connection, objects, bucket,
            layer_scratch_root=layer_scratch_root,layer_scratch_bytes=layer_scratch_bytes,
            render_replay=render_replay,render_max_tile_bytes=render_max_tile_bytes,
            region_target_icc_sha=region_target_icc_sha,semantic_authority=semantic_authority,
            semantic_ram_bytes=semantic_ram_bytes,semantic_plan_snapshot=snapshot,
            semantic_plan_provenance=provenance,exposure_installation=exposure_installation,donor_installation=donor_installation,negative_installation=negative_installation,relight_installation=relight_installation,perspective_installation=perspective_installation,perspective_lines_installation=perspective_lines_installation,multi_installations=multi_installations,interchange_recipe_qualifier_factory=interchange_recipe_qualifier_factory,signing_operator=signing_operator,signing_sdk=signing_sdk,check_active=self.check_active)

        self.multi_preview_runner = None
        self.multi_quality_runner = None
        self.perspective_preview_runner = None
        self.perspective_quality_runner = None
        self.restoration_preview_runner = None
        self.derivative_coordinator = None
        self.derivative_after = None
        if derivative_work_icc_sha is not None:
            from .render_derivative_service import RenderDerivativeAdoptionCoordinator
            self.derivative_coordinator = RenderDerivativeAdoptionCoordinator(read_connection,write_connection,objects,bucket,
                target_icc_sha256=region_target_icc_sha,working_icc_sha256=derivative_work_icc_sha,check_active=self.check_active,linear_icc_sha256=derivative_linear_icc_sha)

        if restoration_preview_enabled:
            if derivative_work_icc_sha is None or region_target_icc_sha is None:
                raise ValueError('V28 restoration preview requires deployed ICC profiles')
            from .restoration_preview_adoption import RestorationPreviewAdoptionCoordinator
            from .restoration_preview_runner import RestorationPreviewRunner
            self.restoration_preview_runner = RestorationPreviewRunner(
                RestorationPreviewAdoptionCoordinator(read_connection, write_connection,
                    objects, bucket, target_icc_sha256=region_target_icc_sha,
                    working_icc_sha256=derivative_work_icc_sha,
                    linear_icc_sha256=derivative_linear_icc_sha,
                    check_active=self.check_active))

        if multi_installations:
            if derivative_work_icc_sha is None or region_target_icc_sha is None:
                raise ValueError('Multi preview requires trusted working and target ICC profiles')
            from .multi_preview_adoption import MultiPreviewAdoptionCoordinator
            from .multi_preview_runner import MultiPreviewRunner
            self.multi_preview_runner = MultiPreviewRunner(
                MultiPreviewAdoptionCoordinator(read_connection, write_connection, objects, bucket,
                    target_icc_sha256=region_target_icc_sha, working_icc_sha256=derivative_work_icc_sha,
                    check_active=self.check_active))
            from .multi_quality_adoption import MultiQualityAdoptionCoordinator
            from .multi_quality_runner import MultiQualityRunner
            self.multi_quality_runner = MultiQualityRunner(
                MultiQualityAdoptionCoordinator(read_connection, write_connection, objects, bucket,
                    target_icc_sha256=region_target_icc_sha, working_icc_sha256=derivative_work_icc_sha,
                    check_active=self.check_active))

        if perspective_installation is not None:
            if derivative_work_icc_sha is None or region_target_icc_sha is None:
                raise ValueError('Perspective preview requires trusted working and target ICC profiles')
            from .perspective_preview_adoption import PerspectivePreviewAdoptionCoordinator
            from .perspective_preview_runner import PerspectivePreviewRunner
            self.perspective_preview_runner = PerspectivePreviewRunner(
                PerspectivePreviewAdoptionCoordinator(read_connection, write_connection, objects, bucket,
                    target_icc_sha256=region_target_icc_sha, working_icc_sha256=derivative_work_icc_sha,
                    check_active=self.check_active))

            from .perspective_quality_adoption import PerspectiveQualityAdoptionCoordinator
            from .perspective_quality_runner import PerspectiveQualityRunner
            self.perspective_quality_runner = PerspectiveQualityRunner(
                PerspectiveQualityAdoptionCoordinator(read_connection, write_connection, objects, bucket,
                    target_icc_sha256=region_target_icc_sha, working_icc_sha256=derivative_work_icc_sha,
                    check_active=self.check_active))

    def check_active(self) -> None:
        if self.stop.is_set():
            raise InterruptedError("Adopter stopped")

    def run_once(self) -> dict[str, int]:
        counts = {"adopted": 0, "rejected": 0, "finalized": 0, "retry": 0}
        # This is process health for the signer lane, not the aggregate retry
        # count of unrelated optional V2 adopters sharing the process.
        self.signing_runtime_healthy = True
        if self.stop.is_set():
            return counts
        candidates = self.read.execute("SELECT * FROM photo.v2_pending_adoptions()").fetchall()
        if self.exposure_enabled:
            candidates += self.read.execute("SELECT * FROM photo.v27_pending_exposure_adoptions()").fetchall()
        for tenant_id, _, task_id in candidates:
            if self.stop.is_set():
                return counts
            key = _advisory_key(task_id)
            acquired = self.read.execute(
                "SELECT pg_try_advisory_lock(hashtextextended(%s,0))", (key,)).fetchone()[0]
            if not acquired:
                continue
            try:
                try:
                    if self.coordinator.adopt(tenant_id, task_id):
                        counts["adopted"] += 1
                except AdoptionInvalid as exc:
                    code = str(exc)
                    if self.stop.is_set():
                        counts["retry"] += 1
                        continue
                    if code in {"V2_ADOPTION_CONTEXT_STALE", "RETRYABLE_CONFLICT", "LAYER_ADOPTION_SCRATCH_REQUIRED", "ADOPTION_SCRATCH_EXCEEDED",
                                "RENDER_ADOPTION_REPLAY_REQUIRED", "RENDER_ADOPTION_SCRATCH_REQUIRED",
                                "RENDER_ADOPTION_INPUTS_STALE", "RENDER_REPLAY_QUALIFICATION_MISMATCH",
                                "REGION_ADOPTION_ICC_REQUIRED", "SEMANTIC_REPLAY_AUTHORITY_REQUIRED",
                                "SEMANTIC_ADOPTION_SCRATCH_REQUIRED", "SEMANTIC_REPLAY_RAM_EXCEEDED",
                                "DONOR_ADOPTION_SCRATCH_REQUIRED", "DONOR_ADOPTION_RAM_REQUIRED", "DONOR_ADOPTION_INSTALLATION_REQUIRED", "DONOR_ADOPTION_RUNTIME_CHANGED",
                                "DONOR_ADOPTION_CONTEXT_STALE", "DONOR_ADOPTION_CONTEXT_CHANGED",
                                "INTERCHANGE_IMPORT_ADOPTION_INSTALLATION_REQUIRED",
                                "INTERCHANGE_IMPORT_QUALIFICATION_UNAVAILABLE",
                                "INTERCHANGE_IMPORT_QUALIFICATION_CHANGED",
                                "INTERCHANGE_IMPORT_PERSON_AUTHORITY_UNAVAILABLE",
                                "INTERCHANGE_IMPORT_PERSON_AUTHORITY_CHANGED",
                                "INTERCHANGE_IMPORT_ADOPTER_SCHEMA_PIN"}:
                        counts["retry"] += 1
                        continue
                    if code in {"SIGNING_ADOPTION_OPERATOR_REQUIRED",
                                "SIGNING_ADOPTION_SCRATCH_REQUIRED",
                                "SIGNING_ADOPTION_SDK_REQUIRED"}:
                        self.signing_runtime_healthy = False
                        counts["retry"] += 1
                        continue
                    try:
                        context = self.coordinator.context(tenant_id, task_id)
                    except AdoptionInvalid:
                        counts["retry"] += 1
                        continue
                    try:
                        with self.write.transaction():
                            self.check_active()
                            row = self.write.execute(
                                "SELECT photo.reject_v2_adoption(%s,%s,%s,%s)",
                                (tenant_id, task_id, context.result_id, code),
                            ).fetchone()
                            if row and row[0]:
                                counts["rejected"] += 1
                    except Exception:
                        self.signing_runtime_healthy = False
                        counts["retry"] += 1
                except Exception:
                    # DB/object-store availability is not a result-validation
                    # failure. Retry later without publishing exception text.
                    self.signing_runtime_healthy = False
                    counts["retry"] += 1
            finally:
                self.read.execute("SELECT pg_advisory_unlock(hashtextextended(%s,0))", (key,))
        if self.stop.is_set():
            return counts
        delivery_encode=getattr(self,'delivery_encode',None)
        if delivery_encode is not None:
            try:
                page=delivery_encode.process_pending(10)
                counts['adopted']+=page.adopted
                counts['rejected']+=len(page.terminal)
                # Backoff delays invalid results; they remain retries until a
                # seventh confirmed semantic refusal of the same result.
                counts['retry']+=len(page.rejected)
            except Exception:
                counts['retry']+=1
        if self.stop.is_set():
            return counts
        negative_reviews = getattr(self, 'negative_review_publisher', None)
        if negative_reviews is not None:
            try:
                published, retries = negative_reviews.process_pending()
                counts['adopted'] += published
                counts['retry'] += retries
            except Exception:
                counts['retry'] += 1
        if self.stop.is_set():
            return counts
        finalizations = self.read.execute("SELECT * FROM photo.v2_pending_finalizations()").fetchall()
        for tenant_id, run_id in finalizations:
            if self.stop.is_set():
                return counts
            try:
                with self.write.transaction():
                    self.check_active()
                    state = self.write.execute("SELECT photo.finalize_v2_run(%s,%s)",
                                               (tenant_id, run_id)).fetchone()[0]
                if state in {"completed", "waiting_review", "failed", "cancelled"}:
                    counts["finalized"] += 1
            except Exception:
                self.signing_runtime_healthy = False
                counts["retry"] += 1
        if self.derivative_coordinator is not None and not self.stop.is_set():
            candidates = self.read.execute('SELECT * FROM photo.pending_v26_render_derivative_receipts(%s)',(self.derivative_after,)).fetchall()
            if not candidates:
                self.derivative_after = None
            for tenant_id,run_id,variant,task_id in candidates:
                if self.stop.is_set():break
                self.derivative_after = task_id
                key = _advisory_key(task_id)
                if not self.read.execute('SELECT pg_try_advisory_lock(hashtextextended(%s,0))',(key,)).fetchone()[0]:continue
                try:
                    if self.derivative_coordinator.adopt(tenant_id,run_id,variant):counts['adopted']+=1
                    else:counts['rejected']+=1
                except Exception:
                    counts['retry']+=1
                finally:
                    self.read.execute('SELECT pg_advisory_unlock(hashtextextended(%s,0))',(key,))
        if self.multi_quality_runner is not None and not self.stop.is_set():
            changes = self.multi_quality_runner.run_once()
            for key in ('adopted', 'rejected', 'finalized', 'retry'):
                counts[key] += changes[key]
        if self.multi_preview_runner is not None and not self.stop.is_set():
            changes = self.multi_preview_runner.run_once()
            for key in ('adopted', 'rejected', 'finalized', 'retry'):
                counts[key] += changes[key]
        if self.restoration_preview_runner is not None and not self.stop.is_set():
            changes = self.restoration_preview_runner.run_once()
            for key in ('adopted', 'rejected', 'finalized', 'retry'):
                counts[key] += changes[key]
        if self.perspective_preview_runner is not None and not self.stop.is_set():
            changes = self.perspective_preview_runner.run_once()
            for key in ('adopted', 'rejected', 'finalized', 'retry'):
                counts[key] += changes[key]
        if self.perspective_quality_runner is not None and not self.stop.is_set():
            changes = self.perspective_quality_runner.run_once()
            for key in ('adopted', 'rejected', 'finalized', 'retry'):
                counts[key] += changes[key]
        return counts


def run_adopter(config: AdopterConfig, stop: Event | None = None, *,
                render_replay=None, render_max_tile_bytes=None, region_target_icc_sha=None,
                semantic_scene_verifier=None,semantic_ram_bytes=None,exposure_working_icc_sha=None,donor_installation=None,negative_installation=None,relight_installation=None,perspective_installation=None,perspective_lines_installation=None,multi_installations=(),interchange_recipe_qualifier_factory=None,interchange_recipe_qualifier_builder=None,interchange_recipe_operator_factory=None,signing_operator=None,signing_sdk=None) -> None:
    """Run with explicitly installed, trusted Render replay dependencies.

    Environment/client data cannot import a provider or manufacture qualification.
    The deployment runtime installer supplies the replay context and tile bound.
    """
    delivery_options={}
    signing_pins=None
    if signing_operator is not None or signing_sdk is not None:
        from photo_worker.operations.provenance.installation import InstalledSigningOperator
        if (not isinstance(signing_operator, InstalledSigningOperator)
                or not callable(getattr(signing_sdk, 'Reader', None))):
            raise ValueError('Signing adopter requires installed operator and SDK')
        if config.layer_scratch_root is None or type(config.layer_scratch_bytes) is not int or config.layer_scratch_bytes < 1 << 30:
            raise ValueError('Signing adopter requires bounded scratch')
        delivery_options['signing_operator']=signing_operator
        delivery_options['signing_sdk']=signing_sdk
        signing_pins=signing_operator.policy_pins()
    if sum(value is not None for value in (interchange_recipe_qualifier_factory,
            interchange_recipe_qualifier_builder, interchange_recipe_operator_factory)) > 1:
        raise ValueError('Conflicting import adopter recipe installations')
    if interchange_recipe_operator_factory is not None and not callable(interchange_recipe_operator_factory):
        raise ValueError('Invalid import adopter operator factory')
    if interchange_recipe_qualifier_builder is not None and not callable(interchange_recipe_qualifier_builder):
        raise ValueError('Invalid import adopter recipe builder')
    if interchange_recipe_qualifier_factory is not None:
        if not callable(interchange_recipe_qualifier_factory):
            raise ValueError('Invalid import adopter recipe qualifier installation')
        delivery_options['interchange_recipe_qualifier_factory']=interchange_recipe_qualifier_factory
    if config.delivery_encode_max_pixels is not None:
        if (type(config.delivery_encode_max_pixels) is not int or not 1<=config.delivery_encode_max_pixels<=200000000
                or config.layer_scratch_root is None or type(config.layer_scratch_bytes) is not int
                or not 1<=config.layer_scratch_bytes<=2**53-1):
            raise ValueError('Invalid delivery encode adopter configuration')
        delivery_options['delivery_encode_max_pixels']=config.delivery_encode_max_pixels
    donor_options={}
    if not isinstance(multi_installations,(list,tuple)):raise ValueError('Invalid multi adopter installations')
    if multi_installations:
        from .multi_adoption_prepare import validate_multi_adoption_installation
        owned=tuple(validate_multi_adoption_installation(item) for item in multi_installations)
        names=[item.options.operation for item in owned]
        if len(set(names))!=len(names):raise ValueError('Duplicate multi adopter operations')
        donor_options['multi_installations']=owned
    if perspective_lines_installation is not None:
        from .perspective_lines_adoption_prepare import validate_perspective_lines_adoption_installation
        donor_options['perspective_lines_installation']=validate_perspective_lines_adoption_installation(perspective_lines_installation)
    if perspective_installation is not None:
        from .perspective_adoption_prepare import validate_perspective_adoption_installation
        donor_options['perspective_installation']=validate_perspective_adoption_installation(perspective_installation)
    if relight_installation is not None:
        from copy import deepcopy
        from pathlib import Path
        from .relight_adoption_prepare import RelightAdoptionInstallation
        from photo_worker.operations.portrait.relight_runtime_lock import verify_installed_runtime
        item=relight_installation
        if (not isinstance(item,RelightAdoptionInstallation)
                or any(type(value) is not int or not 1<=value<=2**53-1 for value in (item.max_bytes,item.max_scratch_bytes))
                or item.max_bytes>item.max_scratch_bytes
                or not isinstance(item.scratch_root,Path) or not item.scratch_root.is_dir() or item.scratch_root.is_symlink()):
            raise ValueError('Invalid relight adopter installation')
        verify_installed_runtime(item.runtime_lock,item.runtime_sha)
        donor_options['relight_installation']=deepcopy(item)
    if negative_installation is not None:
        from copy import deepcopy
        from .negative_adoption_prepare import NegativeAdoptionInstallation
        from pathlib import Path
        from photo_worker.operations.optical.negative_adapter import NegativeRuntimeOptions
        item=negative_installation
        if (not isinstance(item,NegativeAdoptionInstallation) or not isinstance(item.options,NegativeRuntimeOptions)
                or not isinstance(item.policy,dict) or not callable(item.verify_color)
                or type(item.max_bytes) is not int or not 1<=item.max_bytes<=2**53-1
                or not isinstance(item.scratch_root,Path) or not item.scratch_root.is_dir() or item.scratch_root.is_symlink()):
            raise ValueError("Invalid negative adopter installation")
        donor_options["negative_installation"]=deepcopy(negative_installation)
    if donor_installation is not None:
        from copy import deepcopy
        from pathlib import Path
        from .portrait_donor_adoption_prepare import DonorAdoptionInstallation
        from photo_worker.operations.portrait.donor_adapter import DonorRuntimeOptions
        if (not isinstance(donor_installation,DonorAdoptionInstallation)
                or not isinstance(donor_installation.options,DonorRuntimeOptions)
                or not isinstance(donor_installation.policy,dict)
                or type(donor_installation.ram_max_bytes) is not int
                or not 1<=donor_installation.ram_max_bytes<=2**53-1
                or type(donor_installation.output_max_bytes) is not int
                or not 1<=donor_installation.output_max_bytes<=2**53-1
                or not isinstance(donor_installation.output_scratch_root,Path)
                or not donor_installation.output_scratch_root.is_dir()
                or donor_installation.output_scratch_root.is_symlink()):
            raise ValueError('Invalid donor adopter installation')
        donor_options['donor_installation']=deepcopy(donor_installation)
    if type(config.exposure_enabled) is not bool:
        raise ValueError('Invalid exposure adopter enablement')
    if config.exposure_enabled:
        from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
        if exposure_working_icc_sha is None:
            exposure_working_icc_sha=WORKING_PROFILE_SHA
    exposure_options={}
    if exposure_working_icc_sha is not None:
        from photo_worker.operations.semantic.exposure_color import WORKING_PROFILE_SHA
        if (exposure_working_icc_sha!=WORKING_PROFILE_SHA or config.derivative_work_icc_sha!=WORKING_PROFILE_SHA
                or config.layer_scratch_root is None or type(config.layer_scratch_bytes) is not int
                or not 1<=config.layer_scratch_bytes<=2**53-1):
            raise ValueError('Invalid exposure adopter ICC or scratch configuration')
        exposure_options['exposure_working_icc_sha']=exposure_working_icc_sha
    semantic_options={}
    if (semantic_scene_verifier is None)!=(semantic_ram_bytes is None):
        raise ValueError('Semantic scene verifier and RAM budget must be paired')
    if semantic_scene_verifier is not None:
        if (not callable(semantic_scene_verifier) or type(semantic_ram_bytes) is not int
                or not 1<=semantic_ram_bytes<=2**53-1 or config.layer_scratch_root is None
                or type(config.layer_scratch_bytes) is not int or not 1<=config.layer_scratch_bytes<=2**53-1):
            raise ValueError('Invalid semantic adopter runtime or scratch configuration')
        from photo_worker.operations.semantic.color_authority import semantic_working_color_authority
        semantic_working_color_authority(config.derivative_work_icc_sha)
        semantic_options=dict(semantic_scene_verifier=semantic_scene_verifier,
            semantic_ram_bytes=semantic_ram_bytes,semantic_working_icc_sha=config.derivative_work_icc_sha)
    if region_target_icc_sha is None:
        region_target_icc_sha = config.region_target_icc_sha
    elif config.region_target_icc_sha is not None and region_target_icc_sha != config.region_target_icc_sha:
        raise ValueError('Region adopter ICC pin conflicts with installed profile')
    if region_target_icc_sha is not None and (not isinstance(region_target_icc_sha,str)
            or len(region_target_icc_sha)!=64
            or any(c not in '0123456789abcdef' for c in region_target_icc_sha)):
        raise ValueError('Invalid Region adopter ICC pin')
    if (render_replay is None) != (render_max_tile_bytes is None):
        raise ValueError('Render replay and tile budget must be paired')
    if config.derivative_work_icc_sha is not None and (
            region_target_icc_sha is None or not isinstance(config.derivative_work_icc_sha,str)
            or len(config.derivative_work_icc_sha)!=64
            or any(c not in '0123456789abcdef' for c in config.derivative_work_icc_sha)):
        raise ValueError('Invalid derivative adopter ICC configuration')
    if config.derivative_linear_icc_sha is not None and (
            config.derivative_work_icc_sha is None or not isinstance(config.derivative_linear_icc_sha,str)
            or len(config.derivative_linear_icc_sha)!=64
            or any(c not in '0123456789abcdef' for c in config.derivative_linear_icc_sha)):
        raise ValueError('Invalid derivative linear ICC configuration')
    if render_replay is not None:
        if (not callable(render_replay) or type(render_max_tile_bytes) is not int
                or not 1 <= render_max_tile_bytes <= 2**53-1
                or config.layer_scratch_root is None
                or type(config.layer_scratch_bytes) is not int
                or not 1 <= config.layer_scratch_bytes <= 2**53-1):
            raise ValueError('Invalid Render adopter runtime or scratch configuration')
    if perspective_installation is not None and (config.derivative_work_icc_sha is None or region_target_icc_sha is None):
        raise ValueError('Perspective preview requires deployed ICC profiles')
    if config.restoration_preview_enabled and (config.derivative_work_icc_sha is None or region_target_icc_sha is None):
        raise ValueError('V28 restoration preview requires deployed ICC profiles')
    stop = Event() if stop is None else stop
    endpoint = urlsplit(config.endpoint)
    object_options={}
    if config.delivery_encode_max_pixels is not None or donor_installation is not None or negative_installation is not None or relight_installation is not None or perspective_installation is not None or perspective_lines_installation is not None or multi_installations or config.restoration_preview_enabled or interchange_recipe_operator_factory is not None or signing_pins is not None:
        import certifi
        import urllib3
        object_options['http_client']=urllib3.PoolManager(
            timeout=urllib3.Timeout(connect=5,read=10),retries=False,
            cert_reqs='CERT_REQUIRED',ca_certs=certifi.where())
    objects = Minio(endpoint.netloc, access_key=config.access_key,
                    secret_key=config.secret_key, secure=endpoint.scheme == "https",**object_options)
    if interchange_recipe_operator_factory is not None:
        interchange_recipe_qualifier_builder = interchange_recipe_operator_factory(
            objects, config.bucket)
        if not callable(interchange_recipe_qualifier_builder):
            raise ValueError('Invalid import adopter operator builder')
    options = "-c role=photo_scheduler -c statement_timeout=30000 -c lock_timeout=3000"
    with psycopg.connect(config.database_url, autocommit=True, connect_timeout=5,
                         options=options) as read_connection:
        with psycopg.connect(config.database_url, autocommit=True, connect_timeout=5,
                             options=options) as write_connection:
            if (interchange_recipe_qualifier_factory is not None
                    or interchange_recipe_qualifier_builder is not None):
                from .interchange_import_adopter_schema import require_interchange_import_adopter_schema
                require_interchange_import_adopter_schema(read_connection)
                require_interchange_import_adopter_schema(write_connection)
            if interchange_recipe_qualifier_builder is not None:
                from .interchange_import_adopter_builder import build_interchange_import_adopter
                delivery_options['interchange_recipe_qualifier_factory'] = build_interchange_import_adopter(
                    interchange_recipe_qualifier_builder, read_connection, stop)
            if config.delivery_encode_max_pixels is not None:
                from .delivery_encoded_schema import require_delivery_encode_schema
                require_delivery_encode_schema(read_connection)
                require_delivery_encode_schema(write_connection)
            if multi_installations:
                from .multi_adoption_schema import require_multi_adoption_schema
                require_multi_adoption_schema(read_connection)
                require_multi_adoption_schema(write_connection)
                from .multi_preview_schema import require_multi_preview_schema
                require_multi_preview_schema(read_connection)
                require_multi_preview_schema(write_connection)
                from .multi_quality_schema import require_multi_quality_schema
                require_multi_quality_schema(read_connection)
                require_multi_quality_schema(write_connection)
            if perspective_lines_installation is not None:
                from .perspective_lines_schema import require_perspective_lines_schema
                require_perspective_lines_schema(read_connection)
                require_perspective_lines_schema(write_connection)
            derivative_options = {}
            if config.derivative_work_icc_sha is not None:
                from .render_derivative_schema import require_render_derivative_schema
                require_render_derivative_schema(read_connection)
                require_render_derivative_schema(write_connection)
                derivative_options['derivative_work_icc_sha'] = config.derivative_work_icc_sha
                if config.derivative_linear_icc_sha is not None:
                    derivative_options['derivative_linear_icc_sha'] = config.derivative_linear_icc_sha
            if perspective_installation is not None:
                from .perspective_preview_schema import require_perspective_preview_schema
                require_perspective_preview_schema(read_connection)
                require_perspective_preview_schema(write_connection)
                from .perspective_quality_schema import require_perspective_quality_schema
                require_perspective_quality_schema(read_connection)
                require_perspective_quality_schema(write_connection)
            if config.restoration_preview_enabled:
                from .restoration_preview_schema import require_restoration_preview_schema
                require_restoration_preview_schema(read_connection)
                require_restoration_preview_schema(write_connection)
                derivative_options['restoration_preview_enabled'] = True
            with ExitStack() as runtime_scope:
                lease_connection = None
                signing_connection = None
                if interchange_recipe_operator_factory is not None:
                    lease_connection = runtime_scope.enter_context(psycopg.connect(
                        config.database_url, autocommit=True, connect_timeout=5,
                        options='-c role=photo_scheduler -c statement_timeout=3000 -c lock_timeout=1000'))
                    from .interchange_import_adopter_runtime import (
                        require_import_adopter_runtime_schema, ImportAdopterRuntimeLease)
                    require_import_adopter_runtime_schema(lease_connection)
                if signing_pins is not None:
                    signing_connection = runtime_scope.enter_context(psycopg.connect(
                        config.database_url, autocommit=True, connect_timeout=5,
                        options='-c role=photo_scheduler -c statement_timeout=3000 -c lock_timeout=1000'))
                    from .signing_adopter_runtime import (
                        require_signing_adopter_runtime_schema, SigningAdopterRuntimeLease)
                    require_signing_adopter_runtime_schema(signing_connection)
                adopter = Adopter(read_connection, write_connection, objects, config.bucket,
                    layer_scratch_root=config.layer_scratch_root,layer_scratch_bytes=config.layer_scratch_bytes,
                    render_replay=render_replay,render_max_tile_bytes=render_max_tile_bytes,
                    region_target_icc_sha=region_target_icc_sha,stop=stop,**delivery_options,**derivative_options,**semantic_options,**exposure_options,**donor_options)
                if signing_connection is not None:
                    while not stop.is_set():
                        first = adopter.run_once()
                        if stop.is_set():
                            break
                        if not isinstance(first,dict) or type(first.get('retry')) is not int:
                            raise ValueError('Signing adopter first pass invalid')
                        if adopter.signing_runtime_healthy is True:
                            break
                        stop.wait(1.0)
                elif lease_connection is not None and not stop.is_set():
                    adopter.run_once()
                if lease_connection is not None and not stop.is_set():
                    runtime_scope.enter_context(ImportAdopterRuntimeLease(lease_connection, stop, config.bucket))
                signing_lease = None
                if signing_connection is not None and not stop.is_set():
                    signing_lease = runtime_scope.enter_context(SigningAdopterRuntimeLease(
                        signing_connection, stop, config.bucket, signing_pins))
                while not stop.is_set():
                    counts = adopter.run_once()
                    if signing_lease is not None and not stop.is_set():
                        if not isinstance(counts,dict) or type(counts.get('retry')) is not int:
                            raise ValueError('Signing adopter pass invalid')
                        signing_lease.set_ready(adopter.signing_runtime_healthy is True)
                    stop.wait(1.0)
