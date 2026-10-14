import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
export type RenderWorkflowPolicy={schema_version:1;renderer_version:'v2.2';workflow_type:'preview';child_failure_policy:'fail_fast';max_frames:5000};
export type RenderPolicySnapshot={policy_sha:string;policy:RenderWorkflowPolicy};
function fields(v:unknown,keys:string[]):v is Record<string,unknown>{return !!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));}
export function readRenderPolicy(v:unknown):RenderPolicySnapshot{
 if(!fields(v,['policy_sha','policy'])||typeof v.policy_sha!=='string'||!/^[0-9a-f]{64}$/.test(v.policy_sha)||!fields(v.policy,['schema_version','renderer_version','workflow_type','child_failure_policy','max_frames']))throw new Error('RENDER_POLICY_INVALID');
 const p=v.policy;
 if(p.schema_version!==1||p.renderer_version!=='v2.2'||p.workflow_type!=='preview'||p.child_failure_policy!=='fail_fast'||p.max_frames!==5000)throw new Error('RENDER_POLICY_UNSUPPORTED');
 if(bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(p))))!==v.policy_sha)throw new Error('RENDER_POLICY_DIGEST_INVALID');
 return {policy_sha:v.policy_sha,policy:{schema_version:1,renderer_version:'v2.2',workflow_type:'preview',child_failure_policy:'fail_fast',max_frames:5000}};
}
