import {it,expect,vi} from 'vitest';
import {prepareEditorEffectsAttempt,submitEditorEffectsAttempt} from './editor-effects-attempt';
import {EditorEffectsStore,clearEditorEffectsAttempts} from './editor-effects-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const frozen=()=>({document_id:id(1),draft_document_id:id(2),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)}});
const attempt=()=>prepareEditorEffectsAttempt(id(6),frozen(),id(7));
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function memory(){const values=new Map<string,string>();return{getItem:async(key:string)=>values.get(key)??null,setItem:async(key:string,value:string)=>{values.set(key,value)},removeItem:async(key:string)=>{values.delete(key)},getAllKeys:async()=>[...values.keys()]};}
const store=(storage:ReturnType<typeof memory>,origin='https://editor.example',user=id(8))=>new EditorEffectsStore(storage,origin,user,id(9),id(3),lock);

it('retains uncertain immutable full recipe and refuses command replacement',async()=>{
 const journal=store(memory()),value=attempt();await journal.write(value);value.frozen.recipe.sha256='d'.repeat(64);
 expect((await journal.read())?.frozen.recipe.sha256).toBe('c'.repeat(64));await expect(journal.write(value)).rejects.toThrow('CONFLICT');
 await expect(journal.clearAccepted(value.key,id(10))).rejects.toThrow('CONFLICT');
});
it('accepted recovery only reads exact build status, including denied access',async()=>{
 const value={...attempt(),accepted:{run_id:id(10),status:'queued' as const}};
 for(const status of [401,403]){
  const api={queueEditorEffects:vi.fn(),editorEffectsStatus:vi.fn().mockRejectedValue({status})};
  await expect(submitEditorEffectsAttempt(api,value,new AbortController().signal,()=>true)).rejects.toEqual({status});
  expect(api.queueEditorEffects).not.toHaveBeenCalled();expect(api.editorEffectsStatus).toHaveBeenCalledWith(id(6),id(10),expect.any(AbortSignal));
 }
});
it('ignores late accepted answer after unmount or account/scope replacement',async()=>{
 let current=true,resolve!:(value:{run_id:string;status:'queued'})=>void;
 const api={queueEditorEffects:vi.fn(()=>new Promise<{run_id:string;status:'queued'}>(done=>{resolve=done})),editorEffectsStatus:vi.fn()};
 const pending=submitEditorEffectsAttempt(api,attempt(),new AbortController().signal,()=>current);
 current=false;resolve({run_id:id(10),status:'queued'});await expect(pending).rejects.toThrow('SCOPE_EXPIRED');
 expect(api.editorEffectsStatus).not.toHaveBeenCalled();
});
it('receipt is monotonic and logout fences old journal writers while isolating accounts',async()=>{
 const storage=memory(),journal=store(storage),other=store(storage,'https://editor.example',id(11));
 const value=attempt();await journal.write(value);expect(await other.read()).toBeNull();
 const accepted={...value,accepted:{run_id:id(10),status:'queued' as const}};await journal.write(accepted);
 await expect(journal.write(value)).rejects.toThrow('CONFLICT');
 await clearEditorEffectsAttempts(storage,'https://editor.example',lock);await expect(journal.write(accepted)).rejects.toThrow('EXPIRED');expect(await store(storage).read()).toBeNull();
});
