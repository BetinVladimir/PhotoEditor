"""Normative v2.2 contract completion. Imported by build-spec.py; docs only."""
def extend(D, ops, h):
 obj,arr,enum,string,number,integer,ref,null=[h[x] for x in ('obj','arr','enum','string','number','integer','ref','null')]
 U,SHA,REV,T,B,N=[h[x] for x in ('U','SHA','REV','T','B','N')]
 def add_fields(name, fields):
  D[name]['properties'].update(fields);D[name]['required']=list(D[name]['properties'])
 def params(opid,fields,replace=False):
  o=next(o for o in ops if o['id']==opid)
  if replace:D[o['params']]=obj({'schema_version':{'const':1},**fields})
  else:add_fields(o['params'],fields)
  return o
 D['FrozenRecipe']=obj({'schema_version':{'const':1},'source':ref('VersionBinding'),'base_style':ref('ArtifactRef'),'effective_delta':ref('CorrectionVector'),'renderer_version':{'const':'v2.2'},'icc_lock_sha':SHA,'model_lock_sha':SHA})
 D['FrozenRenderLearning']=obj({'version_id':U,'model':ref('ArtifactRef'),'descriptor':ref('ArtifactRef'),'analysis_id':U,'ordinal':integer(0,4999)})
 D['FrozenRenderRecipe']=obj({'schema_version':{'const':1},'recipe':ref('EditingRecipeV3'),'recipe_sha256':SHA,'scalars':ref('FrozenRecipe'),'learning':null(ref('FrozenRenderLearning'))})
 add_fields('EditingRecipeV3',{'parameter_delta':ref('CorrectionVector')})
 D['EditingRecipeV3']['properties']['renderer_version']={'const':'v2.2'}
 D['HoldoutRegionMask']=obj({'region_id':string(120),'mask':ref('ArtifactRef')})
 D['RenderCase']=obj({'binding':ref('VersionBinding'),'working_image':ref('ArtifactRef'),'base_recipe':ref('ArtifactRef'),'approved_recipe':ref('ArtifactRef'),'base_document':ref('ArtifactRef'),'approved_document':ref('ArtifactRef'),'descriptor_shard':ref('ArtifactRef'),'descriptor_analysis_id':U,'descriptor_ordinal':integer(0,4999),'protected':ref('ArtifactRef'),'faces':ref('ArtifactRef'),'effective_mask':ref('ArtifactRef'),'consent_snapshot':ref('ArtifactRef'),'region_masks':arr(ref('HoldoutRegionMask'),0,128),'approval_id':U,'source_lineage_sha':SHA},optional=('effective_mask','consent_snapshot','region_masks'))
 add_fields('TrainingManifest',{'baseline':ref('ArtifactRef'),'renderer_version':{'const':'v2.2'},'icc_lock_sha':SHA,'model_lock_sha':SHA,'calibration':ref('ArtifactRef')})
 ex=D['TrainingManifest']['properties']['examples']['items']
 ex['properties']['render_case']=ref('RenderCase');ex['required'].append('render_case')
 add_fields('Evaluation',{'metric_eligible':B,'safety_status':enum('pass','fail','not_measurable'),'cases':arr(obj({'event_id':U,'color_delta':number(-100,100),'clipping_delta':number(-1,1),'identity_status':enum('pass','fail','not_measurable','no_person'),'protected_max_error':number(0,1e6)}),10,5000)})
 D['LearningSafetyReview']=obj({'expected_revision':REV,'evaluation_sha':SHA,'accept':B,'reason':string(256)})
 params('learning.evaluate',{'model_lock_sha':SHA,'icc_lock_sha':SHA,'renderer_version':{'const':'v2.2'}})
 # Identity primitive must exist before learning evaluation, not only V28.
 next(o for o in ops if o['id']=='identity.compare')['package']='V23'
 D['ChildTicket']=obj({'run_id':U,'state':enum('queued')})
 D['ReviewBatchResult']=obj({'workflow_id':U,'revision':REV,'remaining':integer(0,5000),'child':null(ref('ChildTicket')),'batch_id':U})
 add_fields('SemanticApply',{'expected_revision':REV})
 D['ReviewClose']=obj({'expected_revision':REV,'plan_sha':SHA})
 D['ImportReview']=obj({'expected_revision':REV,'report_sha':SHA,'decisions':arr(obj({'entry_id':U,'decision':enum('approve','reject')}),1,100)})
 add_fields('AnalysisApply',{'expected_revision':REV})
 D['SceneBoardDocument']=obj({'session_id':U,'analysis_id':U,'snapshot_id':U,'snapshot_digest':SHA,'policy_sha':SHA,'plan_sha':SHA,'plan':ref('ScenePlan'),'frames':arr(obj({'ordinal':integer(0,4999),'version_id':U,'source_sha':SHA}),1,5000)})
 D['FrozenPreset']=obj({'preset_id':U,'preset_sha':SHA,'config':ref('ArtifactRef'),'required':B})
 D['DeliverySlot']=obj({'ordinal':integer(0,4999),'binding':ref('VersionBinding'),'preset_id':U,'preset_sha':SHA,'required':B,'state':enum('ready','omitted','failed'),'export_id':null(U),'export':null(ref('ArtifactRef')),'reason':null(string(200))})
 D['DeliverySlotShard']=obj({'schema_version':{'const':1},'snapshot_digest':SHA,'items':arr(ref('DeliverySlot'),1,500)})
 D['DeliveryAssemblyInput']=obj({'schema_version':{'const':1},'snapshot_digest':SHA,'item_count':integer(1,5000),'slot_count':integer(1,80000),'policy':ref('DeliveryPolicy'),'presets':arr(ref('FrozenPreset'),1,16),'shards':arr(ref('ArtifactRef'),1,160)})
 D['DeliveryManifest']=obj({'schema_version':{'const':1},'assembly_sha':SHA,'snapshot_digest':SHA,'slot_count':integer(1,80000),'ready_count':integer(1,80000),'omitted_count':integer(0,80000),'shards':arr(ref('ArtifactRef'),1,160)})
 params('v2.delivery.assemble',{'assembly':ref('ArtifactRef')},True)
 D['MappingEntry']=obj({'entry_id':U,'external_id':string(240),'asset_version_id':null(U),'proposed_document':null(ref('ArtifactRef')),'fields':arr(obj({'name':string(80),'status':enum('exact','unsupported','rejected'),'reason':null(string(200))}),0,128)})
 D['MappingShard']=obj({'schema_version':{'const':1},'entries':arr(ref('MappingEntry'),1,100)})
 D['MappingReport']=obj({'schema_version':{'const':1},'entry_count':integer(0,5000),'shards':arr(ref('ArtifactRef'),0,50),'rejected_count':integer(0,5000),'unsupported_count':integer(0,5000)})
 D['InterchangeEntry']=obj({'entry_id':U,'external_id':string(240),'path':string(240),'content':ref('ArtifactRef'),'binding':ref('VersionBinding'),'recipe':ref('ArtifactRef'),'rating':integer(-1,5),'pick':enum('picked','rejected','unmarked'),'crop':null(ref('Rect'))})
 D['InterchangePlan']=obj({'schema_version':{'const':1},'project_id':U,'direction':enum('import','export'),'format':enum('xmp','photoeditor_bundle','captureone_sidecar'),'snapshot_digest':SHA,'entries':arr(ref('InterchangeEntry'),1,5000),'max_archive_bytes':integer(1,68719476736),'max_expanded_bytes':integer(1,137438953472),'max_files':integer(1,30000)})
 D['InterchangeResult']=obj({'schema_version':{'const':1},'report':ref('ArtifactRef'),'bundle':null(ref('ArtifactRef')),'draft_shards':arr(ref('ArtifactRef'),0,50)})
 params('v2.interchange.export',{'plan':ref('ArtifactRef')},True)
 params('v2.interchange.import',{'request':ref('InterchangeRequest'),'max_expanded_bytes':integer(1,137438953472),'max_files':integer(1,30000)},True)
 D['ImportReviewClose']=obj({'expected_revision':REV,'report_sha':SHA})
 D['FaceObservation']['properties']['face_id']=U;D['FaceObservation']['required'].append('face_id')
 params('identity.compare',{'consents':ref('ArtifactRef')})
 params('provenance.sign',{'policy':ref('ArtifactRef')})
 params('optical.panorama',{'calibration':ref('ArtifactRef')})
 params('optical.focus_stack',{'calibration':ref('ArtifactRef')})
 D['TokenCreate']['properties']['signature']={'type':'string','pattern':'^[A-Za-z0-9_-]{86}$'}
 D['Challenge']['properties']['nonce']={'type':'string','pattern':'^[A-Za-z0-9_-]{43}$'}
 # All operation reports and auxiliary documents have typed payloads.
 D['FaceObservation']['properties']['landmarks']=arr(obj({'x':N,'y':N}),68,68)
 D['FaceObservation']['properties']['embedding']=null(arr(number(-1,1),512,512))
 D['ConsentSnapshot']=obj({'schema_version':{'const':1},'grants':arr(obj({'consent_id':U,'epoch':REV,'person_id':null(U),'purpose':enum('identity_matching','learning_photographer','learning_person')}),0,5000)})
 params('session.describe',{'consents':ref('ArtifactRef')})
 D['ReferenceChoice']=obj({'previous_analysis_id':U,'group_id':U,'asset_version_id':U})
 D['AnalysisCreate']['properties']['manual_references']=arr(ref('ReferenceChoice'),0,5000)
 D['SessionPlanContext']=obj({'schema_version':{'const':1},'snapshot_id':U,'snapshot_digest':SHA,'manual_references':arr(obj({'asset_version_id':U,'previous_members':arr(U,1,5000)}),0,5000),'manual_locked_ordinals':arr(integer(0,4999),0,5000)})
 params('session.plan',{'context':ref('ArtifactRef')})
 D['CaptureContext']=obj({'schema_version':{'const':1},'session_id':U,'confirmed_person_ids':arr(U,0,100),'shot_list':ref('ArtifactRef'),'recent_content_shas':arr(SHA,0,5000),'consents':ref('ArtifactRef')})
 params('capture.assess',{'context':ref('ArtifactRef')})
 add_fields('SessionCreate',{'capture_policy':ref('CapturePolicy')})
 add_fields('SessionUpdate',{'capture_policy':ref('CapturePolicy')})
 add_fields('Calibration',{'focus_warning':N,'focus_critical':N,'eye_open_min':N,'glare_warning_fraction':N,'clipping_warning_fraction':N,'clipping_critical_fraction':N,'min_face_edge_px':integer(32,512),'depth_sigma':number(.01,1),'registration_error_px':number(.01,10),'motion_threshold':number(.001,1)})
 D['DraftReplace']['properties']['expected_revision']={'type':'string','pattern':'^(0|[1-9][0-9]{0,18})$'}
 D['DraftApprovalResult']=obj({'id':U,'revision':REV,'document_id':U,'approval_id':U})
 D['ShotListView']=obj({'session_id':U,'revision':REV,'document':null(ref('ArtifactRef')),'requirements':arr(ref('ShotRequirement'),0,200)})
 D['LearningVersionView']=obj({'id':U,'revision':REV,'profile_id':U,'version_no':REV,'evaluation':ref('ArtifactRef'),'eligibility':enum('eligible','rejected','needs_review','disabled'),'review_id':null(U)})
 D['LearningVersionPage']=obj({'items':arr(ref('LearningVersionView'),0,100),'next_cursor':null(string(2048))})
 D['ResolvedTransform']=obj({'schema_version':{'const':1},'from_canvas_hash':SHA,'to_canvas_hash':SHA,'matrix':arr(number(-1e8,1e8),9,9),'kind':enum('homography','similarity')})
 D['DonorConfirmation']=obj({'schema_version':{'const':1},'person_id':U,'source':ref('VersionBinding'),'donor':ref('VersionBinding'),'source_face_id':U,'donor_face_id':U,'source_observation_id':U,'donor_observation_id':U,'consent_id':U,'epoch':REV,'actor_id':U})
 params('portrait.open_eyes',{'source_faces':ref('ArtifactRef'),'donor_faces':ref('ArtifactRef')})
 params('portrait.glare',{'source_faces':ref('ArtifactRef'),'donor_faces':null(ref('ArtifactRef'))})
 params('portrait.hair',{'protected':ref('ArtifactRef')})
 D['LensProfile']=obj({'schema_version':{'const':1},'camera_model':string(120),'lens_model':string(120),'focal_mm':number(1,2000),'aperture':number(.5,128),'cx':N,'cy':N,'k':arr(number(-2,2),3,3),'p':arr(number(-.1,.1),2,2),'vignette':arr(number(-2,2),3,3),'ca_red_scale':number(.99,1.01),'ca_blue_scale':number(.99,1.01),'qualification':ref('ArtifactRef')})
 D['NegativeCalibration']=obj({'schema_version':{'const':1},'input_icc_sha':SHA,'density_matrix':arr(arr(number(-10,10),3,3),3,3),'density_offset':arr(number(-10,10),3,3),'density_range':arr(number(.001,10),3,3),'qualification':ref('ArtifactRef')})
 D['LineSegments']=obj({'schema_version':{'const':1},'lines':arr(obj({'a':obj({'x':N,'y':N}),'b':obj({'x':N,'y':N}),'family':enum('horizontal','vertical')}),4,1000)})
 D['ModelFamilySpec']=obj({'schema_version':{'const':1},'family':enum('scene384','face68','identity512','eye_open','glare_mask','reflection_rgb','inpaint_rgb'),'adapter_version':{'const':'onnx_v2_1'},'input_size':integer(32,2048),'model_lock_sha':SHA,'model_id':string(120),'runtime_lock_sha':SHA,'calibration':ref('ArtifactRef'),'preprocess':{'const':'srgb_nchw_float32_0_1'},'code_license':string(200),'weights_license':string(200),'qualification':ref('ArtifactRef')})
 D['InputSnapshot']=obj({'schema_version':{'const':1},'snapshot_id':U,'snapshot_digest':SHA,'items':arr(obj({'ordinal':integer(0,4999),'binding':ref('VersionBinding'),'working_image':ref('ArtifactRef'),'captured_at':null(T),'camera_hash':null(SHA),'recipe':ref('ArtifactRef')}),1,5000)})
 D['SigningLineage']=obj({'schema_version':{'const':1},'source_hashes':arr(SHA,1,5000),'actions':arr(obj({'operation':string(80),'version':string(20),'params_sha':SHA,'model_lock_sha':null(SHA),'generated':B,'review_id':null(U)}),0,5000)})
 D['SigningPolicy']=obj({'schema_version':{'const':1},'id':U,'revision':REV,'sdk_lock_sha':SHA,'key_handle':string(200),'certificate_chain_sha':SHA,'timestamp_mode':enum('off','required'),'timestamp_service_id':null(string(120)),'allowed_mime':arr(enum('image/jpeg','image/png'),1,2)})
 # Completion discovered by semantic walkthroughs.
 for n in ('SessionCreate','SessionUpdate'):
  D[n]['properties']['capture_policy']=null(ref('CapturePolicy'))
 D['RowShootSessions']['properties']['capture_policy']=null(ref('CapturePolicy'))
 for field in ('donor','donor_face_id','donor_observation_id'):
  D['DonorConfirmation']['properties'][field]=null(D['DonorConfirmation']['properties'][field])
 add_fields('CaptureContext',{'coverage':arr(obj({'requirement_id':U,'confirmed_frames':integer(0,5000)}),0,200)})
 params('image.relight',{'calibration':ref('ArtifactRef')})
 D['ResourceBudget']=obj({k:integer(0,9007199254740991) for k in ('host_bytes','gpu_bytes','scratch_bytes','output_bytes','cost_seconds')})
 D['CapacityPolicy']=obj({'schema_version':{'const':1},**{k:integer(1,9007199254740991) for k in ('gpu_concurrency','cpu_concurrency','cpu_threads_per_task','heavy_admission_inflight','tenant_heavy_queued','gpu_budget_bytes','photo_ram_budget_bytes','control_ram_budget_bytes','tile_edge','heartbeat_seconds','offline_seconds','lease_seconds','renew_seconds','unknown_cost_seconds')},'scratch_free_floor_fraction':number(.05,.5),'scratch_multiplier':number(1,4),'queue_weights':obj({k:integer(1,100) for k in ('interactive','capture','batch','training')}),'max_retries_after_oom':integer(0,1)})
 D['QualificationReport']=obj({'schema_version':{'const':1},'operation':string(80),'version':string(20),'model_lock_sha':SHA,'runtime_lock_sha':SHA,'device_profile_sha':SHA,'dataset_sha':SHA,'input_fixture_shas':arr(SHA,1,10000),'checks':arr(obj({'id':string(100),'passed':B,'evidence_sha':SHA}),1,1000),'metrics':obj({k:number(0,9007199254740991) for k in ('p50_ms','p95_ms','peak_vram_bytes','peak_rss_bytes','scratch_bytes')}),'status':enum('passed','failed'),'issuer_id':U,'created_at':T})
 D['BundleInventoryEntry']=obj({'artifact_id':U,'path':string(240),'sha256':SHA,'contract':string(120)})
 D['BundleEntry']=obj({'entry_id':U,'external_id':string(240),'image_path':string(240),'image_sha256':SHA,'recipe_path':null(string(240)),'recipe_sha256':null(SHA),'rating':integer(-1,5),'pick':enum('picked','rejected','unmarked'),'crop':null(ref('Rect'))})
 D['BundleManifest']=obj({'schema_version':{'const':1},'renderer_version':{'const':'v2.2'},'entries':arr(ref('BundleEntry'),1,5000),'masks':arr(obj({'path':string(240),'sha256':SHA,'canvas_hash':SHA}),0,5000),'inventory':arr(ref('BundleInventoryEntry'),1,30000)})
 D['ParsedFile']=obj({'path':string(240),'content':ref('ArtifactRef'),'contract':string(120)})
 D['ParsedInterchange']=obj({'schema_version':{'const':1},'manifest':ref('BundleManifest'),'files':arr(ref('ParsedFile'),1,30000)})
 # Parsing before existing canonical ingest; resolving after it avoids future refs.
 h['op']('v2.interchange.parse','V30',{'request':ref('InterchangeRequest'),'max_expanded_bytes':integer(1,137438953472),'max_files':integer(1,30000)},'ParsedInterchange')
 params('v2.interchange.import',{'parsed':ref('ArtifactRef'),'plan':ref('ArtifactRef')},True)
 add_fields('InterchangeResult',{'imported_files':arr(ref('ArtifactRef'),0,30000)})
 for opid in ('v2.interchange.import','v2.interchange.export'):
  params(opid,{'max_report_bytes':{'const':8388608}})
 D['ArtifactSnapshot']=obj({'schema_version':{'const':1},'snapshot_id':U,'snapshot_digest':SHA,'project_id':U,'items':arr(obj({'ordinal':integer(0,4999),'artifact':ref('ArtifactRef'),'contract':enum('InterchangeArchive','QualifiedExport','TrainingManifest'),'owner_id':null(U)}),1,5000)})
 D['WorkflowSnapshot']={'oneOf':[ref('InputSnapshot'),ref('ArtifactSnapshot')]}
 add_fields('ConsentSnapshot',{'confirmed_faces':arr(obj({'asset_version_id':U,'face_id':U,'person_id':U,'consent_id':U,'epoch':REV}),0,12800)})
 for o in ops:
  if o['id']=='learning.evaluate':o['resource_class']='gpu';o['timeout_seconds']=1800
 D['InputClosureChunk']=obj({'schema_version':{'const':1},'entries':arr(obj({'ref':ref('ArtifactRef'),'contract':string(120)}),1,512)})
 D['InputClosureManifest']=obj({'schema_version':{'const':1},'task_id':U,'entry_count':integer(1,1000000),'entries_sha':SHA,'chunks':arr(ref('ArtifactRef'),1,1954)})
 for o in ops:
  o['execution_pool']='signer' if o['id']=='provenance.sign' else o['resource_class']
  if o['id']=='provenance.sign':o['resource_class']='cpu'
 D['FrozenDeliveryInput']=obj({'schema_version':{'const':1},'snapshot':ref('ArtifactRef'),'snapshot_digest':SHA,'policy':ref('DeliveryPolicy'),'presets':arr(ref('FrozenPreset'),1,16)})
 D['DeliveryPreparation']=obj({'schema_version':{'const':1},'input':ref('ArtifactRef'),'snapshot_digest':SHA,'item_count':integer(1,5000),'slot_count':integer(1,80000)})
 h['op']('v2.delivery.prepare','V30',{'input':ref('ArtifactRef')},'DeliveryPreparation')
 ops[-1]['execution_pool']='cpu'
 D['RegisteredTransform']=obj({'schema_version':{'const':1},'source':ref('VersionBinding'),'to_canvas_hash':SHA,'homography':arr(number(-1e8,1e8),9,9),'inverse_field':null(ref('ArtifactRef'))})
 add_fields('ImageReport',{'registrations':arr(ref('ArtifactRef'),0,30)})
 D['ImageReport']['properties']['parent_reports']=arr(ref('ArtifactRef'),0,33)
 add_fields('SigningPolicy',{'trust_bundle_sha':SHA,'revocation_mode':enum('required','off')})
 # RegionPreview uses a frozen typed Params plan and a document envelope.
 D['RegionGeometry']=obj({'left':integer(0,200000000),'top':integer(0,200000000),'width':integer(1,200000000),'height':integer(1,200000000),'output_width':integer(1,1024),'output_height':integer(1,1024)})
 D['RegionPreviewDescriptor']=obj({'schema_version':{'const':1},'asset_id':U,'request':ref('RegionPreview'),'source':ref('ArtifactRef'),'canvas':obj({'width':integer(1,200000000),'height':integer(1,200000000)}),'params_sha256':SHA,'icc_sha256':SHA,'alpha':B})
 D['RegionObjectPin']=obj({'ref':ref('ArtifactRef'),'bucket':string(255),'object_key':string(1024),'object_version':string(1024),'bytes':integer(2,1048576)})
 D['RegionInput']=obj({'ref':ref('ArtifactRef'),'contract':enum('WorkingImage','FrozenRecipe','StyleV2','NpyArray')})
 D['RegionDerivativeManifest']=obj({'schema_version':{'const':1},'params_sha256':SHA,'asset_version_id':U,'source':ref('ArtifactRef'),'recipe':ref('ArtifactRef'),'rect':ref('Rect'),'geometry':ref('RegionGeometry'),'derivative':ref('ArtifactRef'),'bytes':integer(2,16777216),'icc_sha256':SHA,'cache_key':SHA,'media_type':enum('image/jpeg','image/webp'),'alpha':B,'metadata_policy':{'const':'stripped'},'quality':{'const':88}})
 h['op']('region.preview','V26',{'tenant_id':U,'project_id':U,'asset_id':U,'request':ref('RegionPreview'),'source':ref('RegionObjectPin'),'recipe':ref('RegionObjectPin'),'frozen_recipe':ref('FrozenRecipe'),'geometry':ref('RegionGeometry'),'render_order':{'const':'full_canvas_then_crop_then_downscale'},'inputs':arr(ref('RegionInput'),4,133)},'RegionDerivativeManifest','gpu')
 ops[-1]['execution_pool']='gpu'
 # Root output role document always stores the operation's envelope.
 def role(contract,count=1,required=True,bytecap=8388608,mime=None):
  return {'contract':contract,'max_count':count,'required':required,'max_bytes':bytecap,'media_types':mime or ['application/json']}
 for o in ops:
  roles={'document':role(o['output'])}
  if o['output']=='ImageResult':
   roles.update(working_image=role('WorkingImage'),pixels=role('PixelArray',bytecap=2400004096,mime=['application/x-npy']),report=role('ImageReport'),mask=role('MaskRef',2),mask_pixels=role('MaskArray',2,bytecap=800004096,mime=['application/x-npy']),transform=role('ResolvedTransform',required=False))
  if o['output']=='ImageResult':
   roles['registration']=role('RegisteredTransform',30,False)
   roles['alpha']=role('MaskArray',required=False,bytecap=800004096,mime=['application/x-npy'])
   roles['warp_field']=role('DenseWarpArray',32,False,1600004096,['application/x-npy'])
  if o['id']=='session.describe':roles['faces']=role('FaceObservations',128)
  if o['id']=='session.plan':roles['correction_plan']=role('CorrectionPlan',5000)
  if o['id']=='semantic.transfer':roles.update(mask=role('MaskRef',required=False),mask_pixels=role('MaskArray',required=False,bytecap=800004096,mime=['application/x-npy']),transform=role('ResolvedTransform',required=False))
  if o['id']=='identity.compare':roles.update(mask=role('MaskRef'),mask_pixels=role('MaskArray',bytecap=800004096,mime=['application/x-npy']))
  if o['id'] in ('v2.interchange.import','v2.interchange.export'):
   roles.update(report=role('MappingReport'),mapping_shard=role('MappingShard',50,False),draft=role('EditingRecipeV3',5000,False))
   if o['id'].endswith('export'):roles['bundle']=role('InterchangeArchive',bytecap=68719476736,mime=['application/zip'])
   # Imported files are declared inputs from the parse stage.
  if o['id']=='v2.interchange.parse':roles['imported_file']=role('ImportedFile',30000,False,68719476736,['image/jpeg','image/png','image/tiff','application/octet-stream','application/json','application/rdf+xml','application/x-npy'])
  if o['id']=='provenance.sign':roles.update(signed_export=role('QualifiedExport',bytecap=2400004096,mime=['image/jpeg','image/png']),manifest=role('C2PAManifest',mime=['application/c2pa']))
  if o['id']=='region.preview':
   roles['derivative']=role('RegionEncodedDerivative',bytecap=16777216,mime=['image/jpeg','image/webp'])
  if o['id']=='layer.compose':
   roles['working_image']['required']=False
   roles['pixels']['required']=False
  o['role_policies']=roles;o['output_roles']=list(roles);o['max_output_count']=sum(x['max_count'] for x in roles.values())
  o['max_output_bytes']=sum(x['max_count']*x['max_bytes'] for x in roles.values())
  o['envelope_schema']=o['output'];o['nested_outputs']='Every output ref resolves to declared input or a role-declared staged member; validate full transitive closure; reservation is a stricter total cap.'
  o['algorithm_spec']='16-imaging-algorithms.md' if o['output']=='ImageResult' else '15-execution-contracts.md'
 return D,ops

