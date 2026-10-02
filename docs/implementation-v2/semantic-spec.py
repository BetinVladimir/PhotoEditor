"""Executable documentation: semantic reference rules, NOT application/integration tests."""
from pathlib import Path
from copy import deepcopy
import json, math, unittest, unicodedata
from jsonschema import Draft202012Validator
R=Path(__file__).resolve().parent
D=json.loads((R/'contracts/schemas.json').read_text())['$defs']
OPS={o['id']:o for o in json.loads((R/'contracts/operations.json').read_text())['operations']}
MAX_REV=9223372036854775807

def require(ok, code):
    if not ok: raise ValueError(code)

def revision(value, allow_zero=False):
    require(isinstance(value,str) and value.isascii() and value.isdecimal(), 'REVISION')
    require(str(int(value))==value, 'REVISION_CANONICAL')
    require((0 if allow_zero else 1)<=int(value)<=MAX_REV,'REVISION_RANGE')
    return int(value)

def review_batch(state,expected,sha,decisions):
    require(state['state']=='waiting_review','STATE')
    require(revision(expected)==state['revision'],'CAS')
    require(sha==state['sha'],'SHA')
    require(1<=len(decisions)<=100,'BATCH_SIZE')
    ids=[i for i,d in decisions]
    require(len(ids)==len(set(ids)),'DUPLICATE')
    for i,d in decisions:
        require(i in state['targets'] and state['targets'][i]=='proposed','TARGET')
        require(d in ('approve','reject'),'DECISION')
    out=deepcopy(state)
    approved=[i for i,d in decisions if d=='approve']
    child='child-'+str(state['revision']) if approved else None
    for i,d in decisions:out['targets'][i]='queued' if d=='approve' else 'rejected'
    out['revision']+=1
    return out,child,sum(v=='proposed' for v in out['targets'].values())

def close_review(state,expected,sha):
    require(state['state']=='waiting_review','STATE')
    require(revision(expected)==state['revision'] and sha==state['sha'],'CAS_OR_SHA')
    require('proposed' not in state['targets'].values(),'UNRESOLVED')
    out=deepcopy(state);out['state']='completed';out['revision']+=1
    return out

def validate_delivery(snapshot,presets,slots,digest,allow_omitted):
    require(snapshot and presets,'EMPTY')
    ps={p['id']:p for p in presets}
    require(len(ps)==len(presets) and any(p['required'] for p in presets),'PRESETS')
    expected={(i,p) for i in range(len(snapshot)) for p in ps}
    seen=set();ready=omitted=0
    for s in slots:
        key=(s['ordinal'],s['preset_id'])
        require(key in expected and key not in seen,'SLOT_SET');seen.add(key)
        p=ps[s['preset_id']]
        require(s['digest']==digest and s['binding']==snapshot[s['ordinal']],'BINDING')
        require(s['preset_sha']==p['sha'] and s['required']==p['required'],'PRESET_SHA')
        if s['state']=='ready':
            require(s['export'] is not None and s['export_id'] is not None and s['reason'] is None,'READY');ready+=1
        else:
            require(s['state']=='omitted' and not s['required'] and allow_omitted,'REQUIRED_FAILED')
            require(bool(s['reason']) and s['export'] is None and s['export_id'] is None,'OMISSION');omitted+=1
    require(seen==expected,'INCOMPLETE')
    return ready,omitted

def validate_training(examples,available):
    require(40<=len(examples)<=5000,'COUNT')
    ids=[e['id'] for e in examples];require(len(ids)==len(set(ids)),'EVENT_DUPLICATE')
    lineage=[e['lineage'] for e in examples];require(len(lineage)==len(set(lineage)),'LINEAGE_LEAKAGE')
    train={e['session'] for e in examples if e['split']=='train'}
    hold={e['session'] for e in examples if e['split']=='holdout'}
    require(not train&hold and len(train)>=4 and len(hold)>=2,'SESSION_SPLIT')
    require(sum(e['split']=='train' for e in examples)>=20 and sum(e['split']=='holdout' for e in examples)>=10,'SPLIT_COUNT')
    for e in examples:
        require(set(e['refs'])=={'working_image','base_recipe','approved_recipe','protected','faces'},'RENDER_CASE')
        require(all(v in available for v in e['refs'].values()),'MISSING_INPUT')
        require(e['approval_binding']==e['binding'],'APPROVAL_SOURCE')

def eligibility(metric,safety,review=None,revoked=False):
    if revoked:return 'disabled'
    if not metric or safety=='fail':return 'rejected'
    if safety=='pass':return 'eligible'
    if review is None:return 'needs_review'
    return 'eligible' if review else 'rejected'

