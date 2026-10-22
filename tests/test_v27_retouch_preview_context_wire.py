"""Shared Go/Python context bytes; content only, not production qualification."""
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
from photo_worker.contracts.images import MaskRef


def test_shared_retouch_context_wire_roundtrip():
    root=Path(__file__).resolve().parents[1]/'backend/internal/semanticreview/testdata'
    raw=(root/'retouch_preview_context.json').read_bytes()
    context=RetouchPreviewAdapterContext.model_validate_json(raw)
    assert canonical_bytes(context)==raw
    manual=(root/'retouch_preview_manual_mask.json').read_bytes()
    assert canonical_bytes(MaskRef.model_validate_json(manual))==manual
    assert canonical_bytes(context.faces[0].manual)==manual
    assert context.beauty.texture_keep==.8
