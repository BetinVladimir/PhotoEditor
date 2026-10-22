import type {RunAccepted} from './catalog-types';
export function readGenerationResolution(value:unknown,key:string):{kind:'abandoned';receipt:{status:'abandoned';key:string}}|{kind:'accepted';receipt:RunAccepted}{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('GENERATION_RESOLUTION_INVALID');
 const record=value as Record<string,unknown>,keys=Object.keys(record).sort().join(',');
 if(keys==='key,status'&&record.status==='abandoned'&&record.key===key)return {kind:'abandoned',receipt:{status:'abandoned',key}};
 if(keys==='run_id,status'&&typeof record.run_id==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(record.run_id)&&record.run_id!=='00000000-0000-0000-0000-000000000000'&&typeof record.status==='string'&&/^[a-z_]{1,64}$/.test(record.status))return {kind:'accepted',receipt:{run_id:record.run_id,status:record.status}};
 throw new Error('GENERATION_RESOLUTION_INVALID');
}
