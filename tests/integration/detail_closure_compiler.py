"""Work-only compiler/SQL registration adapter for the controlled detail graph.

No handwritten closure hashes/entries; use Go domain compilers and production
register/seal functions. The material store is relational, not physical S3 proof.
"""
import base64
import json
import os
from pathlib import Path
import signal
import subprocess
from hashlib import sha256
from uuid import UUID
from psycopg.types.json import Jsonb
from photo_worker.contracts.base import canonical_bytes
from conftest import ROOT

class ClosureCompiler:
    def __init__(self,evidence):
        self.evidence=Path(evidence);self.evidence.mkdir(parents=True,exist_ok=False)

    def compile(self,stage,state,published=None):
        folder=self.evidence/(stage+'-'+str(len(list(self.evidence.iterdir()))));folder.mkdir()
        encode=lambda b:base64.b64encode(b).decode()
        manifest=state['manifest'];material=state['material']
        draft=dict(TenantID=str(state['tenant']),ActorID=str(state['actor']),AuthSessionID=str(state['session']),
            ProjectID=str(state['project']),ProfileID=str(state['profile']),RunID=str(state['run']),
            WorkflowID=str(state['workflow']),SnapshotID=str(state['snapshot']),Manifest=manifest,
            ManifestSHA=state['manifest_sha'],PolicySHA=state['policy_sha'],
            ManifestBody=encode(canonical_bytes(manifest)),RecoveryBody=encode(canonical_bytes(state['recovery'])))
        request=dict(Stage=stage,Draft=draft,Policy=state['policy'],Published=published or {},
            Params={'artifact_id':str(state['params_id']),'sha256':state['params_sha']})
        if stage=='evaluate':
            claim=state['claim']
            request['Lease']=dict(ExpansionID=str(claim[0]),Token=str(claim[1]),Until=claim[2].isoformat(),
                TenantID=str(state['tenant']),RunID=str(state['run']),WorkflowID=str(state['workflow']),
                Candidate={'artifact_id':str(claim[6]),'sha256':claim[7]})
            frozen={str(event):raw for event,raw in state['frozen']}
            details={str(event):json.loads(raw) for event,raw in state['detail']}
            resolved=[]
            for example in manifest['examples']:
                if example['split']!='holdout':continue
                case=example['render_case'];source=material.images[case['working_image']['artifact_id']]
                recipe=frozen[example['event_id']]
                row=dict(EventID=example['event_id'],WorkingImage=case['working_image'],PixelCount=4,
                    Pixels=source['pixels'],Protected=case['protected'],ProtectedPixels=source['protected_pixels'],
                    BaseStyle=json.loads(recipe)['base_style'],BaseRecipeBody=encode(recipe),ApprovedRecipeBody=encode(recipe),
                    RegionMasks=[],Extras=[])
                if case.get('detail_evidence'):
                    detail=details[example['event_id']]
                    entries=[dict(ref=case['detail_evidence'],contract='FrozenPortraitDetailHoldoutEvidence'),
                        dict(ref=detail['image'],contract='WorkingImage'),dict(ref=detail['params'],contract='PortraitParams'),
                        dict(ref=detail['resolved'],contract='ResolvedStyle')]
                    entries.extend(dict(ref=ref,contract='NpyArray') for ref in detail['mask_artifacts'])
                    row.update(DetailEvidence=case['detail_evidence'],DetailProof=dict(Evidence=detail,Entries=entries))
                resolved.append(row)
            request['Resolved']=resolved
        path=folder/'request.json';output=folder/'compiled.json';path.write_bytes(canonical_bytes(request));path.chmod(0o600)
        env={k:v for k,v in os.environ.items() if not k.startswith('PHOTO_')}
        env.update(PHOTO_TEST_V23_DETAIL_CLOSURE_REQUEST=str(path),PHOTO_TEST_V23_DETAIL_CLOSURE_OUTPUT=str(output),
            GOCACHE=str(ROOT/'work/go-cache'),GOMODCACHE=str(ROOT/'.cache/go-mod'),GOPATH=str(ROOT/'.runtime/go'),
            TMPDIR=str(ROOT/'.cache/tmp'),GOPROXY='off',GOSUMDB='off',GOTOOLCHAIN='local')
        name='TestWriteV23DetailClosures'
        command=['go','test','-json','-p','1','./internal/v2learning','-run','^'+name+'$','-count=1','-timeout=60s']
        expected=json.loads(Path(__file__).with_name('source-sha256.json').read_text())
        before={name:sha256((ROOT/name).read_bytes()).hexdigest() for name in expected}
        assert before==expected, 'reviewed source inventory changed before compiler launch'
        payload=json.loads(Path(__file__).with_name('payload-sha256.json').read_text())
        exporter=ROOT/'backend/internal/v2learning/detail_closure_export_test.go'
        assert sha256(exporter.read_bytes()).hexdigest()==payload['detail_closure_export_test.go']
        timed_out=False
        with (folder/'go.jsonl').open('w') as stdout,(folder/'stderr.log').open('w') as stderr:
            child=subprocess.Popen(command,cwd=ROOT/'backend',env=env,stdout=stdout,stderr=stderr,start_new_session=True)
            try:code=child.wait(timeout=90)
            except subprocess.TimeoutExpired:
                timed_out=True
                try:os.killpg(child.pid,signal.SIGINT)
                except ProcessLookupError:pass
                try:code=child.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    try:os.killpg(child.pid,signal.SIGKILL)
                    except ProcessLookupError:pass
                    code=child.wait(timeout=5)
        after={name:sha256((ROOT/name).read_bytes()).hexdigest() for name in expected}
        assert after==before, 'source changed during compiler execution'
        events=[json.loads(line) for line in (folder/'go.jsonl').read_text().splitlines() if line.startswith('{')]
        (folder/'result.json').write_text(json.dumps(dict(exit=code,timed_out=timed_out,command=command,source_before=before,source_after=after),indent=2)+'\n')
        assert code==0 and not timed_out
        assert [e['Action'] for e in events if e.get('Test')==name and e['Action'] in ('pass','fail','skip')]==['pass']
        assert not any(e['Action'] in ('skip','fail') for e in events)
        result=json.loads(output.read_text())
        # Ensure the staged/published Params are exactly what the domain compiler used.
        assert UUID(result['params']['artifact_id'])==state['params_id']
        assert result['params']['sha256']==state['params_sha']
        assert sha256(canonical_bytes(result['params_body'])).hexdigest()==state['params_sha']
        result['_tenant'],result['_run'],result['_project']=state['tenant'],state['run'],state['project']
        result['_state'],result['_stage']=state,stage
        return result

    def register(self,db,compiled):
        tenant,run,task=compiled['_tenant'],compiled['_run'],UUID(compiled['task'])
        # Exercise actual SQL publication/ownership/accounting. Object versions are
        # synthetic relational receipts; this adapter makes no physical S3 claim.
        db.execute('SET LOCAL ROLE photo_scheduler')
        published={}
        original_entries=compiled['entries']
        original_digest=compiled['input_digest']
        def publish(item):
            raw=canonical_bytes(item['body']);purpose=item['purpose']
            assert sha256(raw).hexdigest()==item['ref']['sha256']
            outcome,aid,token,ready=db.execute(
                'SELECT * FROM photo.reserve_v2_input_document(%s,%s,%s,%s,%s,%s,%s)',
                (tenant,run,task,purpose,item['ref']['sha256'],len(raw),'test-bucket')).fetchone()
            assert outcome in ('created','resumed','replayed') and aid is not None
            assert ready is (outcome=='replayed')
            assert purpose not in published
            assert db.execute('SELECT photo.complete_v2_input_document('+','.join(['%s']*10)+')',
                (tenant,run,task,purpose,aid,token,item['ref']['sha256'],len(raw),'test-bucket','fixture-v1')).fetchone()==(True,)
            published[purpose]={'artifact_id':str(aid),'sha256':item['ref']['sha256']}
        for item in compiled['transport_bodies']:
            if item['contract']=='InputClosureChunk':publish(item)
        rebound=self.compile(compiled['_stage'],compiled['_state'],published)
        assert rebound['entries']==original_entries and rebound['input_digest']==original_digest
        manifests=[item for item in rebound['transport_bodies'] if item['contract']=='InputClosureManifest']
        assert len(manifests)==1
        publish(manifests[0])
        final=self.compile(compiled['_stage'],compiled['_state'],published)
        assert final['entries']==original_entries and final['input_digest']==original_digest
        assert len(final['transport_bodies'])==len(published)
        assert final['direct']==compiled['direct'] and final['profile']==compiled['profile']
        assert final['built']['Count']==compiled['built']['Count']
        assert final['built']['SHA256']==compiled['built']['SHA256']
        for item in final['transport_bodies']:
            assert item['ref']==published[item['purpose']]
        compiled=final
        entries=compiled['entries']
        for first in range(0,len(entries),512):
            batch=entries[first:first+512]
            assert db.execute('SELECT photo.register_v2_task_input_batch(%s,%s,%s,%s,%s)',
                (tenant,run,task,first,Jsonb(batch))).fetchone()==(len(batch),)
        built=compiled['built']
        assert db.execute('SELECT photo.seal_v2_task_inputs('+','.join(['%s']*8)+')',
            (tenant,run,task,Jsonb(built['Manifest']),Jsonb(built['Chunks']),built['Count'],built['SHA256'],Jsonb(compiled['direct']))).fetchone()==(True,)
        db.execute('RESET ROLE')
