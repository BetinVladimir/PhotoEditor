import {expect,it} from 'vitest';
import registry from '../../../packages/contracts/v2-endpoints.json';
import schemas from '../../../docs/implementation-v2/contracts/schemas.json';
import openapi from '../../../docs/implementation-v2/contracts/openapi.json';
const root='/api/v1/tenants/{t}/projects/{id}';
it('declares whole Lens admission, profile discovery and exact selected proposal API',()=>{
 const expected=[
  ['GET','/lens-profiles',null,'LensCatalogPage',200],
  ['POST','/lens-profiles','ArtifactRef','LensCatalogEntry',200],
  ['POST','/lens-intents','LensIntent','JobTicket',202],
  ['GET','/lens-intents/{run}',null,'LensStatusPage',200],
  ['GET','/versions/{version}/lens-proposals/{task}',null,'LensProposal',200],
  ['GET','/versions/{version}/lens-proposals/{task}/preview',null,'LensPreview',200],
  ['POST','/versions/{version}/lens-proposals/{task}/apply','LensApplyDecision','PerspectiveApplied',200],
 ] as const;
 for(const [method,path,request,response,status] of expected){
  const matches=registry.endpoints.filter(entry=>entry.method===method&&entry.path===root+path);
  expect(matches).toHaveLength(1);expect(matches[0]).toMatchObject({request_schema:request,response_schema:response,success_status:status,role:'editor',package:'V25'});
 }
});
it('distinguishes restoration preparation from admitted queued task contracts',()=>{
 for(const family of ['open-eyes','reflection']){
  const prepare=registry.endpoints.find(entry=>entry.path===`${root}/${family}-preparations`&&entry.method==='POST');
  const submit=registry.endpoints.find(entry=>entry.path===`${root}/${family}-intents`&&entry.method==='POST');
  expect(prepare).toMatchObject({success_status:200,role:'editor'});
  expect(submit).toMatchObject({success_status:202,response_schema:'RestorationAdmissionTicket',role:'editor'});
 }
});

