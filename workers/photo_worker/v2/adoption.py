"""Validate a V2 result's typed artifact closure before SQL adoption.

The caller supplies authoritative DB evidence and version-pinned object bytes.
This module performs no DB or object-store IO inside the adoption transaction.
"""
from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from typing import Any, Protocol
from uuid import UUID
import json

from jsonschema import Draft202012Validator, FormatChecker

from photo_worker.contracts.base import canonical_bytes, parse_json


MAX_JSON_BYTES = 8 * 1024 * 1024
MAX_DEPTH = 32
MAX_REFS = 1_000_000
MAX_SQL_INVENTORY = 30_001
SCHEMA_PATH = Path(__file__).resolve().parents[3] / "docs/implementation-v2/contracts/schemas.json"
OPERATIONS_PATH = Path(__file__).resolve().parents[3] / "docs/implementation-v2/contracts/operations.json"


class AdoptionInvalid(ValueError):
    """The result must not be published."""


@dataclass(frozen=True)
class ArtifactRef:
    artifact_id: UUID
    sha256: str

    @classmethod
    def parse(cls, value: Any) -> "ArtifactRef":
        if not isinstance(value, dict) or set(value) != {"artifact_id", "sha256"}:
            raise AdoptionInvalid("INVALID_ARTIFACT_REF")
        try:
            artifact_id = UUID(value["artifact_id"])
        except (TypeError, ValueError, AttributeError) as exc:
            raise AdoptionInvalid("INVALID_ARTIFACT_REF") from exc
        digest = value["sha256"]
        if not isinstance(digest, str) or len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
            raise AdoptionInvalid("INVALID_ARTIFACT_REF")
        return cls(artifact_id, digest)


@dataclass(frozen=True)
class ArtifactEvidence:
    ref: ArtifactRef
    media_type: str
    bytes: int
    role: str | None
    source: str  # "input" or "output", established by the DB adapter.


class EvidenceReader(Protocol):
    def evidence(self, ref: ArtifactRef) -> ArtifactEvidence: ...
    def read_json(self, ref: ArtifactRef, max_bytes: int) -> bytes: ...
    def staged_outputs(self) -> set[ArtifactRef]: ...


@dataclass(frozen=True)
class ChildBinding:
    contract: str
    role: str
    media_types: frozenset[str]


# Output refs without a binding are rejected. Input refs are trusted only when
# EvidenceReader proves an exact v2_task_inputs row for the current task.
OUTPUT_BINDINGS: dict[tuple[str, str], ChildBinding] = {
    ("RegionDerivativeManifest", "/derivative"): ChildBinding(
        "RegionEncodedDerivative", "derivative", frozenset({"image/jpeg", "image/webp"})),
    ("DescriptorShard", "/frames/*/faces"): ChildBinding(
        "FaceObservations", "faces", frozenset({"application/json"})),
    ("ScenePlan", "/groups/*/corrections"): ChildBinding(
        "CorrectionPlan", "correction_plan", frozenset({"application/json"})),
    ("ImageResult", "/working_image"): ChildBinding("WorkingImage", "working_image", frozenset({"application/json"})),
    ("ImageReport", "/parent_reports/*"): ChildBinding("ImageReport", "report", frozenset({"application/json"})),
    ("ImageResult", "/report"): ChildBinding("ImageReport", "report", frozenset({"application/json"})),
    ("ImageResult", "/changed_mask"): ChildBinding("MaskRef", "mask", frozenset({"application/json"})),
    ("ImageResult", "/generated_mask"): ChildBinding("MaskRef", "mask", frozenset({"application/json"})),
    ("WorkingImage", "/pixels"): ChildBinding("PixelArray", "pixels", frozenset({"application/x-npy"})),
    ("WorkingImage", "/alpha"): ChildBinding("MaskArray", "alpha", frozenset({"application/x-npy"})),
    ("WorkingImage", "/transform_chain/steps/*/inverse_field"): ChildBinding("DenseWarpArray", "warp_field", frozenset({"application/x-npy"})),
    ("MaskRef", "/artifact"): ChildBinding("MaskArray", "mask_pixels", frozenset({"application/x-npy"})),
    ("MaskRef", "/transform_chain/steps/*/inverse_field"): ChildBinding("DenseWarpArray", "warp_field", frozenset({"application/x-npy"})),
}


@dataclass(frozen=True)
class PreparedAdoption:
    inventory: tuple[dict[str, Any], ...]
    digest: str


def _load_registry(path: Path) -> dict[str, Any]:
    result = json.loads(path.read_text())
    if path == SCHEMA_PATH:
        # V2 references portable baseline contracts rather than duplicating them.
        from photo_worker.contracts.images import WorkingImage, MaskRef
        def portable(value):
            if isinstance(value, dict):
                return {key: ('#/$defs/Baseline'+item.removeprefix('#/$defs/')
                              if key=='$ref' and item.startswith('#/$defs/') and item!='#/$defs/ArtifactRef'
                              else portable(item)) for key,item in value.items()}
            if isinstance(value, list):return [portable(item) for item in value]
            return value
        for model in (WorkingImage, MaskRef):
            schema = model.model_json_schema()
            for name, definition in schema.pop('$defs', {}).items():
                if name!='ArtifactRef':
                    result['$defs'].setdefault('Baseline'+name, portable(definition))
            result['$defs'].setdefault(model.__name__, portable(schema))
    return result