def check_output(operation,members,reserved):
    p=OPS[operation]['role_policies'];counts={};total=0
    for role,size,mime in members:
        require(role in p,'UNDECLARED_ROLE');q=p[role]
        counts[role]=counts.get(role,0)+1;total+=size
        require(counts[role]<=q['max_count'] and 0<=size<=q['max_bytes'] and mime in q['media_types'],'ROLE_LIMIT')
    require(total<=reserved,'RESERVATION')

def check_closure(root,objects,inputs,staged):
    visited=set();active=set()
    def visit(ref):
        require(ref in inputs or ref in staged,'UNDECLARED_REF')
        require(ref not in active,'CYCLE')
        if ref in visited:return
        require(ref in objects,'MISSING_OBJECT');active.add(ref)
        for child in objects[ref]:visit(child)
        active.remove(ref);visited.add(ref)
    visit(root);require(staged<=visited,'ORPHAN_OUTPUT')
    return visited

def path_ok(path):
    require(isinstance(path,str) and 0<len(path)<=240 and unicodedata.normalize('NFC',path)==path,'PATH')
    require(not any(c in path for c in ('\\',':','\x00')),'PATH')
    require(all(p not in ('','.','..') for p in path.split('/')),'PATH')

def safe_blend(s,p,mask,protected,strength):
    e=0 if protected>0 else mask*strength
    return s if e==0 else (1-e)*s+e*p

def relight(x,y,depth,lights,sigma):
    return 2**max(-1,min(1,sum(l['ev']*math.exp(-((x-l['x'])**2+(y-l['y'])**2)/(2*l['r']**2))*math.exp(-(depth-l['depth'])**2/(2*sigma*sigma)) for l in lights)))