def endpoints(E,endpoint):
 endpoint('GET','/assets/{id}/region-previews/{run}/descriptor',None,'RegionPreviewDescriptor','V26','GetRegionPreviewDescriptor','editor')
 endpoint('GET','/assets/{id}/v2-layers/{artifact}',None,'LayerStack','V26','GetLayerStack','editor')
 endpoint('GET','/shoot-sessions/{id}/analyses',None,'ResourcePage','V26','ListSessionAnalyses','viewer')
 endpoint('GET','/session-analyses/{id}/scene-board',None,'SceneBoardDocument','V26','GetSceneBoard','viewer')
 for e in E:
  if e['handler']=='ApplyApproved':e['response_schema']='ReviewBatchResult';e['success_status']=200
  if e['handler']=='ApproveDraft':e['response_schema']='DraftApprovalResult'
  if e['handler'] in ('ReplaceShotList','GetShotList'):e['response_schema']='ShotListView'
  if e['handler']=='ListVersions':e['response_schema']='LearningVersionPage'
 endpoint('POST','/semantic-intents/{id}/close-review','ReviewClose','JobStatus','V27','CloseSemanticReview')
 endpoint('POST','/interchange-jobs/{id}/decisions','ImportReview','ReviewBatchResult','V30','DecideImport')
 endpoint('POST','/interchange-jobs/{id}/close-review','ImportReviewClose','JobStatus','V30','CloseImportReview')
 endpoint('POST','/learning-versions/{id}/safety-review','LearningSafetyReview','LearningVersionView','V23','ReviewLearningSafety','profile_owner')
