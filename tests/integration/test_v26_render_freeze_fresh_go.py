from pathlib import Path
import json
import os
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
from render_full_freeze_fixture import seed_full_render_freeze


def test_full_json_documents_through_actual_freeze_service(database):
    with psycopg.connect(**database) as conn:
        fixture=seed_full_render_freeze(conn)
    root=Path(__file__).resolve().parents[2]
    import tempfile
    from contextlib import contextmanager
    from io import BytesIO
    from types import SimpleNamespace
    from photo_worker.contracts.base import canonical_bytes, canonical_hash
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.operations.session.contracts import CorrectionVector
    from photo_worker.v2.render_recipe import decode_frozen_render_recipe
    from photo_worker.v2.render_scalars import ResolvedRenderOverride
    from photo_worker.v2.render_style import prepare_render_style
    with tempfile.TemporaryDirectory(prefix='freeze-cross-',dir=root/'work') as directory:
        output=Path(directory)/'frozen.json'
        env=dict(os.environ,PHOTO_TEST_DATABASE_URL=make_conninfo(**database),PHOTO_RENDER_FREEZE_FRESH=json.dumps(fixture),PHOTO_RENDER_FREEZE_RESULT=str(output),GOCACHE='/private/tmp/photoeditor-go-cache')
        result=subprocess.run(['go','test','./internal/v2render','-run','^TestIntegrationRenderFreezeFreshDocuments$','-count=1'],cwd=root/'backend',env=env,capture_output=True,text=True,timeout=90)
        assert result.returncode==0,result.stdout+result.stderr
        frozen=decode_frozen_render_recipe(output.read_bytes())
        assert frozen.recipe.layer_stack is not None and len(frozen.recipe.overrides)==1
        assert frozen.scalars.effective_delta.ev==.1
        documents={canonical_hash(json.loads(body)):body.encode() for body in fixture['Objects'].values()}
        class Documents:
            @contextmanager
            def stream(self,ref):
                with BytesIO(documents[ref.sha256]) as stream:
                    yield stream
        zero=CorrectionVector(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0)
        inputs=SimpleNamespace(recipe=frozen,source=WorkingImage.model_validate_json(fixture['Objects']['image']))
        style=prepare_render_style(Documents(),inputs,(ResolvedRenderOverride(frozen.recipe.overrides[0],zero),),max_override_document_bytes=16384)
        assert style.merged.strength==.4 and style.effective.extensions.background.mode=='keep'
        assert style.effective.global_.gains_rgb==[2**.1]*3
