import {describe,it,expect,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {photographerRoutes} from './routes';
import {globalRoutes,roleAllows,routeRolePolicies,scopedRoute,workspaceScope,readWorkspaceAuthority} from './workspace-access';
const tenant='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222';
describe('current workspace authority boundary',()=>{
 it('covers every current route while keeping global authenticated screens tenant-independent',()=>{
  expect(Object.keys(routeRolePolicies).sort()).toEqual([...photographerRoutes].sort());
  for(const route of globalRoutes)expect(workspaceScope(route,{tenant:['untrusted']})).toBeNull();
  expect(scopedRoute(['collections','[id]','index'])).toBe('/collections/[id]');
  expect(()=>scopedRoute(['unknown'])).toThrow();
 });
 it('refuses malformed, zero and repeated route identifiers before tenant IO',()=>{
  for(const value of [undefined,'tenant','00000000-0000-0000-0000-000000000000',[tenant],tenant.toUpperCase()+'X'])expect(()=>workspaceScope('/projects',{tenant:value})).toThrow();
  for(const key of ['project','id','asset','version','original','collection','item','analysis','export','session','snapshot','style'])expect(()=>workspaceScope('/collections/[id]',{tenant,[key]:[project]})).toThrow();
  expect(workspaceScope('/projects/[id]',{tenant,id:project})).toEqual({tenant,project});
 });
 it('never elevates unknown roles or trusts another tenant authority/revision',()=>{
  expect(roleAllows('viewer','editor')).toBe(false);expect(roleAllows('editor','admin')).toBe(false);expect(roleAllows('owner','admin')).toBe(true);
  for(const role of ['administrator','constructor',undefined])expect(roleAllows(role,'viewer')).toBe(false);
  for(const row of [{id:project,role:'owner',revision:'1'},{id:tenant,role:'unknown',revision:'1'},{id:tenant,role:'owner',revision:'0'},{id:tenant,role:'owner',revision:'9223372036854775808'}])expect(()=>readWorkspaceAuthority(row,tenant)).toThrow();
  expect(readWorkspaceAuthority({id:tenant,role:'viewer',revision:'1'},tenant).role).toBe('viewer');
 });
 it('allows intentional global OwnerAPI but refuses malformed tenant requests before transport',async()=>{
  const request=vi.fn().mockResolvedValue({id:project,email:'owner@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'});
  const api=new OwnerAPI({request} as unknown as ApiClient,'');
  await api.me();expect(request.mock.calls[0][0]).toBe('/api/v1/me');request.mockClear();
  await expect(api.tenantSettings()).rejects.toThrow();expect(request).not.toHaveBeenCalled();
  request.mockResolvedValue({id:tenant,name:'Studio',role:'owner',revision:'1',quotas:{storage_bytes:1048576,source_files:1,max_active_gpu_tasks:1},retention:{processing_days:7,exports_days:30,temporary_hours:24}});
  const scoped=new OwnerAPI({request} as unknown as ApiClient,tenant);await scoped.tenantSettings();expect(request.mock.calls[0][0]).toBe(`/api/v1/tenants/${tenant}`);
 });
});

it('requires every dynamic path identifier before authorizing a tenant route',()=>{
 for(const route of photographerRoutes){
  if(globalRoutes.has(route))continue;
  const names=[...route.matchAll(/\[([a-zA-Z]+)\]/g)].map(match=>match[1]);
  const complete:Record<string,unknown>={tenant,asset:project};
  for(const name of names)complete[name]=project;
  expect(()=>workspaceScope(route,complete)).not.toThrow();
  for(const missing of names){const incomplete={...complete};delete incomplete[missing];expect(()=>workspaceScope(route,incomplete)).toThrow('WORKSPACE_SCOPE_INVALID');}
 }
});
