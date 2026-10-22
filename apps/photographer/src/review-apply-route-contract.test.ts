import sharedEndpoints from '../../../packages/contracts/endpoints.json';
import {expect,it} from 'vitest';
import endpoints from '../../../packages/contracts/v2-endpoints.json';
import normative from '../../../docs/implementation-v2/contracts/endpoints.json';
import openapi from '../../../docs/implementation-v2/contracts/openapi.json';
import schemas from '../../../docs/implementation-v2/contracts/schemas.json';
import {routeCapabilities} from './routes';
it.each([['relight','Relight'],['open-eyes','OpenEyes'],['glare','Glare'],['local-portrait','LocalPortrait']])('traces %s selected Apply from review route through its immutable contract', (slug,name)=>{
 const path=`/api/v1/tenants/{t}/reviews/{id}/${slug}-apply`;
 expect(routeCapabilities.find(route=>route.route==='/reviews/[id]')?.api).toContain(`POST ${path}`);
 const requestName=name==='LocalPortrait'?'LocalPortraitReviewApply':name+'ApplyRequest';
 const contract={method:'POST',path,role:'editor',success_status:200,request_schema:requestName,response_schema:name+'AppliedVersion'};
 for(const registry of [sharedEndpoints,endpoints,normative])expect(registry.endpoints.filter(row=>row.method==='POST'&&row.path===path)).toEqual([expect.objectContaining(contract)]);
 const operation=(openapi.paths as Record<string,any>)[path].post;
 expect(operation['x-web-mutation-csrf']).toBe(true);expect(operation['x-role']).toBe('editor');
 expect(operation.requestBody.content['application/json'].schema.$ref).toBe('schemas.json#/$defs/'+requestName);
 const request=(schemas.$defs as Record<string,any>)[requestName];
 expect(request.additionalProperties).toBe(false);expect(request.required).toEqual(['decision_hash']);expect(Object.keys(request.properties)).toEqual(['decision_hash']);
 expect(operation.responses['200'].content['application/json'].schema.$ref).toBe('schemas.json#/$defs/'+name+'AppliedVersion');
});

it.each([
 ['GET','local-portrait-result',null,'LocalPortraitResult'],
 ['POST','local-portrait-apply','LocalPortraitResultApply','LocalPortraitAutoAppliedVersion'],
])('traces localportrait auto-result %s through all registries', (method,slug,request,response)=>{
 const path=`/api/v1/tenants/{t}/v2-workflows/{id}/${slug}`;
 expect(routeCapabilities.find(route=>route.route==='/runs/[id]')?.api).toContain(`${method} ${path}`);
 for(const registry of [sharedEndpoints,endpoints,normative]){
  expect(registry.endpoints.filter(row=>row.method===method&&row.path===path)).toEqual([expect.objectContaining({request_schema:request,response_schema:response,role:'editor',success_status:200})]);
 }
 const operation=(openapi.paths as Record<string,any>)[path][method.toLowerCase()];
 expect(operation.responses['200'].content['application/json'].schema.$ref).toBe('schemas.json#/$defs/'+response);
 if(request){
  expect(operation['x-web-mutation-csrf']).toBe(true);
  expect(operation.requestBody.content['application/json'].schema.$ref).toBe('schemas.json#/$defs/'+request);
  const body=(schemas.$defs as Record<string,any>)[request];
  expect(body.additionalProperties).toBe(false);expect(body.required).toEqual(['result_id','candidate_sha']);
 }
});
