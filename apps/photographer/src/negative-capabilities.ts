import type {ArtifactRef} from './catalog-types';
export type NegativeCapabilities={schema_version:1;tenant_id:string;project_id:string;calibration:ArtifactRef;max_pixels:number};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));}
export function readNegativeCapabilities(v:unknown,tenant:string,project:string):NegativeCapabilities {
 const fail=():never=>{throw new Error('NEGATIVE_CAPABILITIES_INVALID')};
 if(!uuid(tenant)||!uuid(project)||!fields(v,['schema_version','tenant_id','project_id','calibration','max_pixels'])||v.schema_version!==1||v.tenant_id!==tenant||v.project_id!==project||!fields(v.calibration,['artifact_id','sha256'])||!uuid(v.calibration.artifact_id)||typeof v.calibration.sha256!=='string'||!/^[0-9a-f]{64}$/.test(v.calibration.sha256)||typeof v.max_pixels!=='number'||!Number.isSafeInteger(v.max_pixels)||v.max_pixels<1||v.max_pixels>200000000)return fail();
 return {schema_version:1,tenant_id:tenant,project_id:project,calibration:{artifact_id:v.calibration.artifact_id,sha256:v.calibration.sha256},max_pixels:v.max_pixels};
}
