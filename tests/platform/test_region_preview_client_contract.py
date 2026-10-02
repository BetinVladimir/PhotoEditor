"""Actual worker serialization/encoding consumed by the TypeScript client."""
import base64
import json
import os
import subprocess
import tempfile
from hashlib import sha256
from pathlib import Path
from uuid import uuid4

import numpy as np
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.operations.region.presentation import prepare_region_presentation, write_region_presentation
from photo_worker.operations.region.result import build_region_manifest, verify_region_encoded
from test_region_preview_presentation import fixture, ROOT, ICC_LOCK


def test_worker_region_outputs_are_accepted_by_client():
    repo = Path(__file__).resolve().parents[2]
    runtime = repo / '.runtime'
    runtime.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='region-client-', dir=runtime) as directory:
        root = Path(directory)
        cases = []
        for alpha in (False, True):
            params, source = fixture(alpha=alpha)
            rgb = np.full((300, 400, 3), .25, np.float32)
            opacity = np.full((300, 400), .5, np.float32) if alpha else None
            presentation = prepare_region_presentation(params, source, rgb, opacity,
                ROOT, runtime_icc_lock_sha=ICC_LOCK)
            image = root / ('region.webp' if alpha else 'region.jpg')
            size = write_region_presentation(image, presentation)
            encoded = image.read_bytes()
            derivative = ArtifactRef(artifact_id=uuid4(), sha256=sha256(encoded).hexdigest())
            manifest = build_region_manifest(params, presentation, derivative, size)
            verify_region_encoded(manifest, params, encoded,
                target_sha256=presentation.profile_sha256, expected_alpha=alpha)
            raw = canonical_bytes(manifest)
            def catalog(ref, role, media, count, url):
                return dict(artifact_id=str(ref.artifact_id), sha256=ref.sha256,
                    role=role, media_type=media, bytes=count, url=url, expires_at='2099-01-01T00:00:00Z')
            document = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
            cases.append(dict(raw=base64.b64encode(raw).decode(), encoded=base64.b64encode(encoded).decode(),
                document=catalog(document, 'document', 'application/json', len(raw), 'https://photo.test/document'),
                derivative=catalog(derivative, 'derivative', presentation.media_type, size, 'https://photo.test/image'),
                binding=dict(request=params.request.model_dump(mode='json'), source=params.source.ref.model_dump(mode='json'),
                    canvas=dict(width=400, height=300), paramsSha256=canonical_hash(params),
                    iccSha256=presentation.profile_sha256, alpha=alpha)))
        path = root / 'cases.json'
        path.write_text(json.dumps(cases))
        env = dict(os.environ, PHOTO_REGION_CLIENT_CASES=str(path))
        result = subprocess.run(['npm', 'test', '--', '--run', 'src/region-preview-worker.test.ts'],
            cwd=repo / 'apps/photographer', env=env, capture_output=True, text=True, timeout=60)
        assert result.returncode == 0, result.stdout + result.stderr
        assert '2 passed' in result.stdout, result.stdout