class Semantics(unittest.TestCase):
    def rejects(self,fn,*args):
        with self.assertRaises(ValueError):fn(*args)
    def state(self,n=3):return {'state':'waiting_review','revision':1,'sha':'plan','targets':{i:'proposed' for i in range(n)}}
    def test_review_5000_in_50_batches(self):
        s=self.state(5000)
        for start in range(0,5000,100):
            s,c,left=review_batch(s,str(s['revision']),'plan',[(i,'approve') for i in range(start,start+100)])
            self.assertIsNotNone(c);self.assertEqual(s['state'],'waiting_review')
            self.assertEqual(left,5000-start-100)
        self.assertEqual(close_review(s,str(s['revision']),'plan')['state'],'completed')
    def test_review_reject_only_no_child(self):
        s,c,left=review_batch(self.state(),'1','plan',[(0,'reject')]);self.assertIsNone(c);self.assertEqual(left,2)
    def test_review_cas_conflict(self):
        s,_,_=review_batch(self.state(),'1','plan',[(0,'approve')]);self.rejects(review_batch,s,'1','plan',[(1,'approve')])
    def test_review_redecision_rejected(self):
        s,_,_=review_batch(self.state(),'1','plan',[(0,'approve')]);self.rejects(review_batch,s,'2','plan',[(0,'reject')])
    def test_review_duplicate_atomic(self):
        s=self.state();before=deepcopy(s);self.rejects(review_batch,s,'1','plan',[(0,'approve'),(0,'reject')]);self.assertEqual(s,before)
    def test_review_close_unresolved_rejected(self):self.rejects(close_review,self.state(),'1','plan')
    def test_review_cancelled_no_new_child(self):
        s=self.state();s['state']='cancelled';self.rejects(review_batch,s,'1','plan',[(0,'approve')])
    def test_review_wrong_plan(self):self.rejects(review_batch,self.state(),'1','other',[(0,'approve')])
    def test_review_101_rejected(self):self.rejects(review_batch,self.state(101),'1','plan',[(i,'approve') for i in range(101)])
    def delivery(self):
        snap=['a','b'];pres=[{'id':'required','sha':'s1','required':True},{'id':'optional','sha':'s2','required':False}]
        slots=[{'ordinal':i,'binding':b,'preset_id':p['id'],'preset_sha':p['sha'],'required':p['required'],'digest':'d','state':'ready','export':f'{i}-{p["id"]}','export_id':f'{i}-{p["id"]}','reason':None} for i,b in enumerate(snap) for p in pres]
        return snap,pres,slots,'d',True
    def test_delivery_exact_matrix(self):self.assertEqual(validate_delivery(*self.delivery()),(4,0))
    def test_delivery_missing_pair(self):
        a=list(self.delivery());a[2].pop();self.rejects(validate_delivery,*a)
    def test_delivery_duplicate_pair(self):
        a=list(self.delivery());a[2][1]=a[2][0];self.rejects(validate_delivery,*a)
    def test_delivery_preset_changed(self):
        a=list(self.delivery());a[2][0]['preset_sha']='new';self.rejects(validate_delivery,*a)
    def test_delivery_wrong_source(self):
        a=list(self.delivery());a[2][0]['binding']='foreign';self.rejects(validate_delivery,*a)
    def test_delivery_required_omission(self):
        a=list(self.delivery());a[2][0].update(state='omitted',export=None,export_id=None,reason='fail');self.rejects(validate_delivery,*a)
    def test_delivery_optional_omission(self):
        a=list(self.delivery());a[2][1].update(state='omitted',export=None,export_id=None,reason='fail');self.assertEqual(validate_delivery(*a),(3,1))
        a[-1]=False;self.rejects(validate_delivery,*a)
    def test_delivery_80000_sharding(self):
        pairs=[(i,p) for i in range(5000) for p in range(16)]
        shards=[pairs[i:i+500] for i in range(0,len(pairs),500)]
        self.assertEqual(len(shards),160);self.assertEqual(len({x for shard in shards for x in shard}),80000)
    def training(self):
        available=set('abcde');examples=[]
        for i in range(48):examples.append({'id':i,'lineage':i,'session':i//8,'split':'holdout' if i//8<2 else 'train','binding':'src'+str(i),'approval_binding':'src'+str(i),'refs':dict(zip(('working_image','base_recipe','approved_recipe','protected','faces'),'abcde'))})
        return examples,available
    def test_training_complete(self):validate_training(*self.training())
    def test_training_missing_render_input(self):
        a,b=self.training();b.remove('c');self.rejects(validate_training,a,b)
    def test_training_missing_faces(self):
        a,b=self.training();del a[0]['refs']['faces'];self.rejects(validate_training,a,b)
    def test_training_source_approval_mismatch(self):
        a,b=self.training();a[0]['approval_binding']='other';self.rejects(validate_training,a,b)
    def test_training_session_leakage(self):
        a,b=self.training();a[0]['session']=3;self.rejects(validate_training,a,b)
    def test_training_lineage_leakage(self):
        a,b=self.training();a[20]['lineage']=a[0]['lineage'];self.rejects(validate_training,a,b)
    def test_training_too_small(self):
        a,b=self.training();self.rejects(validate_training,a[:39],b)
    def test_learning_human_cannot_override_failure(self):
        self.assertEqual(eligibility(True,'fail',True),'rejected');self.assertEqual(eligibility(False,'not_measurable',True),'rejected')
    def test_learning_not_measurable_review(self):
        self.assertEqual(eligibility(True,'not_measurable'),'needs_review');self.assertEqual(eligibility(True,'not_measurable',True),'eligible')
    def test_learning_revoke_dominates_review(self):self.assertEqual(eligibility(True,'pass',True,True),'disabled')
    def test_export_bundle_role(self):check_output('v2.interchange.export',[('document',100,'application/json'),('bundle',4*1024**3,'application/zip')],5*1024**3)
    def test_archive_cannot_use_json_role(self):self.rejects(check_output,'v2.interchange.export',[('document',100,'application/zip')],1000)
    def test_output_reservation_stricter_than_policy(self):self.rejects(check_output,'v2.interchange.export',[('bundle',1000,'application/zip')],999)
    def test_output_role_multiplicity(self):self.rejects(check_output,'v2.interchange.export',[('bundle',1,'application/zip')]*2,1000)
    def test_closure_nested_pixels(self):self.assertEqual(check_closure('result',{'result':['image'],'image':['pixels'],'pixels':[]},set(),{'result','image','pixels'}),{'result','image','pixels'})
    def test_closure_foreign_ref(self):self.rejects(check_closure,'result',{'result':['foreign'],'foreign':[]},set(),{'result'})
    def test_closure_orphan(self):self.rejects(check_closure,'result',{'result':[],'orphan':[]},set(),{'result','orphan'})
    def test_closure_cycle(self):self.rejects(check_closure,'result',{'result':['image'],'image':['result']},set(),{'result','image'})
    def test_closure_bypass_reuses_input(self):self.assertEqual(check_closure('result',{'result':['image'],'image':[]},{'image'},{'result'}),{'result','image'})
    def test_path_traversal(self):
        for p in ('../x','/x','a//b','a/./b','C:x','a\\b','a\x00b'):self.rejects(path_ok,p)
        path_ok('images/frame-1.tif')
    def test_revision_overflow_and_zero(self):
        for v in ('9223372036854775808','01','-1','1.0','０'):self.rejects(revision,v)
        self.assertEqual(revision('0',True),0);self.rejects(revision,'0')
    def test_protected_exact(self):self.assertEqual(safe_blend(.23,.9,1,.01,1),.23)
    def test_strength_zero_exact(self):self.assertEqual(safe_blend(.23,.9,1,0,0),.23)
    def test_relight_center_and_cap(self):
        l={'x':.5,'y':.5,'r':.2,'depth':.5,'ev':1};self.assertEqual(relight(.5,.5,.5,[l],.2),2);self.assertEqual(relight(.5,.5,.5,[l]*4,.2),2)
    def test_negative_base_black(self):self.assertEqual(-math.log(.6/.6),0)
    def test_lens_identity(self):
        x,y=.25,.3;r2=x*x+y*y;k=(0,0,0);self.assertEqual(x*(1+k[0]*r2+k[1]*r2*r2+k[2]*r2**3),x)
    def test_no_missing_render_case_in_schema(self):
        s=D['TrainingManifest']['properties']['examples']['items'];self.assertIn('render_case',s['required'])
        optional={'effective_mask','consent_snapshot','region_masks'}
        self.assertEqual(set(D['RenderCase']['required']),set(D['RenderCase']['properties'])-optional)
        for name in ('effective_mask','consent_snapshot'):
            self.assertEqual(D['RenderCase']['properties'][name],{'$ref':'#/$defs/ArtifactRef'})
        self.assertEqual(D['RenderCase']['properties']['region_masks']['maxItems'],128)
    def test_review_and_learning_routes(self):
        es=json.loads((R/'contracts/endpoints.json').read_text())['endpoints'];handlers={e['handler']:e for e in es}
        for name in ('CloseSemanticReview','DecideImport','CloseImportReview','ReviewLearningSafety'):self.assertIn(name,handlers)
        self.assertEqual(handlers['ApplyApproved']['response_schema'],'ReviewBatchResult')
    def test_identity_dependency_before_learning(self):self.assertEqual(OPS['identity.compare']['package'],'V23')
    def test_import_parser_before_resolver(self):
        self.assertEqual(OPS['v2.interchange.parse']['output'],'ParsedInterchange')
        self.assertIn('parsed',D[OPS['v2.interchange.import']['params']]['required'])
    def test_all_policy_contracts_known(self):
        external={'WorkingImage','PixelArray','MaskRef','MaskArray','DenseWarpArray','InterchangeArchive','ImportedFile','QualifiedExport','C2PAManifest','RegionEncodedDerivative'}
        for o in OPS.values():
            for policy in o['role_policies'].values():self.assertIn(policy['contract'],set(D)|external)
    def test_runtime_manifest_sharding(self):
        for n in (1,512,513,50000,1000000):
            chunks=math.ceil(n/512);self.assertLessEqual(chunks,1954)
            self.assertLessEqual(chunks*160,8388608)
        self.assertEqual(OPS['provenance.sign']['resource_class'],'cpu')
        self.assertEqual(OPS['provenance.sign']['execution_pool'],'signer')
    def test_runtime_task_scoped_refs_persisted(self):
        db=json.loads((R/'contracts/database.json').read_text())
        table=next(t for t in db['tables'] if t['name']=='v2_task_inputs')
        self.assertFalse(table['mutable']);self.assertEqual(table['migration'],196)
        self.assertIn('UNIQUE (tenant_id,task_id,artifact_id)',table['constraints'])
    def test_delivery_preparation_before_children(self):
        self.assertEqual(OPS['v2.delivery.prepare']['output'],'DeliveryPreparation')
        self.assertIn('input',D['DeliveryPreparation']['required'])
    def test_artifact_snapshot_before_ingest(self):
        self.assertIn('ArtifactSnapshot',D)
        self.assertNotIn('asset_version_id',D['ArtifactSnapshot']['properties']['items']['items']['required'])
    def test_schema_nullable_capture_and_donor(self):
        self.assertIn({'type':'null'},D['SessionCreate']['properties']['capture_policy']['anyOf'])
        self.assertIn({'type':'null'},D['DonorConfirmation']['properties']['donor']['anyOf'])
    def test_required_algorithm_docs(self):
        for o in OPS.values():self.assertTrue((R/o['algorithm_spec']).is_file())

if __name__=='__main__':
    suite=unittest.defaultTestLoader.loadTestsFromTestCase(Semantics)
    names=[]
    def flatten(s):
        for t in s:
            if isinstance(t,unittest.TestSuite):flatten(t)
            else:names.append(t.id().split('.')[-1])
    flatten(suite)
    result=unittest.TextTestRunner(verbosity=1).run(suite)
    report={'scope':'Executable documentation reference rules; not application or PostgreSQL verification','tests_run':result.testsRun,'failures':len(result.failures),'errors':len(result.errors),'result':'passed' if result.wasSuccessful() else 'failed','tests':names}
    (R/'contracts/semantic-validation-report.json').write_text(json.dumps(report,indent=2)+'\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
