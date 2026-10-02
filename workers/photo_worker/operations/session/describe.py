"""V22 session descriptor worker using pinned scene, face and person models."""
from __future__ import annotations

from contextlib import ExitStack
from uuid import UUID, uuid5

import cv2
import numpy as np

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.operations.face.detect import SFD_MEAN, _nms
from photo_worker.operations.face.landmarks import _crop, _points
from photo_worker.operations.features.scene import pad_preview
from photo_worker.operations.mask.person import MEAN, STD, _probability
from photo_worker.operations.identity.embedding import align_identity_face
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult

from .contracts import (Calibration, ConsentSnapshot, DescriptorShard, FaceObservation,
                        FaceObservations, FacePoint, FaceRect, FrameDescriptor,
                        InputSnapshot, SessionDescribeParams)
from .describe_features import frame_statistics, normalize_scene_embedding


REQUIRED_MODELS = frozenset({'dinov2_vits14', 'sfd', 'fan2d', 'lraspp_mobilenet_v3_large'})


def _preview(rgb: np.ndarray, longest: int) -> np.ndarray:
    height, width = rgb.shape[:2]
    scale = min(1.0, longest / max(height, width))
    size = (max(1, round(width * scale)), max(1, round(height * scale)))
    if size == (width, height):
        return np.asarray(rgb, np.float32)
    return cv2.resize(rgb, size, interpolation=cv2.INTER_AREA).astype(np.float32)


def _face_observations(rgb: np.ndarray, binding, pool: ModelPool,
                       backend: str, min_edge: int, check_active,
                       confirmed: dict[UUID, UUID] | None = None) -> FaceObservations:
    preview = _preview(rgb, 2048)
    height, width = preview.shape[:2]
    face_input = preview[..., ::-1] * np.float32(255) - SFD_MEAN
    with pool.acquire('sfd', backend) as engine:
        candidates = np.asarray(engine.infer(face_input), np.float32)
    if (candidates.ndim != 2 or candidates.shape[1] != 5 or not np.isfinite(candidates).all()
            or np.any(candidates[:, :4] < 0) or np.any(candidates[:, 2] > width)
            or np.any(candidates[:, 3] > height)
            or np.any(candidates[:, 2] <= candidates[:, 0])
            or np.any(candidates[:, 3] <= candidates[:, 1])):
        raise ValueError('MODEL_OUTPUT_INVALID')
    kept, truncated = _nms(candidates[candidates[:, 4] >= .5], .4, 100)
    if truncated:
        raise ValueError('FACE_COUNT_EXCEEDED')
    ordered = sorted(kept, key=lambda row: (-float(row[4]), float(row[1]), float(row[0]),
                                            float(row[3]), float(row[2])))
    entry = next(item for item in pool.lock.models if item.id == 'sfd')
    model_sha = canonical_hash([file.sha256 for file in entry.files])
    faces = []
    with pool.acquire('fan2d', backend) as engine:
        for index, row in enumerate(ordered):
            check_active()
            x0, y0, x1, y1, score = (float(value) for value in row)
            box = FaceRect(x0=x0 / width, y0=y0 / height,
                           x1=x1 / width, y1=y1 / height)
            crop, geometry = _crop(preview, box, 256, 1.25)
            points = _points(engine.infer(crop), width, height, geometry)
            face_id = uuid5(binding.asset_version_id, f'{model_sha}:{index}')
            observation_id = uuid5(binding.asset_version_id, f'{model_sha}:{face_id}')
            face_edge = min((box.x1 - box.x0) * width, (box.y1 - box.y0) * height)
            quality = max(0.0, min(1.0, score * min(1.0, face_edge / min_edge)))
            faces.append(FaceObservation(observation_id=observation_id, box=box,
                                         landmarks=[FacePoint(x=p.x, y=p.y) for p in points],
                                         embedding=None, quality=quality, person_id=None,
                                         face_id=face_id))
    if confirmed:
        for index, face in enumerate(faces):
            person_id = confirmed.get(face.face_id)
            if person_id is None:
                continue
            check_active()
            aligned = align_identity_face(preview, face)
            with pool.acquire('identity512', backend) as engine:
                embedding = engine.infer(aligned)
            faces[index] = FaceObservation.model_validate({
                **face.model_dump(mode='python'),
                'embedding': np.asarray(embedding, dtype=np.float32).tolist(),
                'person_id': person_id})
    return FaceObservations(schema_version=1, binding=binding, faces=faces)


def _confirmed_faces(consents: ConsentSnapshot,
                     selected: set[UUID]) -> dict[UUID, dict[UUID, UUID]]:
    grants = {}
    for grant in consents.grants:
        if grant.consent_id in grants:
            raise ValueError('DUPLICATE_CONSENT_GRANT')
        grants[grant.consent_id] = grant
    by_version: dict[UUID, dict[UUID, UUID]] = {}
    for face in consents.confirmed_faces:
        grant = grants.get(face.consent_id)
        if (grant is None or grant.purpose != 'identity_matching' or
                grant.epoch != face.epoch or grant.person_id != face.person_id):
            raise ValueError('INVALID_CONFIRMED_FACE_CONSENT')
        if face.asset_version_id not in selected:
            continue
        faces = by_version.setdefault(face.asset_version_id, {})
        if face.face_id in faces:
            raise ValueError('DUPLICATE_CONFIRMED_FACE')
        faces[face.face_id] = face.person_id
    return by_version


