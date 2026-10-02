"""Generate documentation contracts only. Never touches production source or database."""
from pathlib import Path
import json
ROOT=Path(__file__).resolve().parent
C=ROOT/'contracts'; C.mkdir(exist_ok=True)
def write(name,obj):
 (C/name).write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n')
def obj(props,optional=()):return {'type':'object','additionalProperties':False,'properties':props,'required':[k for k in props if k not in optional]}
def arr(items,lo=0,hi=5000):return {'type':'array','items':items,'minItems':lo,'maxItems':hi}
def enum(*v):return {'type':'string','enum':list(v)}
def string(n=200):return {'type':'string','minLength':1,'maxLength':n}
def number(a,b):return {'type':'number','minimum':a,'maximum':b}
def integer(a,b):return {'type':'integer','minimum':a,'maximum':b}
def ref(n):return {'$ref':'#/$defs/'+n}
def null(s):return {'anyOf':[s,{'type':'null'}]}
U={'type':'string','format':'uuid'};SHA={'type':'string','pattern':'^[0-9a-f]{64}$'};REV={'type':'string','pattern':'^[1-9][0-9]{0,18}$'};T={'type':'string','format':'date-time'};B={'type':'boolean'};N=number(0,1)
D={}
D['ArtifactRef']=obj({'artifact_id':U,'sha256':SHA})
D['VersionBinding']=obj({'asset_version_id':U,'source':ref('ArtifactRef'),'canvas_hash':SHA})
D['Error']=obj({'code':string(80),'message_key':string(120),'args':{'type':'object','additionalProperties':{'type':['string','number','boolean','null']}},'request_id':U})
D['JobTicket']=obj({'run_id':U,'workflow_id':U,'state':enum('queued')})
D['Resource']=obj({'id':U,'revision':REV,'document':null(ref('ArtifactRef'))})
D['ResourcePage']=obj({'items':arr(ref('Resource'),0,100),'next_cursor':null(string(2048))})
D['JobStatus']=obj({'run_id':U,'workflow_id':U,'revision':REV,'state':enum('queued','running','waiting_review','cancelling','cancelled','completed','failed'),'outputs':arr(ref('ArtifactRef'),0,128),'error_code':null(string(80))})
D['CAS']=obj({'expected_revision':REV})
D['SessionPolicy']=obj({'schema_version':{'const':1},'time_gap_seconds':integer(30,7200),'neighbor_count':integer(1,64),'protect_manual_picks':{'const':True},'camera_offsets':arr(obj({'camera_hash':SHA,'seconds':integer(-86400,86400)}),0,32),'calibration':ref('ArtifactRef')})
D['Calibration']=obj({'schema_version':{'const':1},'model_lock_sha':SHA,'dataset_sha':SHA,'qualified':B,'scene_cosine_min':number(-1,1),'scene_log_luma_max':number(0,4),'cluster_diameter_max':number(0,2),'sharpness_scale':number(0.000001,1e6),'clipping_penalty':number(0,10),'match_inlier_min':N,'match_error_px_max':number(0,100),'identity_similarity_min':number(-1,1),'landmark_drift_max':N,'normalization_scales':arr(number(0.000001,1e6),6,6),'loss_weights':arr(number(0,1),6,6)})
D['CorrectionVector']=obj({'ev':number(-1,1),'log_r':number(-.223144,.223144),'log_g':number(-.223144,.223144),'log_b':number(-.223144,.223144),'saturation_delta':number(-.15,.15),'retouch_delta':number(-.15,.15)})
D['CorrectionPlan']=obj({'schema_version':{'const':1},'snapshot_digest':SHA,'items':arr(obj({'binding':ref('VersionBinding'),'ordinal':integer(0,4999),'delta':ref('CorrectionVector'),'disposition':enum('apply','no_change','manual_locked'),'reason':null(string(80))}),1,5000)})
D['ScenePlan']=obj({'schema_version':{'const':1},'snapshot_id':U,'snapshot_digest':SHA,'policy_sha':SHA,'groups':arr(obj({'id':U,'ordinals':arr(integer(0,4999),1,5000),'reference_ordinal':integer(0,4999),'confidence':N,'corrections':ref('ArtifactRef')}),1,5000)})
D['EditingRecipeV3']=obj({'schema_version':{'const':3},'source':ref('VersionBinding'),'base_style':ref('ArtifactRef'),'session_corrections':null(ref('ArtifactRef')),'learning_version_id':null(U),'overrides':arr(ref('ArtifactRef'),0,128),'layer_stack':null(ref('ArtifactRef')),'protected_mask':ref('ArtifactRef'),'renderer_version':{'const':'v2.2'},'parameter_delta':ref('CorrectionVector')},optional=('protected_mask',))
D['CorrectionCreate']=obj({'client_event_id':U,'session_id':U,'asset_version_id':U,'base_document_id':U,'approved_document_id':U,'reason':enum('exposure','color','crop','retouch','selection','other'),'learn':B,'person_id':null(U),'scene_group_id':null(U),'consent_id':null(U),'consent_epoch':null(REV)})
D['CorrectionEvent']=obj({**D['CorrectionCreate']['properties'],'id':U,'project_id':U,'approved_by':U,'created_at':T,'changed_paths':arr(enum('/ev','/log_r','/log_g','/log_b','/saturation_delta','/retouch_delta','/crop','/selection'),1,8)})
D['LearningPolicy']=obj({'schema_version':{'const':1},'mode':enum('photographer','person'),'min_examples':{'const':40},'min_sessions':{'const':6},'holdout_fraction':{'const':.2},'seed':{'const':2026},'calibration':ref('ArtifactRef')})
D['TrainingManifest']=obj({'schema_version':{'const':1},'profile_id':U,'mode':enum('photographer','person'),'person_id':null(U),'consents':arr(obj({'consent_id':U,'epoch':REV}),1,5000),'examples':arr(obj({'event_id':U,'session_id':U,'subject_group_id':U,'split':enum('train','holdout'),'features':arr(number(-100,100),12,12),'target':ref('CorrectionVector')}),40,5000),'policy_sha':SHA})
D['CandidateProfile']=obj({'schema_version':{'const':1},'profile_id':U,'training_sha':SHA,'feature_order':{'const':'scene12_v1'},'means':arr(number(-100,100),12,12),'scales':arr(number(1e-8,1e8),12,12),'coefficients':arr(arr(number(-1e6,1e6),6,6),12,12),'intercept':arr(number(-1e6,1e6),6,6),'ridge_lambda':{'enum':[.01,.1,1,10]}})
D['Evaluation']=obj({'schema_version':{'const':1},'candidate_sha':SHA,'manifest_sha':SHA,'holdout_sessions':integer(0,5000),'baseline_loss':number(0,1e9),'candidate_loss':number(0,1e9),'color_p95_delta':number(-100,100),'clipping_delta_fraction':number(-1,1),'safety_regression':B,'eligible':B,'reasons':arr(string(80),0,32)})
D['Rect']=obj({'x0':N,'y0':N,'x1':N,'y1':N})
D['MaskExpr']={'oneOf':[obj({'op':enum('ref'),'mask':ref('ArtifactRef'),'canvas_hash':SHA}),obj({'op':enum('invert'),'operand':ref('MaskExpr')}),obj({'op':enum('union','intersect','subtract'),'left':ref('MaskExpr'),'right':ref('MaskExpr')})]}
D['LayerComposeParams']=obj({'schema_version':{'const':1},'base':ref('VersionBinding'),'source_result':null(ref('ArtifactRef')),'proposal_result':ref('ArtifactRef'),'mask':ref('MaskExpr'),'protected_mask':null(ref('ArtifactRef')),'opacity':N,'blend':enum('normal','multiply','screen')})
D['Layer']=obj({'id':U,'enabled':B,'opacity':N,'blend':enum('normal','multiply','screen'),'operation':string(80),'operation_version':string(20),'params':ref('ArtifactRef'),'mask':ref('MaskExpr')})
D['LayerStack']=obj({'schema_version':{'const':1},'source':ref('VersionBinding'),'layers':arr(ref('Layer'),0,32)})
D['DraftApproval']=obj({'expected_revision':REV,'document_id':U})
D['DraftReplace']=obj({'expected_revision':REV,'recipe':ref('EditingRecipeV3'),'layers':null(ref('LayerStack'))})
D['ConsentChange']=obj({'expected_revision':REV,'purpose':enum('identity_matching','learning_photographer','learning_person'),'person_id':null(U),'action':enum('grant','revoke'),'policy_version':string(80)})
D['ObservationConfirm']=obj({'expected_revision':REV,'asset_version_id':U,'face_observation_id':U,'evidence':ref('ArtifactRef'),'consent_id':U,'consent_epoch':REV})
D['ShotRequirement']=obj({'id':U,'label':string(200),'person_ids':arr(U,0,100),'min_confirmed_frames':integer(1,100),'min_quality':N})
D['ShotListReplace']=obj({'expected_revision':REV,'requirements':arr(ref('ShotRequirement'),0,200)})
D['CapturePolicy']=obj({'schema_version':{'const':1},'checks':arr(enum('focus','eyes','clipping','glare','duplicate','shot_list'),1,6),'max_queue_age_seconds':{'const':30},'calibration':ref('ArtifactRef')})
D['CaptureFrameConfirm']=obj({'alert_id':U})
D['CaptureFrameConfirmation']=obj({'confirmation_id':U,'receipt_id':U,'alert_id':U})
D['CaptureAlertView']=obj({'resource_type':{'const':'capture_alerts'},'id':U,'tenant_id':U,'project_id':U,'created_at':T,'updated_at':T,'revision':REV,'session_id':U,'asset_version_id':U,'report_artifact_id':U,'severity':enum('info','warning','critical'),'receipt_id':null(U),'qa_stale':B,'acknowledged_by':null(U),'acknowledged_at':null(T),'confirmation_id':null(U)})
D['CaptureAlertPage']=obj({'items':arr(ref('CaptureAlertView'),0,100),'next_cursor':null(string(2048))})
D['CaptureReceipt']=obj({'device_id':U,'sequence':{'type':'string','pattern':'^(0|[1-9][0-9]{0,18})$'},'content_sha':SHA,'upload_id':U,'captured_at':null(T)})
D['QAReport']=obj({'schema_version':{'const':1},'binding':ref('VersionBinding'),'checks':arr(obj({'code':string(80),'status':enum('pass','warning','critical','unavailable'),'face_id':null(U),'score':null(N),'threshold':null(number(-1e6,1e6)),'region':null(ref('Rect')),'reason':null(string(80))}),1,600)})
D['IdentityReport']=obj({'schema_version':{'const':1},'status':enum('pass','review','fail','not_measurable'),'faces':arr(obj({'face_id':U,'similarity':null(number(-1,1)),'landmark_drift':null(N)}),0,100),'changed_mask':ref('ArtifactRef'),'generated_mask':null(ref('ArtifactRef')),'outside_mask_max_error':number(0,1e6),'calibration':ref('ArtifactRef')})
D['EditIntent']=obj({'schema_version':{'const':1},'kind':enum('remove_object','local_exposure','background_cleanup','retouch_region'),'reference':ref('VersionBinding'),'reference_mask':ref('ArtifactRef'),'target_snapshot_id':U,'strength':N,'ev':null(number(-1,1)),'preserve_persons':{'const':True},'prompt':null(string(1024))})
D['TransferCandidate']=obj({'schema_version':{'const':1},'target':ref('VersionBinding'),'mask':null(ref('ArtifactRef')),'transform':null(ref('ArtifactRef')),'confidence':N,'state':enum('proposed','unsupported','rejected'),'reason':null(string(80))})
D['SemanticPlan']=obj({'schema_version':{'const':1},'intent_sha':SHA,'snapshot_digest':SHA,'candidates':arr(ref('TransferCandidate'),1,5000),'model_lock_sha':SHA,'calibration':ref('ArtifactRef')})
D['DeliveryPolicy']=obj({'required_presets':arr(U,1,8),'optional_presets':arr(U,0,8),'require_review':B,'metadata_policy':enum('strip','copyright','approved_exif'),'provenance':enum('off','required'),'allow_omitted':B})
D['InterchangeRequest']={'oneOf':[obj({'project_id':U,'direction':enum('import'),'format':enum('xmp','photoeditor_bundle','captureone_sidecar'),'bundle':ref('ArtifactRef')}),obj({'project_id':U,'direction':enum('export'),'format':enum('xmp','photoeditor_bundle','captureone_sidecar'),'snapshot_id':U})]}
D['MappingReport']=obj({'schema_version':{'const':1},'items':arr(obj({'external_id':string(240),'asset_version_id':null(U),'fields':arr(obj({'name':string(80),'status':enum('exact','unsupported','rejected'),'reason':null(string(200))}),0,128)}),0,5000)})
# Explicit endpoint body models.
reqs={
'SessionCreate':{'project_id':U,'collection_id':U,'name':string(),'policy':ref('SessionPolicy')},
'SessionUpdate':{'expected_revision':REV,'name':string(),'state':enum('open','closed','archived'),'policy':ref('SessionPolicy')},
'AnalysisCreate':{'expected_revision':REV,'selection_snapshot_id':U,'policy':ref('SessionPolicy'),'manual_references':arr(obj({'group_id':U,'asset_version_id':U}),0,5000)},
'AnalysisApply':{'plan_sha':SHA,'approved_groups':arr(U,1,5000),'recipe':ref('ArtifactRef')},
'ProfileCreate':{'project_id':U,'name':string(120),'mode':enum('photographer','person'),'person_id':null(U)},
'TrainCreate':{'expected_revision':REV,'session_ids':arr(U,6,5000),'policy':ref('LearningPolicy')},
'Activate':{'expected_revision':REV,'version_id':U,'reason':enum('promote','rollback')},
'PersonCreate':{'project_id':U,'label':string(120)},
'DeviceCreate':{'project_id':U,'label':string(120),'public_key':{'type':'string','pattern':'^[A-Za-z0-9_-]{43}$'}},
'ChallengeCreate':{'device_id':U},'Challenge':{'challenge_id':U,'nonce':string(64),'expires_at':T,'credential_epoch':REV},
'TokenCreate':{'challenge_id':U,'signature':{'type':'string','pattern':'^[A-Za-z0-9_-]{86}$'}},'Token':{'access_token':string(8192),'expires_in':{'const':900}},
'SemanticApply':{'plan_sha':SHA,'target_decisions':arr(obj({'target_id':U,'decision':enum('approve','reject')}),1,100)},
'DeliveryCreate':{'project_id':U,'snapshot_id':U,'name':string(),'policy':ref('DeliveryPolicy')},
'SignCreate':{'signing_policy_id':U},
'RegionPreview':{'version_id':U,'rect':ref('Rect'),'recipe':ref('ArtifactRef'),'max_edge':integer(64,1024)},
'ProfileGet':{'id':U,'revision':REV,'mode':enum('photographer','person'),'person_id':null(U),'active_version_id':null(U),'consent_state':enum('disabled','enabled','revoked')},
}
for n,v in reqs.items():D[n]=obj(v)
# Worker params use ref masks until loaded; loaded MaskRef checked against existing schema.
ops=[]
def op(name,pkg,params,output,resource='cpu'):
 n=''.join(s.title() for s in name.split('.'))+'Params'
 D[n]=obj({'schema_version':{'const':1},**params})
 ops.append({'id':name,'version':'1','package':pkg,'params':n,'output':output,'resource_class':resource,'output_roles':['document'] if output not in ['ImageResult','SignedResult'] else (['working_image','report','changed_mask','generated_mask'] if output=='ImageResult' else ['signed_export','manifest']),'timeout_seconds':1800 if resource=='gpu' else 600,'max_output_bytes':2400000000 if output in ['ImageResult','SignedResult'] else 8388608,'max_output_count':4})