it('resolves every new producer request/response definition and exact OpenAPI operation',()=>{
 const definitions=schemas.$defs as Record<string,unknown>;
 type Operation = {operationId:string;requestBody?:{content:Record<string,{schema:{$ref:string}}>};responses:Record<string,{content?:Record<string,{schema:{$ref:string}}>}>};
 const paths=openapi.paths as unknown as Record<string,Record<string,Operation>>;
 const selected=registry.endpoints.filter(entry=>/\/(lens-(?:profiles|intents)|open-eyes-(?:source-candidates|donor-candidates|mask-pairs|preparations|intents)|reflection-(?:mask-pairs|preparations|intents))|\/lens-proposals\//.test(entry.path));
 for(const entry of selected){
  if(entry.request_schema)expect(definitions[entry.request_schema],entry.request_schema).toBeDefined();
  expect(definitions[entry.response_schema!],entry.response_schema!).toBeDefined();
  const operation=paths[entry.path]?.[entry.method.toLowerCase()];expect(operation?.operationId).toBe(entry.handler);
  expect(operation.responses[String(entry.success_status)].content?.['application/json'].schema.$ref).toBe('schemas.json#/$defs/'+entry.response_schema);
  if(entry.request_schema)expect(operation.requestBody?.content['application/json'].schema.$ref).toBe('schemas.json#/$defs/'+entry.request_schema);
 }
});
it('retains strict response nullability and real shared restoration mask field names',()=>{
 const defs=schemas.$defs as unknown as Record<string,{required?:string[];properties?:Record<string,unknown>;additionalProperties?:boolean}>;
 expect(defs.LensTaskStatus.required).toContain('result_id');expect(defs.LensTaskStatus.required).toContain('result');
 expect(defs.GlareMaskPair.required).toEqual(['glasses_mask','protected','target_pixel','protected_pixel']);
 expect(defs.OpenEyesClientPreparation.required).toContain('params_proposal');
 expect(defs.ReflectionClientIntent.required).toContain('params_proposal');
 expect(defs.LensApplyDecision.additionalProperties).toBe(false);
});

it('binds remaining editor producer delegates to explicit request/response schemas',()=>{
 const suffixes=['glare-donor-preparations','glare-donor-intents','glare-donor-candidates','glare-source-candidates','glare-mask-pairs','local-portrait-preparations','local-portrait-intents','local-portrait-mask-pairs','negative-capabilities','negative-intents','perspective-intents','editor-effects','editor-effects/{build}'];
 const defs=schemas.$defs as Record<string,unknown>;
 for(const suffix of suffixes){const entries=registry.endpoints.filter(entry=>entry.path===root+'/'+suffix);expect(entries.length).toBeGreaterThan(0);for(const entry of entries){
  if(entry.request_schema)expect(defs[entry.request_schema]).toBeDefined();expect(defs[entry.response_schema!]).toBeDefined();
 }}
});
it('requires embedded training pair-page revision without changing generic page optionality',()=>{
 const defs=schemas.$defs as unknown as Record<string,{required?:string[]}>;expect(defs.TrainingSet.required).toContain('pairs');expect(defs.TrainingPairPage.required).toContain('revision');
});

it('expresses exact positive PostgreSQL int64 revision bounds without number coercion',()=>{
 const definition=(schemas.$defs as unknown as Record<string,{pattern:string}>).PositiveInt64Revision;const bound=new RegExp(definition.pattern);
 for(const value of ['1','9007199254740992','9223372036854775807'])expect(bound.test(value)).toBe(true);
 for(const value of ['0','01','-1','9223372036854775808','9999999999999999999','10000000000000000000'])expect(bound.test(value)).toBe(false);
});

it('keeps detected evidence and photographer-authored line publication distinct',()=>{
 const defs=schemas.$defs as unknown as Record<string,{properties?:Record<string,{items?:unknown;maxItems?:number}>;oneOf?:unknown[]}>;
 expect(defs.PerspectiveLineSegments.properties?.lines.maxItems).toBe(1000);
 expect(defs.DetectedPerspectiveLines.oneOf).toHaveLength(2);
 for(const suffix of ['/perspective-lines','/perspective-line-detections','/versions/{version}/perspective-line-detections/{task}'])expect(registry.endpoints.some(entry=>entry.path===root+suffix)).toBe(true);
});

it('records current read permissions and actual shared review DTOs',()=>{
 const expected=[['/training-sets/{id}','editor','TrainingSet'],['/reviews/{id}/previews/{side}/{variant}','editor','ReviewPreview'],['/reviews/{id}/selected-preview/{variant}','editor','ReviewPreview'],['/reviews/{id}/render-result-selection','editor','RenderResultSelection'],['/reviews/{id}/result-selection','editor','NegativeResultSelection'],['/semantic-intents/{id}/review','editor','SemanticReviewStatus'],['/interchange-jobs/{id}/inspection','viewer','InterchangeInspection']];
 for(const [suffix,role,response] of expected)expect(registry.endpoints.find(entry=>entry.path==='/api/v1/tenants/{t}'+suffix&&entry.method==='GET')).toMatchObject({role,response_schema:response,success_status:200});
});

it('records actual draft stream/history DTOs and optional nullable editor bridge',()=>{
 const definitions=schemas.$defs as Record<string,any>;
 for(const method of ['GET','PUT'])expect(registry.endpoints.find(entry=>entry.method===method&&entry.path==='/api/v1/tenants/{t}/assets/{id}/v2-draft')).toMatchObject({response_schema:'V2DraftResource'});
 expect(registry.endpoints.find(entry=>entry.method==='GET'&&entry.path==='/api/v1/tenants/{t}/assets/{id}/v2-draft/history')).toMatchObject({response_schema:'V2DraftHistory'});
 expect(definitions.DraftReplace.required).not.toContain('editor_bridge');expect(definitions.DraftReplace.properties.editor_bridge.anyOf).toContainEqual({type:'null'});
 expect(definitions.EditorRecipeBridgeRequest.required).toEqual(expect.arrayContaining(['revision','document_id','params']));
 expect(definitions.V2DraftHistory.required).toEqual(expect.arrayContaining(['items','next_cursor']));
});

it('documents exact legacy producer responses including source-bound masks and abandonment union',()=>{
 const definitions=schemas.$defs as Record<string,any>,paths=openapi.paths as unknown as Record<string,any>;
 expect(definitions.MaskAccepted.required).toEqual(expect.arrayContaining(['run_id','status','source_sha256']));expect(definitions.MaskAccepted.additionalProperties).toBe(false);
 expect(definitions.Override.required).toEqual(expect.arrayContaining(['asset_version_id','input_sha256','revision','config','config_hash']));
 for(const [path,status,response] of [['masks','202','MaskAccepted'],['previews','202','RunAccepted'],['generations','202','RunAccepted'],['overrides','201','Override'],['generations/abandon','200','GenerationAbandonment']])expect(paths[`/api/v1/tenants/{t}/${path}`].post.responses[status].content['application/json'].schema.$ref).toBe(`schemas.json#/$defs/${response}`);
 expect(paths['/api/v1/tenants/{t}/generations/abandon'].post.responses['202'].content['application/json'].schema.$ref).toBe('schemas.json#/$defs/RunAccepted');
});

it('includes mounted restoration and metadata delegates with actual Editor authority',()=>{
 for(const [method,path,response] of [['GET','reviews/{id}/restoration-result-selection','RestorationResultSelection'],['GET','reviews/{id}/restoration-selected-preview/{variant}','ReviewPreview'],['PATCH','export-presets/{preset}/metadata-policy','ExportPresetResource']]){
  const entries=registry.endpoints.filter(entry=>entry.method===method&&entry.path===`/api/v1/tenants/{t}/${path}`);expect(entries).toHaveLength(1);expect(entries[0]).toMatchObject({role:'editor',response_schema:response,success_status:200});
 }
 const defs=schemas.$defs as Record<string,any>;expect(defs.CullingPage.required).toEqual(expect.arrayContaining(['next_cursor','selected_item_ids','confirmed_eyes_item_ids','snapshot_digest']));expect(defs.CullingPage.properties.next_cursor.anyOf).toContainEqual({type:'null'});expect(defs.CullingDecision.properties.never_delete).toEqual({const:true});expect(defs.RestorationResultSelection.$ref).toBe('#/$defs/NegativeResultSelection');
});
