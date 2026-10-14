"""Actual Go compiler output consumed by Python worker contracts and memory admission."""
from pathlib import Path
import json
import os
import subprocess
import tempfile
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.media import MediaParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.media.derivative import dimensions
from photo_worker.operations.export.memory import admit_presentation
from photo_worker.v2.render_derivative import compile_render_derivative


def test_go_render_derivative_wire_matches_worker_compiler():
    root=Path(__file__).resolve().parents[2]
    with tempfile.TemporaryDirectory(prefix='derivative-parity-',dir=root/'work') as directory:
        output=Path(directory)/'wire.json'
        env=dict(os.environ,PHOTO_RENDER_DERIVATIVE_WIRE=str(output),GOCACHE='/private/tmp/photoeditor-go-cache')
        result=subprocess.run(['go','test','./internal/workflow','-run','^TestRenderDerivativeDocumentsBindCanonicalRootAndSource$','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=60)
        assert result.returncode==0,result.stdout+result.stderr
        cases=json.loads(output.read_bytes())
    assert len(cases)==4
    for case in cases:
        source=WorkingImage.model_validate_json(canonical_bytes(case['image']))
        for document in case['documents']:
            compiled=compile_render_derivative(ArtifactRef.model_validate_json(canonical_bytes(case['root_ref'])),canonical_bytes(case['root']),canonical_bytes(case['image']),case['srgb_sha'],document['Variant'])
            params_bytes=canonical_bytes(document['Params'])
            assert params_bytes==compiled.params_bytes
            assert document['ParamsSHA']==compiled.params_sha256
            assert tuple(ArtifactRef.model_validate_json(canonical_bytes(ref)) for ref in document['Inputs'])==compiled.inputs
            params=MediaParams.model_validate_json(params_bytes)
            width,height=dimensions(source.canvas.width,source.canvas.height,params.variant)
            required=admit_presentation(document['Profile'],source.canvas.width,source.canvas.height,width,height,alpha=source.alpha is not None)
            assert document['Profile']['resource_estimate']['ram_bytes']>=required