def _person_fraction(rgb: np.ndarray, pool: ModelPool, backend: str) -> float:
    preview = _preview(rgb, 512)
    with pool.acquire('lraspp_mobilenet_v3_large', backend) as engine:
        logits = engine.infer((preview - MEAN) / STD)
    person, _ = _probability(logits, preview.shape[0], preview.shape[1])
    return float(np.mean(person >= .5, dtype=np.float64))


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts,
            pool: ModelPool, backend: str) -> OperationResult:
    profile = ctx.lease.execution_profile
    if profile.get('renderer_version') != 'v2.2' or profile.get('backend') != backend:
        raise OperationFailure('CONFIG_INVALID')
    try:
        params = artifacts.params(SessionDescribeParams)
        if params.model_lock_sha != pool.lock.lock_id:
            raise ModelError('MODEL_LOCK_MISMATCH')
        snapshot = artifacts.contract(params.snapshot, InputSnapshot)
        calibration = artifacts.contract(params.calibration, Calibration)
        consents = artifacts.contract(params.consents, ConsentSnapshot)
        if not calibration.qualified or calibration.model_lock_sha != params.model_lock_sha:
            raise ModelError('MODEL_UNQUALIFIED')
        expected_digest = canonical_hash(snapshot.model_dump(mode='json', exclude={'snapshot_digest'}))
        if expected_digest != snapshot.snapshot_digest:
            raise ValueError('SNAPSHOT_DIGEST_MISMATCH')
        if [item.ordinal for item in snapshot.items] != list(range(len(snapshot.items))):
            raise ValueError('INVALID_SNAPSHOT_ORDINALS')
        if len(set(params.ordinals)) != len(params.ordinals) or params.ordinals != sorted(params.ordinals):
            raise ValueError('INVALID_SHARD_ORDINALS')
        if any(ordinal >= len(snapshot.items) for ordinal in params.ordinals):
            raise ValueError('ORDINAL_OUT_OF_SNAPSHOT')
        selected_ids = {snapshot.items[o].binding.asset_version_id for o in params.ordinals}
        confirmed = _confirmed_faces(consents, selected_ids)
        if confirmed and 'identity512' not in {item.id for item in pool.lock.models}:
            raise ModelError('IDENTITY_MODEL_UNAVAILABLE')
        if not REQUIRED_MODELS <= {item.id for item in pool.lock.models}:
            raise ModelError('MODEL_UNAVAILABLE')
    except ModelError as exc:
        raise OperationFailure(str(exc)) from exc
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    frames = []
    bytes_written = 0
    try:
        for ordinal in params.ordinals:
            ctx.check_active()
            item = snapshot.items[ordinal]
            if item.binding.source != item.working_image:
                raise ValueError('SOURCE_BINDING_MISMATCH')
            image = artifacts.contract(item.working_image, WorkingImage)
            if (image.dtype != 'float32' or image.color.profile_id != 'work_rec2020_gamma24_v1'
                    or canonical_hash(image.canvas) != item.binding.canvas_hash
                    or image.canvas.width * image.canvas.height > 200_000_000):
                raise ValueError('INVALID_WORKING_IMAGE')
            with ExitStack() as stack:
                local = stack.enter_context(artifacts.input(image.pixels, suffix='.npy'))
                work = stack.enter_context(read_array(local.path,
                    (image.canvas.height, image.canvas.width, 3), 'float32',
                    ctx.check_active, unit_interval=True))
                proxy = _preview(work, 2048)
                linear = np.power(proxy, np.float32(2.4), dtype=np.float32)
                statistics = frame_statistics(linear)
                srgb, _ = work_to_model(proxy)
                if image.alpha is not None:
                    local_alpha = stack.enter_context(artifacts.input(image.alpha, suffix='.npy'))
                    alpha = stack.enter_context(read_array(local_alpha.path,
                        (image.canvas.height, image.canvas.width), 'uint16', ctx.check_active))
                    alpha_proxy = _preview(alpha, 2048)
                    srgb = np.where(alpha_proxy[..., None] > 0, srgb, 0).astype(np.float32)
                with pool.acquire('dinov2_vits14', backend) as engine:
                    embedding = normalize_scene_embedding(engine.infer(pad_preview(srgb)))
                faces = _face_observations(srgb, item.binding, pool, backend,
                                           calibration.min_face_edge_px, ctx.check_active,
                                           confirmed.get(item.binding.asset_version_id))
                person_fraction = _person_fraction(srgb, pool, backend)
            face_ref = artifacts.stage_contract(faces, 'faces')
            bytes_written += len(canonical_bytes(faces))
            frames.append(FrameDescriptor(schema_version=1, binding=item.binding,
                ordinal=ordinal, captured_at=item.captured_at,
                camera_hash=item.camera_hash, scene_embedding=list(embedding),
                luma_quantiles=list(statistics.luma_quantiles),
                neutral_rgb=None if statistics.neutral_rgb is None else list(statistics.neutral_rgb),
                sharpness=statistics.sharpness, clipping_fraction=statistics.clipping_fraction,
                person_fraction=person_fraction, faces=face_ref))
    except ModelError as exc:
        raise OperationFailure(str(exc)) from exc
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    result = DescriptorShard(schema_version=1, snapshot_digest=snapshot.snapshot_digest,
                             frames=frames)
    root = artifacts.stage_contract(result, 'document')
    bytes_written += len(canonical_bytes(result))
    return OperationResult(outputs=[root], bytes_written=bytes_written)


def session_describe_operations(config: ArtifactConfig, pool: ModelPool,
                                backend: str) -> list[Operation]:
    def run(ctx: ExecutionContext) -> OperationResult:
        with TaskArtifacts.create(ctx, config, frozenset({'document', 'faces'})) as artifacts:
            return execute(ctx, artifacts, pool, backend)
    return [Operation('session.describe', 'gpu', run)]
