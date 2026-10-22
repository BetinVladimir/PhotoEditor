import {expect,it} from 'vitest';
import endpoints from '../../../packages/contracts/v2-endpoints.json';
import normative from '../../../docs/implementation-v2/contracts/endpoints.json';
import openapi from '../../../docs/implementation-v2/contracts/openapi.json';
import schemas from '../../../docs/implementation-v2/contracts/schemas.json';

type JSONValue=null|boolean|number|string|JSONValue[]|{[key:string]:JSONValue};
function pointer(document:unknown,path:string):unknown{
 let current=document;
 for(const part of path.slice(2).split('/')){
  if(!current||typeof current!=='object')return undefined;
  current=(current as Record<string,unknown>)[part.replace(/~1/g,'/').replace(/~0/g,'~')];
 }
 return current;
}
it('projects current endpoints to OpenAPI with exact success DTO and media type',()=>{
 expect(endpoints).toEqual(normative);
 for(const endpoint of endpoints.endpoints){
  type Operation={operationId:string;'x-role':string;responses:Record<string,{content?:Record<string,{schema:{$ref:string}}>}>};
  const paths=openapi.paths as unknown as Record<string,Record<string,Operation>>;
  const operation=paths[endpoint.path]?.[endpoint.method.toLowerCase()];
  expect(operation?.operationId).toBe(endpoint.handler);
  if(endpoint.role==='capability')continue;
  expect(operation['x-role']).toBe(endpoint.role);
  if(endpoint.response_schema){
   const media=('response_media_type' in endpoint?endpoint.response_media_type:'application/json') as string;
   const actualRef=operation.responses[String(endpoint.success_status)].content?.[media].schema.$ref.replace(/^\.\//,'');
   const rootSchema=pointer(schemas,`#/$defs/${endpoint.response_schema}`) as {oneOf?:Array<{$ref?:string}>}|undefined;
   // Registry unions cover every resolution status; OpenAPI narrows each wire branch.
   const statusRefs=[`schemas.json#/$defs/${endpoint.response_schema}`,...(rootSchema?.oneOf??[]).flatMap(branch=>branch.$ref?.startsWith('#/')?[`schemas.json${branch.$ref}`]:[])];
   expect(statusRefs).toContain(actualRef);
   expect(pointer(schemas,`#/$defs/${endpoint.response_schema}`)).toBeDefined();
  }
 }
});
it('resolves every local schema reference including embedded defect/dust definitions',()=>{
 function visit(value:JSONValue){
  if(Array.isArray(value)){value.forEach(visit);return;}
  if(!value||typeof value!=='object')return;
  if(typeof value.$ref==='string'&&value.$ref.startsWith('#/'))expect(pointer(schemas,value.$ref),value.$ref).toBeDefined();
  Object.values(value).forEach(visit);
 }
 visit(schemas as unknown as JSONValue);
});

it('keeps ContactSheet abandonment and accepted replay status-specific',()=>{
 const op=openapi.paths['/api/v1/tenants/{t}/contact-sheets/abandon'].post;
 expect(op.responses['200'].content['application/json'].schema.$ref).toBe('schemas.json#/$defs/ContactSheetAbandoned');
 expect(op.responses['202'].content['application/json'].schema.$ref).toBe('schemas.json#/$defs/ContactSheetAccepted');
});

it('keeps Bulk abandonment and accepted replay status-specific',()=>{
 const op=openapi.paths['/api/v1/tenants/{t}/collections/{id}/bulk-operations/abandon'].post;
 expect(op.responses['200'].content['application/json'].schema.$ref).toBe('schemas.json#/$defs/CollectionBulkAbandoned');
 expect(op.responses['202'].content['application/json'].schema.$ref).toBe('schemas.json#/$defs/CollectionBulkAcceptedResolution');
});

it('keeps Share and SaveEdits resolution statuses bound to their real wire branches',()=>{
 const paths=openapi.paths as unknown as Record<string,{post:{responses:Record<string,{content:Record<string,{schema:{$ref:string}}>}>}}> ;
 for(const [path,codes] of [
  ['/api/v1/tenants/{t}/collections/{id}/shares/abandon',{'200':'ShareCreationAbandoned','201':'CollectionShareCreated'}],
  ['/api/v1/tenants/{t}/collections/{id}/save-edits',{'200':'CollectionSaveEditsResolution','202':'CollectionApplyAccepted'}],
 ] as const){
  for(const [status,schema] of Object.entries(codes))expect(paths[path].post.responses[status].content['application/json'].schema.$ref).toBe(`schemas.json#/$defs/${schema}`);
 }
});
