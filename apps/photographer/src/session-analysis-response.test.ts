import {expect,it} from 'vitest';
import {readSessionAnalysis} from './session-analysis-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,hash='a'.repeat(64);
const response=()=>({id:id(1),revision:'1',document:{artifact_id:id(2),sha256:hash},data:{id:id(1),resource_type:'session_analyses',tenant_id:id(3),project_id:id(4),session_id:id(5),workflow_id:id(6),snapshot_id:id(7),plan_artifact_id:id(2),policy_sha:hash,model_lock_sha:hash,created_at:'2026-10-17T12:00:00.123456789Z'}});
const scope={tenant:id(3),project:id(4),session:id(5),id:id(1)};
it('decodes the actual immutable Resource envelope and detaches caller objects',()=>{const value=response(),out=readSessionAnalysis(value,scope);value.data.project_id=id(90);expect(out.data.project_id).toBe(id(4));expect(out.revision).toBe('1');});
it('refuses cross-scope resources, corrupt pins and malformed immutable fields',()=>{for(const patch of [{tenant_id:id(90)},{project_id:id(90)},{session_id:id(90)},{workflow_id:'bad'},{snapshot_id:'bad'},{model_lock_sha:'bad'},{policy_sha:'bad'},{plan_artifact_id:id(90)},{created_at:'today'}]){const value=response();Object.assign(value.data,patch);expect(()=>readSessionAnalysis(value,scope)).toThrow('SESSION_ANALYSIS_INVALID');}expect(()=>readSessionAnalysis({...response(),revision:'2'},scope)).toThrow();});