op('layer.compose','V25',{k:v for k,v in D['LayerComposeParams']['properties'].items() if k!='schema_version'},'ImageResult')
D['ImageResult']=obj({'schema_version':{'const':1},'working_image':ref('ArtifactRef'),'report':ref('ArtifactRef'),'changed_mask':ref('ArtifactRef'),'generated_mask':null(ref('ArtifactRef'))})
D['SignedResult']=obj({'schema_version':{'const':1},'signed_export':ref('ArtifactRef'),'manifest':ref('ArtifactRef')})
D['FrameDescriptor']=obj({'schema_version':{'const':1},'binding':ref('VersionBinding'),'ordinal':integer(0,4999),'captured_at':null(T),'camera_hash':null(SHA),'scene_embedding':arr(number(-1,1),384,384),'luma_quantiles':arr(number(0,1),5,5),'neutral_rgb':null(arr(N,3,3)),'sharpness':number(0,1e6),'clipping_fraction':N,'person_fraction':N,'faces':ref('ArtifactRef')})
D['DescriptorShard']=obj({'schema_version':{'const':1},'snapshot_digest':SHA,'frames':arr(ref('FrameDescriptor'),1,128)})
op('session.describe','V22',{'snapshot':ref('ArtifactRef'),'ordinals':arr(integer(0,4999),1,128),'model_lock_sha':SHA,'calibration':ref('ArtifactRef')},'DescriptorShard','gpu')
op('session.plan','V22',{'shards':arr(ref('ArtifactRef'),1,40),'policy':ref('SessionPolicy')},'ScenePlan')
op('learning.fit','V23',{'manifest':ref('ArtifactRef'),'base_profile':ref('ArtifactRef'),'policy':ref('LearningPolicy')},'CandidateProfile')
op('learning.evaluate','V23',{'candidate':ref('ArtifactRef'),'manifest':ref('ArtifactRef'),'baseline':ref('ArtifactRef'),'calibration':ref('ArtifactRef')},'Evaluation')
op('capture.assess','V24',{'binding':ref('VersionBinding'),'proxy':ref('ArtifactRef'),'faces':ref('ArtifactRef'),'policy':ref('CapturePolicy'),'model_lock_sha':SHA},'QAReport','gpu')
op('identity.compare','V28',{'before':ref('VersionBinding'),'after':ref('VersionBinding'),'effective_mask':ref('ArtifactRef'),'protected':ref('ArtifactRef'),'model_lock_sha':SHA,'calibration':ref('ArtifactRef')},'IdentityReport','gpu')
op('semantic.transfer','V27',{'intent':ref('ArtifactRef'),'reference':ref('VersionBinding'),'target':ref('VersionBinding'),'model_lock_sha':SHA,'calibration':ref('ArtifactRef')},'TransferCandidate','gpu')
base={'base':ref('VersionBinding'),'strength':N}
model={'model_lock_sha':SHA,'calibration':ref('ArtifactRef')}
for name,pkg,props,gpu in [
('portrait.open_eyes','V28',{'donor':ref('VersionBinding'),'source_face_id':U,'donor_face_id':U,'eyes_mask':ref('ArtifactRef'),'protected':ref('ArtifactRef'),'confirmation':ref('ArtifactRef')},False),
('portrait.glare','V28',{'donor':null(ref('VersionBinding')),'glasses_mask':ref('ArtifactRef'),'protected':ref('ArtifactRef'),'mode':enum('donor','generative'),'confirmation':ref('ArtifactRef'),**model},True),
('image.reflection','V28',{'mask':ref('ArtifactRef'),'protected':ref('ArtifactRef'),**model},True),
('portrait.hair','V28',{'mask':ref('ArtifactRef'),'hue_delta_degrees':number(-30,30),'saturation_factor':number(.7,1.3),'detail':N},False),
('portrait.clothing','V28',{'mask':ref('ArtifactRef'),'protected':ref('ArtifactRef'),'hue_delta_degrees':number(-180,180),'saturation_factor':number(0,2),'wrinkle_strength':N},False),
('image.relight','V28',{'depth':ref('ArtifactRef'),'subject':ref('ArtifactRef'),'protected':ref('ArtifactRef'),'lights':arr(obj({'x':number(-1,2),'y':number(-1,2),'radius':number(.05,2),'ev':number(-1,1)}),1,4)},False),
('optical.lens','V25',{'profile':ref('ArtifactRef'),'focal_mm':number(1,2000),'aperture':number(.5,128)},False),
('optical.perspective','V29',{'mode':enum('auto','manual'),'corners':null(arr(obj({'x':N,'y':N}),4,4)),'lines':null(ref('ArtifactRef')),'max_crop_fraction':N},False),
('optical.negative','V29',{'film_base_rgb':arr(number(1e-6,1),3,3),'calibration':ref('ArtifactRef'),'exposure_ev':number(-3,3)},False)]:op(name,pkg,{**base,**props},'ImageResult','gpu' if gpu else 'cpu')
op('optical.panorama','V29',{'frames':arr(ref('VersionBinding'),2,20),'reference_ordinal':integer(0,19),'projection':enum('cylindrical','planar'),'max_pixels':integer(1,200000000)},'ImageResult')
op('optical.focus_stack','V29',{'frames':arr(ref('VersionBinding'),2,30),'reference_ordinal':integer(0,29),'motion_policy':enum('review','reject'),'max_pixels':integer(1,200000000)},'ImageResult')
op('provenance.sign','V31',{'export':ref('ArtifactRef'),'lineage':ref('ArtifactRef'),'signing_policy_id':U},'SignedResult','signer')

