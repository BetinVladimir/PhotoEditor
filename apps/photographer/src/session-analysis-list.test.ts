import {expect,it} from 'vitest';
import {readSessionAnalyses} from './session-analysis-list';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const item={id:id(1),revision:'1',document:{artifact_id:id(2),sha256:'a'.repeat(64)},data:{id:id(1),resource_type:'session_analyses',session_id:id(3),plan_artifact_id:id(2),created_at:'2026-10-02T12:00:00Z'}};
it('decodes immutable resources without requiring a nonexistent row revision',()=>{expect(readSessionAnalyses({items:[item],next_cursor:'signed'},id(3)).items[0]).toEqual({id:id(1),sessionId:id(3),createdAt:item.data.created_at,plan:item.document});});
it('rejects rebound resources, duplicate IDs and malformed artifact pins',()=>{for(const items of [[item,item],[{...item,data:{...item.data,session_id:id(4)}}],[{...item,document:{...item.document,sha256:'bad'}}],[{...item,data:{...item.data,plan_artifact_id:id(4)}}]])expect(()=>readSessionAnalyses({items},id(3))).toThrow('SESSION_ANALYSES_INVALID');});
