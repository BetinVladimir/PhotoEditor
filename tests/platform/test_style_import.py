from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import hashlib,io,zipfile
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.style_import import StyleImportParams
from photo_worker.operations.style.import_style import execute
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for
from test_style_resolve import profile

def ref(b):return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(b).hexdigest())
class A:
 def __init__(self,raw,target):self.raw=raw;self.package=ref(raw);self.value=StyleImportParams(package=self.package,target_style_id=target);self.staged=[]
 def params(self,m):return self.value
 @contextmanager
 def stream(self,r):yield io.BytesIO(self.raw)
 def stage_contract(self,v,role):self.staged.append((role,v));return ref(canonical_bytes(v))
 def stage(self,path,reservation,role,media):self.staged.append((role,path.read_bytes()));return ref(path.read_bytes())
def run(tmp_path,raw):
 target=uuid4();a=A(raw,target);ctx=context_for(a.package,'style.import');ctx.lease.execution_profile['resource_estimate']={'ram_bytes':536870912};root=tmp_path/'scratch';root.mkdir()
 with ScratchSpace(root,uuid4(),uuid4(),1,200*1024*1024,min_free_bytes=0) as s:a.scratch=s;result=execute(ctx,a)
 return target,a,result
def test_import_json_rebinds_style_and_requires_review(tmp_path):
 target,a,result=run(tmp_path,canonical_bytes(profile()))
 style=a.staged[0][1];report=a.staged[1][1]
 assert style.style_id==str(target) and report.source_sha256==hashlib.sha256(canonical_bytes(profile())).hexdigest()
 assert result.review_required

def test_import_zip_validates_embedded_asset(tmp_path):
 data=b'asset';manifest=b'{"assets":[{"media_type":"application/octet-stream","path":"assets/a.bin","portable_asset_id":"a","sha256":"'+hashlib.sha256(data).hexdigest().encode()+b'"}],"schema_version":"style-assets/v1"}'
 out=io.BytesIO()
 with zipfile.ZipFile(out,'w') as z:z.writestr('style.json',canonical_bytes(profile()));z.writestr('manifest.json',manifest);z.writestr('assets/a.bin',data)
 _,a,_=run(tmp_path,out.getvalue());assert [x[0] for x in a.staged]==['style_asset','style_candidate','aggregate_report']