# Technical CPU orchestration operations have the same fenced result contracts.
D['DeliveryManifest']=obj({'schema_version':{'const':1},'snapshot_digest':SHA,'items':arr(obj({'ordinal':integer(0,4999),'preset_id':U,'export':ref('ArtifactRef'),'required':B}),1,80000)})
D['InterchangeResult']=obj({'schema_version':{'const':1},'report':ref('ArtifactRef'),'bundle':null(ref('ArtifactRef')),'drafts':arr(ref('ArtifactRef'),0,5000)})
op('v2.delivery.assemble','V30',{'policy':ref('DeliveryPolicy'),'exports':arr(ref('ArtifactRef'),1,80000),'snapshot_digest':SHA},'DeliveryManifest')
op('v2.interchange.import','V30',{'request':ref('InterchangeRequest')},'InterchangeResult')
op('v2.interchange.export','V30',{'request':ref('InterchangeRequest'),'snapshot':ref('ArtifactRef')},'InterchangeResult')
op('v2.semantic.assemble','V27',{'intent':ref('ArtifactRef'),'candidates':arr(ref('ArtifactRef'),1,5000),'model_lock_sha':SHA,'calibration':ref('ArtifactRef')},'SemanticPlan')
D['ShotList']=obj({'schema_version':{'const':1},'requirements':arr(ref('ShotRequirement'),0,200)})
# Read models are explicit table projections. Never expose auth or cleanup rows.
db=json.loads((C/'database.json').read_text())
public={'v2_documents','v2_document_streams','shoot_sessions','session_analyses','scene_groups','project_persons','person_observations','consent_grants','learning_versions','capture_devices','capture_receipts','capture_alerts','shot_lists','semantic_targets','delivery_packages','delivery_items','interchange_jobs'}
row_names=[]
for table in db['tables']:
 if table['name'] not in public:continue
 props={'resource_type':{'const':table['name']}}
 for col in table['columns']:
  name=col['name'];ddl=col['ddl']
  if name in ('public_key',):continue
  typ=ddl.split()[0]
  sc=U if typ=='uuid' else T if typ=='timestamptz' else ({'type':'string','pattern':'^(0|[1-9][0-9]{0,18})$'} if name=='sequence' else REV) if typ=='bigint' else integer(0,2147483647) if typ=='integer' else N if typ=='double' else B if typ=='boolean' else ref('SessionPolicy') if name=='policy' else ref('CapturePolicy') if name=='capture_policy' else string(256)
  if col.get('enum'):sc=enum(*col['enum'])
  if col.get('nullable'):sc=null(sc)
  props[name]=sc
 n='Row'+''.join(x.title() for x in table['name'].split('_'));D[n]=obj(props);row_names.append(n)