def _pointer(path: tuple[str, ...]) -> str:
    return "/" + "/".join(path)


def _schema_refs(schema: dict[str, Any], value: Any, defs: dict[str, Any],
                 validator: Draft202012Validator, path: tuple[str, ...] = ()):
    """Yield only refs proven to be ArtifactRef by the pinned JSON schema."""
    ref = schema.get("$ref")
    if ref is not None:
        if ref == "#/$defs/ArtifactRef":
            yield path, ArtifactRef.parse(value)
            return
        if not ref.startswith("#/$defs/") or ref[8:] not in defs:
            raise AdoptionInvalid("UNKNOWN_SCHEMA_REF")
        yield from _schema_refs(defs[ref[8:]], value, defs, validator, path)
        return
    for key in ("allOf", "oneOf", "anyOf"):
        if key in schema:
            branches = schema[key]
            selected = [item for item in branches if validator.evolve(schema=item).is_valid(value)]
            if key != "allOf" and len(selected) != 1:
                raise AdoptionInvalid("AMBIGUOUS_SCHEMA_BRANCH")
            for item in selected:
                yield from _schema_refs(item, value, defs, validator, path)
            return
    if isinstance(value, dict):
        properties = schema.get("properties", {})
        additional = schema.get("additionalProperties")
        for key in sorted(value):
            child = properties.get(key, additional)
            if isinstance(child, dict):
                yield from _schema_refs(child, value[key], defs, validator, (*path, key))
    elif isinstance(value, list):
        prefix = schema.get("prefixItems", [])
        items = schema.get("items")
        for index, item in enumerate(value):
            child = prefix[index] if index < len(prefix) else items
            if isinstance(child, dict):
                yield from _schema_refs(child, item, defs, validator, (*path, "*"))


