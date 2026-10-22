import {expect,it} from 'vitest';
import {SessionAnalysisAttemptStore,clearSessionAnalysisAttempts,type SessionAnalysisAttempt} from './session-analysis-attempt-store';
import type {SemanticJournalLock} from './semantic-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock:SemanticJournalLock=async(_origin,action)=>action();
function memory(){const data=new Map<string,string>();return {getItem:async(k:string)=>data.get(k)??null,setItem:async(k:string,v:string)=>{data.set(k,v)},removeItem:async(k:string)=>{data.delete(k)},getAllKeys:async()=>[...data.keys()]};}
const attempt=():SessionAnalysisAttempt=>({schema_version:1,session_id:id(4),key:id(6),request:{expected_revision:'3',selection_snapshot_id:id(7),policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(8),sha256:'a'.repeat(64)}},manual_references:[]},ticket:null});
it('retains immutable initial analysis and its accepted ticket across remount',async()=>{
 const storage=memory(),first=new SessionAnalysisAttemptStore(storage,'origin',id(1),id(2),id(4),lock),record=attempt();
 await first.write(record);record.request.policy.time_gap_seconds=120;
 const second=new SessionAnalysisAttemptStore(storage,'origin',id(1),id(2),id(4),lock);
 expect((await second.read())?.request.policy.time_gap_seconds).toBe(60);
 await second.write({...attempt(),ticket:{run_id:id(12)}});
 expect((await first.read())?.ticket).toEqual({run_id:id(12)});
 await expect(first.discardRejected(id(6))).rejects.toThrow('CONFLICT');
});
it('refuses changed request or key and cannot cross account or tenant scope',async()=>{
 const storage=memory(),store=new SessionAnalysisAttemptStore(storage,'origin',id(1),id(2),id(4),lock);
 await store.write(attempt());
 await expect(async()=>store.write({...attempt(),key:id(9)})).rejects.toThrow('CONFLICT');
 await expect(async()=>store.write({...attempt(),request:{...attempt().request,expected_revision:'4'}})).rejects.toThrow('CONFLICT');
 expect(await new SessionAnalysisAttemptStore(storage,'origin',id(10),id(2),id(4),lock).read()).toBeNull();
 expect(await new SessionAnalysisAttemptStore(storage,'origin',id(1),id(10),id(4),lock).read()).toBeNull();
 await store.discardRejected(id(6));expect(await store.read()).toBeNull();
});
it('rejects manual reference decisions in an initial analysis journal',async()=>{
 const store=new SessionAnalysisAttemptStore(memory(),'origin',id(1),id(2),id(4),lock);
 await expect(async()=>store.write({...attempt(),request:{...attempt().request,manual_references:[{previous_analysis_id:id(5),group_id:id(9),asset_version_id:id(10)}]}})).rejects.toThrow('INVALID');
});
it('fences mounted writers and clears saved attempts at logout',async()=>{
 const storage=memory(),store=new SessionAnalysisAttemptStore(storage,'logout-analysis',id(1),id(2),id(4),lock);
 await store.write(attempt());await clearSessionAnalysisAttempts(storage,'logout-analysis',lock);
 await expect(store.read()).rejects.toThrow('EXPIRED');
 expect(await new SessionAnalysisAttemptStore(storage,'logout-analysis',id(1),id(2),id(4),lock).read()).toBeNull();
});

it('clears an accepted attempt only for its exact key and run',async()=>{
 const storage=memory(),store=new SessionAnalysisAttemptStore(storage,'clear-analysis',id(1),id(2),id(4),lock);
 await store.write({...attempt(),ticket:{run_id:id(12)}});
 await expect(store.clearAccepted(id(6),id(13))).rejects.toThrow('CONFLICT');
 await store.clearAccepted(id(6),id(12));expect(await store.read()).toBeNull();
});