D['Resource']['properties']['data']={'oneOf':[ref(n) for n in row_names]}
D['Resource']['required'].append('data')
D['DocumentBody']={'anyOf':[ref(n) for n in ['EditingRecipeV3','LayerStack','ShotList']]}


D['FaceObservation']=obj({'observation_id':U,'box':ref('Rect'),'landmarks':ref('ArtifactRef'),'embedding':null(ref('ArtifactRef')),'quality':N,'person_id':null(U)})
D['FaceObservations']=obj({'schema_version':{'const':1},'binding':ref('VersionBinding'),'faces':arr(ref('FaceObservation'),0,100)})
D['ImageReport']=obj({'schema_version':{'const':1},'status':enum('pass','review','fail','unavailable'),'operation':string(80),'skipped_count':integer(0,5000),'reason':null(string(200)),'outside_mask_max_error':number(0,1e6),'clipping_fraction':N,'identity':null(ref('ArtifactRef')),'transform':null(ref('ArtifactRef')),'sources':arr(ref('VersionBinding'),1,30)})
D['EditIntent']['allOf']=[{'if':{'properties':{'kind':{'const':'local_exposure'}}},'then':{'properties':{'ev':number(-1,1)}},'else':{'properties':{'ev':{'type':'null'}}}}]
D['CorrectionCreate']['allOf']=[{'if':{'properties':{'learn':{'const':True}}},'then':{'properties':{'consent_id':U,'consent_epoch':REV}},'else':{'properties':{'consent_id':{'type':'null'},'consent_epoch':{'type':'null'}}}}]
D['CorrectionEvent']['allOf']=D['CorrectionCreate']['allOf']
from spec_v22 import extend, endpoints as extend_endpoints
D,ops=extend(D,ops,globals())