def prepare_v2_adoption(*, task_id: UUID, operation: str, version: str,
                        root: ArtifactRef, reader: EvidenceReader,
                        bindings: dict[tuple[str, str], ChildBinding] = OUTPUT_BINDINGS,
                        schemas: dict[str, Any] | None = None,
                        operations: dict[str, Any] | None = None,
                        params_ref: ArtifactRef | None = None,
                        layer_scratch_root: Path | None = None,
                        layer_scratch_bytes: int | None = None,
                        layer_alpha_mode: str = "require_float32",
                        region_target_icc_sha: str | None = None) -> PreparedAdoption:
    """Validate exact closure, roles and hashes; return a canonical SQL inventory.

    The reader must query primary DB metadata for this task/attempt/fence and
    return only exact declared inputs or ready, verified staged outputs.
    """
    schemas = schemas or _load_registry(SCHEMA_PATH)
    operations = operations or _load_registry(OPERATIONS_PATH)
    op = next((item for item in operations["operations"]
               if item["id"] == operation and item["version"] == version), None)
    if op is None:
        raise AdoptionInvalid("UNKNOWN_OPERATION")
    identity_bypass = False
    if operation == 'optical.lens':
        if params_ref is None:
            raise AdoptionInvalid('LENS_ADOPTION_PARAMS_REQUIRED')
        from .lens_bypass import _document, verify_lens_identity
        from photo_worker.operations.optical.contracts import OpticalLensParams
        params = _document(reader, params_ref, OpticalLensParams, 'input')
        if params.strength == 0:
            verify_lens_identity(reader, params_ref, root)
            identity_bypass = True
        else:
            from .lens_adoption import verify_lens_correction
            verify_lens_correction(reader, params_ref, root)
    if operation == 'region.preview':
        if params_ref is None:
            raise AdoptionInvalid('REGION_ADOPTION_PARAMS_REQUIRED')
        from .region_adoption import verify_region_result
        verify_region_result(reader, params_ref, root, target_icc_sha256=region_target_icc_sha)
    if operation == 'layer.compose':
        if params_ref is None:
            raise AdoptionInvalid('LAYER_ADOPTION_PARAMS_REQUIRED')
        from .layer_adoption import verify_layer_output_arrays
        verify_layer_output_arrays(reader,params_ref,root)
        if layer_scratch_root is None or layer_scratch_bytes is None:
            raise AdoptionInvalid('LAYER_ADOPTION_SCRATCH_REQUIRED')
        from .layer_adoption import verify_layer_composition
        verify_layer_composition(reader,params_ref,root,scratch_root=layer_scratch_root,max_bytes=layer_scratch_bytes,alpha_mode=layer_alpha_mode)
    defs = schemas["$defs"]
    root_type = op["output"]
    if root_type not in defs:
        raise AdoptionInvalid("UNKNOWN_ROOT_CONTRACT")
    policies = op["role_policies"]
    inventory: list[dict[str, Any]] = []
    active: set[tuple[UUID, str, str]] = set()
    visited: set[tuple[UUID, str, str]] = set()
    output_seen: set[ArtifactRef] = set()
    role_counts: dict[str, int] = {}
    type_by_id: dict[UUID, str] = {}

    def visit(ref: ArtifactRef, contract: str, path: str, depth: int,
              required_role: str | None = None) -> None:
        if depth > MAX_DEPTH or len(visited) >= MAX_REFS:
            raise AdoptionInvalid("CLOSURE_LIMIT_EXCEEDED")
        evidence = reader.evidence(ref)
        if evidence.ref != ref or evidence.source not in {"input", "output"}:
            raise AdoptionInvalid("ARTIFACT_EVIDENCE_MISMATCH")
        prior_type = type_by_id.setdefault(ref.artifact_id, contract)
        if prior_type != contract:
            if evidence.source != 'input' or 'declared_input' not in {prior_type, contract}:
                raise AdoptionInvalid("ARTIFACT_TYPE_CONFLICT")
            if prior_type == 'declared_input':type_by_id[ref.artifact_id] = contract
        if evidence.source == "output":
            role = required_role or "document"
            policy = policies.get(role)
            if (policy is None or evidence.role != role or evidence.media_type not in policy["media_types"]
                    or evidence.bytes > policy["max_bytes"]):
                raise AdoptionInvalid("OUTPUT_ROLE_MISMATCH")
            if ref not in output_seen:
                output_seen.add(ref)
                role_counts[role] = role_counts.get(role, 0) + 1
                if role_counts[role] > policy["max_count"]:
                    raise AdoptionInvalid("OUTPUT_COUNT_EXCEEDED")
        elif required_role == "document":
            raise AdoptionInvalid("ROOT_MUST_BE_STAGED")
        key = (ref.artifact_id, ref.sha256, contract)
        if key in active:
            raise AdoptionInvalid("ARTIFACT_CYCLE")
        if key in visited:
            return
        visited.add(key)
        purpose = sha256(f"tasks/{task_id}/{path}".encode()).hexdigest()
        inventory.append({"artifact_id": str(ref.artifact_id), "sha256": ref.sha256,
                          "purpose": purpose, "ordinal": len(inventory)})
        if len(inventory) > MAX_SQL_INVENTORY:
            raise AdoptionInvalid("SQL_INVENTORY_LIMIT_EXCEEDED")
        if evidence.source == "input" or evidence.media_type != "application/json":
            return
        if contract not in defs:
            raise AdoptionInvalid("UNKNOWN_CONTRACT")
        raw = reader.read_json(ref, MAX_JSON_BYTES)
        if not isinstance(raw, bytes) or len(raw) > MAX_JSON_BYTES or sha256(raw).hexdigest() != ref.sha256:
            raise AdoptionInvalid("ARTIFACT_BYTES_MISMATCH")
        try:
            value = parse_json(raw, max_bytes=MAX_JSON_BYTES)
        except ValueError as exc:
            raise AdoptionInvalid("ARTIFACT_JSON_INVALID") from exc
        validator = Draft202012Validator({**schemas, "$ref": f"#/$defs/{contract}"},
                                         format_checker=FormatChecker())
        if not validator.is_valid(value):
            raise AdoptionInvalid("ARTIFACT_SCHEMA_INVALID")
        active.add(key)
        try:
            for child_path, child_ref in _schema_refs(defs[contract], value, defs, validator):
                typed_path = _pointer(child_path)
                child_evidence = reader.evidence(child_ref)
                binding = bindings.get((contract, typed_path))
                if child_evidence.source == "output" and binding is None:
                    raise AdoptionInvalid("UNBOUND_OUTPUT_REFERENCE")
                child_type = binding.contract if binding else "declared_input"
                child_role = binding.role if binding else None
                if binding and child_evidence.media_type not in binding.media_types:
                    raise AdoptionInvalid("OUTPUT_MEDIA_MISMATCH")
                visit(child_ref, child_type, path + typed_path, depth + 1, child_role)
        finally:
            active.remove(key)

    visit(root, root_type, "$", 0, "document")
    if output_seen != reader.staged_outputs():
        raise AdoptionInvalid("STAGED_OUTPUT_CLOSURE_MISMATCH")
    for role, policy in policies.items():
        if identity_bypass and role in {"working_image", "pixels"}:
            continue
        if policy["required"] and role_counts.get(role, 0) == 0:
            raise AdoptionInvalid("REQUIRED_OUTPUT_MISSING")
    if len(output_seen) > op["max_output_count"]:
        raise AdoptionInvalid("OUTPUT_COUNT_EXCEEDED")
    body = canonical_bytes(inventory)
    if len(body) > 16 * 1024 * 1024:
        raise AdoptionInvalid("SQL_INVENTORY_LIMIT_EXCEEDED")
    return PreparedAdoption(tuple(inventory), sha256(body).hexdigest())
