import type {TenantSettings} from './catalog-types';
import {readWorkspaceTenant} from './workspace-response';

function policy(raw:unknown,bounds:Record<string,readonly[number,number]>):Record<string,number>{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))throw Error('TENANT_SETTINGS_INVALID');const v=raw as Record<string,unknown>;
 if(Object.keys(v).sort().join(',')!==Object.keys(bounds).sort().join(',')||Object.entries(bounds).some(([key,[min,max]])=>!Number.isSafeInteger(v[key])||Number(v[key])<min||Number(v[key])>max))throw Error('TENANT_SETTINGS_INVALID');
 return {...v} as Record<string,number>;
}
function storedPolicy(raw:unknown,bounds:Record<string,readonly[number,number]>,historical:Record<string,number>,defaults:Record<string,number>):Record<string,number>{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))throw Error('TENANT_SETTINGS_INVALID');
 const source=raw as Record<string,unknown>,projected:Record<string,unknown>={};let legacy=false;
 for(const key of Object.keys(bounds))if(Object.prototype.hasOwnProperty.call(source,key))projected[key]=source[key];
 for(const [key,min] of Object.entries(historical))if(Object.prototype.hasOwnProperty.call(source,key)){
  if(!Number.isSafeInteger(source[key])||Number(source[key])<min)throw Error('TENANT_SETTINGS_INVALID');
  legacy=true;
 }
 // Historical retention fields are not equivalent to current processing or
 // export retention. Fill only documented modern defaults, never reinterpret
 // old days or turn the inert active_gpu_tasks metadata into an admission cap.
 return policy(legacy?{...defaults,...projected}:projected,bounds);
}
export function readTenantSettings(raw:unknown,tenant:string):TenantSettings{
 if(!raw||typeof raw!=='object'||Array.isArray(raw))throw Error('TENANT_SETTINGS_INVALID');const v=raw as Record<string,unknown>;
 // These are the actual tenant.validQuotas/validRetention limits. In
 // particular, storage's maximum 2^50 is exactly representable JSON numeric
 // data; no rounded int64-to-number conversion is accepted.
 const quotas=storedPolicy(v.quotas,{storage_bytes:[1048576,2**50],source_files:[1,1000000],max_active_gpu_tasks:[0,16]},{active_runs:0,active_gpu_tasks:0},{max_active_gpu_tasks:1});
 const retention=storedPolicy(v.retention,{processing_days:[1,365],exports_days:[1,365],temporary_hours:[1,168]},{deleted_days:0,artifact_days:0},{processing_days:7,exports_days:30,temporary_hours:24});
 const identity=readWorkspaceTenant({...v,quotas,retention},{id:tenant});
 return {...identity,quotas:quotas as TenantSettings['quotas'],retention:retention as TenantSettings['retention']};
}
export function readTenantSettingsUpdate(raw:unknown,tenant:string,before:TenantSettings,patch:Partial<Pick<TenantSettings,'name'|'quotas'|'retention'>>):TenantSettings{
 const row=readTenantSettings(raw,tenant),expected={...before,...patch};
 if(BigInt(row.revision)!==BigInt(before.revision)+1n||row.name!==expected.name||Object.entries(expected.quotas).some(([key,value])=>row.quotas[key as keyof TenantSettings['quotas']]!==value)||Object.entries(expected.retention).some(([key,value])=>row.retention[key as keyof TenantSettings['retention']]!==value))throw Error('TENANT_SETTINGS_RECEIPT_INVALID');
 return row;
}