write('schemas.json',{'$schema':'https://json-schema.org/draft/2020-12/schema','$id':'https://photoeditor.invalid/spec/v2/schemas.json','$defs':D})
write('operations.json',{'schema_version':1,'operations':ops})
# Every endpoint has one strict request and response; resource documents are separately schema-checked.
E=[]
def endpoint(method,path,req,response,pkg,handler,role='editor',status=200):
 E.append({'method':method,'path':'/api/v1/tenants/{t}'+path,'request_schema':req,'response_schema':response,'package':pkg,'handler':handler,'role':role,'success_status':status})
endpoint('POST','/shoot-sessions','SessionCreate','Resource','V22','CreateSession',status=201)
endpoint('GET','/shoot-sessions/{id}',None,'Resource','V22','GetSession','viewer')
endpoint('GET','/shoot-sessions',None,'ResourcePage','V22','ListSessions','viewer')
endpoint('PATCH','/shoot-sessions/{id}','SessionUpdate','Resource','V22','UpdateSession')
endpoint('POST','/shoot-sessions/{id}/analyses','AnalysisCreate','JobTicket','V22','AnalyzeSession',status=202)
endpoint('GET','/session-analyses/{id}',None,'Resource','V22','GetAnalysis','viewer')
endpoint('POST','/session-analyses/{id}/apply','AnalysisApply','JobTicket','V22','ApplyAnalysis',status=202)
endpoint('POST','/corrections','CorrectionCreate','CorrectionEvent','V23','RecordCorrection',status=201)
endpoint('POST','/learning-profiles','ProfileCreate','ProfileGet','V23','CreateProfile',status=201)
endpoint('GET','/learning-profiles/{id}',None,'ProfileGet','V23','GetProfile','viewer')
endpoint('POST','/learning-profiles/{id}/train','TrainCreate','JobTicket','V23','TrainProfile',status=202)
endpoint('GET','/learning-profiles/{id}/versions',None,'ResourcePage','V23','ListVersions','viewer')
endpoint('POST','/learning-profiles/{id}/activate','Activate','ProfileGet','V23','ActivateProfile','profile_owner')
endpoint('POST','/projects/{id}/consents','ConsentChange','Resource','V22','ChangeConsent','owner')
endpoint('GET','/projects/{id}/consents',None,'ResourcePage','V22','ListConsents','owner')
endpoint('POST','/project-persons','PersonCreate','Resource','V22','CreatePerson',status=201)
endpoint('GET','/projects/{id}/persons',None,'ResourcePage','V22','ListPersons','editor')
endpoint('POST','/project-persons/{id}/observations','ObservationConfirm','Resource','V22','ConfirmObservation',status=201)
endpoint('POST','/project-persons/{id}/revoke','CAS','Resource','V22','RevokePerson','owner')
endpoint('PUT','/shoot-sessions/{id}/shot-list','ShotListReplace','Resource','V24','ReplaceShotList')
endpoint('GET','/shoot-sessions/{id}/shot-list',None,'Resource','V24','GetShotList','viewer')
endpoint('POST','/capture-devices','DeviceCreate','Resource','V24','RegisterDevice','admin',201)
endpoint('POST','/capture-devices/{id}/revoke','CAS','Resource','V24','RevokeDevice','admin')
endpoint('POST','/capture-devices/{id}/challenges','ChallengeCreate','Challenge','V24','CreateChallenge','device_public')
endpoint('POST','/capture-devices/{id}/token','TokenCreate','Token','V24','ExchangeChallenge','device_public')
endpoint('POST','/shoot-sessions/{id}/capture-receipts','CaptureReceipt','Resource','V24','AcceptReceipt','device',201)
endpoint('GET','/shoot-sessions/{id}/alerts',None,'CaptureAlertPage','V24','ListAlerts','viewer')
endpoint('POST','/capture-receipts/{id}/confirm','CaptureFrameConfirm','CaptureFrameConfirmation','V24','ConfirmFrame')
endpoint('POST','/capture-alerts/{id}/ack','CAS','Resource','V24','Acknowledge')
endpoint('PUT','/assets/{id}/v2-draft','DraftReplace','Resource','V21','ReplaceDraft')
endpoint('GET','/assets/{id}/v2-draft',None,'Resource','V21','GetDraft','viewer')
endpoint('GET','/assets/{id}/v2-binding',None,'VersionBinding','V21','GetDraftBinding')
endpoint('GET','/assets/{id}/v2-draft/history',None,'ResourcePage','V21','ListDraftHistory','viewer')
endpoint('POST','/assets/{id}/v2-draft/approve','DraftApproval','Resource','V21','ApproveDraft')
endpoint('POST','/assets/{id}/region-previews','RegionPreview','JobTicket','V26','PreviewRegion',status=202)
endpoint('POST','/semantic-intents','EditIntent','JobTicket','V27','Propose',status=202)
endpoint('GET','/semantic-intents/{id}/targets',None,'ResourcePage','V27','ListTargets','viewer')
endpoint('POST','/semantic-intents/{id}/apply','SemanticApply','JobTicket','V27','ApplyApproved',status=202)
endpoint('POST','/delivery-packages','DeliveryCreate','JobTicket','V30','CreateDelivery',status=202)
endpoint('GET','/delivery-packages/{id}',None,'Resource','V30','GetDelivery','viewer')
endpoint('POST','/delivery-packages/{id}/publish','CAS','Resource','V30','PublishDelivery')
endpoint('POST','/interchange-jobs','InterchangeRequest','JobTicket','V30','CreateInterchange',status=202)
endpoint('GET','/interchange-jobs/{id}',None,'Resource','V30','GetInterchange','viewer')
endpoint('POST','/exports/{id}/provenance','SignCreate','JobTicket','V31','SignExport',status=202)
endpoint('GET','/v2-documents/{id}',None,'DocumentBody','V21','GetDocument','viewer')
endpoint('GET','/v2-workflows/{id}',None,'JobStatus','V21','GetWorkflow','viewer')
endpoint('POST','/v2-workflows/{id}/cancel','CAS','JobStatus','V21','CancelWorkflow')
extend_endpoints(E,endpoint)
write('endpoints.json',{'schema_version':1,'endpoints':E})
paths={}
for e in E:
 params=[{'name':name,'in':'path','required':True,'schema':U} for name in __import__('re').findall(r'{([^}]+)}',e['path'])]
 if e['handler']=='ListSessions':params += [{'name':'project_id','in':'query','required':True,'schema':U}]
 if e['method']=='GET' and e['response_schema'] in ('ResourcePage','LearningVersionPage','CaptureAlertPage'):params += [{'name':'limit','in':'query','schema':integer(1,100)},{'name':'cursor','in':'query','schema':string(2048)}]
 public=e['role']=='device_public'
 if e['method']!='GET' and not public:params += [{'name':'Idempotency-Key','in':'header','required':True,'schema':string(128)}]
 auth=[] if public else [{'deviceBearer':[]}] if e['role']=='device' else [{'userBearer':[]}]
 operation={'operationId':e['handler'],'x-role':e['role'],'x-package':e['package'],'parameters':params,'security':auth,'x-web-mutation-csrf':e['method']!='GET' and not public and e['role']!='device','responses':{str(e['success_status']):{'description':'Success','content':{'application/json':{'schema':{'$ref':'schemas.json#/$defs/'+e['response_schema']}}}},**{str(x):{'description':'Domain error','content':{'application/json':{'schema':{'$ref':'schemas.json#/$defs/Error'}}}} for x in [400,401,403,404,409,410,413,422,428,429,503]}}}
 if e['request_schema']:operation['requestBody']={'required':True,'content':{'application/json':{'schema':{'$ref':'schemas.json#/$defs/'+e['request_schema']}}}}
 paths.setdefault(e['path'],{})[e['method'].lower()]=operation
write('openapi.json',{'openapi':'3.1.0','info':{'title':'PhotoEditor v2 additions','version':'2.2.0'},'paths':paths,'components':{'securitySchemes':{'userBearer':{'type':'http','scheme':'bearer'},'deviceBearer':{'type':'http','scheme':'bearer'},'sessionCookie':{'type':'apiKey','in':'cookie','name':'__Host-photo_refresh'},'csrfHeader':{'type':'apiKey','in':'header','name':'X-CSRF-Token'}}}})
print(len(D),'schemas',len(E),'endpoints',len(ops),'operations')
