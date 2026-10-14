import {it,expect} from 'vitest';
import {EditorRenderCache,clearEditorRenderCaches} from './editor-render-cache';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
function fixture(){const values=new Map<string,string>();return {values,getItem:async(key:string)=>values.get(key)??null,setItem:async(key:string,value:string)=>{values.set(key,value);},removeItem:async(key:string)=>{values.delete(key);},getAllKeys:async()=>[...values.keys()]};}
const ticket=()=>({run_id:id(5),state:'queued' as const,child_run_ids:[id(6)]});
it('restores detached admission identifiers within the exact account scope',async()=>{
 const storage=fixture(),cache=new EditorRenderCache(storage,'restore',id(1),id(2),id(3));
 const value=ticket();await cache.write(value,100);value.child_run_ids[0]=id(7);
 expect(await cache.read(101)).toEqual(ticket());
 expect(await new EditorRenderCache(storage,'restore',id(4),id(2),id(3)).read(101)).toBeNull();
 expect(await new EditorRenderCache(storage,'restore',id(1),id(4),id(3)).read(101)).toBeNull();
 expect(await new EditorRenderCache(storage,'restore',id(1),id(2),id(4)).read(101)).toBeNull();
 expect(JSON.parse(storage.values.get(cache.key)!)).toEqual({schema_version:1,saved_at:100,ticket:ticket()});
});
it('purges malformed and expired persisted values',async()=>{
 const storage=fixture(),cache=new EditorRenderCache(storage,'expiry',id(1),id(2),id(3));
 await cache.write(ticket(),100);expect(await cache.read(100+8*86400000)).toBeNull();expect(storage.values.has(cache.key)).toBe(false);
 storage.values.set(cache.key,JSON.stringify({schema_version:1,saved_at:100,ticket:ticket(),url:'https://private'}));
 expect(await cache.read(101)).toBeNull();expect(storage.values.has(cache.key)).toBe(false);
});
it('logout prevents delayed cache owners from recreating history',async()=>{
 const storage=fixture(),cache=new EditorRenderCache(storage,'logout',id(1),id(2),id(3));
 const other=new EditorRenderCache(storage,'other-origin',id(1),id(2),id(3));await other.write(ticket(),100);
 const queued=cache.write(ticket(),100),cleared=clearEditorRenderCaches(storage,'logout');
 await Promise.all([queued,cleared]);await cache.write(ticket(),100);
 expect(await cache.read(101)).toBeNull();expect(storage.values.has(cache.key)).toBe(false);
 expect(await other.read(101)).toEqual(ticket());
});

it('restores an uncertain command with its original keys and refuses replacement',async()=>{
 const {prepareEditorRenderSubmission}=await import('./editor-render-submit');
 const storage=fixture(),cache=new EditorRenderCache(storage,'pending',id(1),id(2),id(3)),now=Date.now(),sha='a'.repeat(64);
 const frozen={document_id:id(9),draft_document_id:id(10),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(8),sha256:sha},canvas_hash:sha},recipe:{artifact_id:id(11),sha256:sha}};
 const command={project:id(4),command:prepareEditorRenderSubmission(frozen,sha,'inventory-original','render-original')};
 await cache.writePending(command,now);
 const restored=await new EditorRenderCache(storage,'pending',id(1),id(2),id(3)).readPending(now);
 expect(restored).toEqual(command);
 await expect(cache.writePending({...command,command:{...command.command,renderKey:'different'}},now)).rejects.toThrow('RENDER_PENDING_EXISTS');
 expect(await cache.readPending(now)).toEqual(command);
 await cache.writePending(restored!,now);await cache.write(ticket(),now);await cache.clearPending(command);
 expect(await cache.readPending(now)).toBeNull();expect(await cache.read(now)).toEqual(ticket());
});
it('clears pending commands at logout and prevents a stale owner writing them',async()=>{
 const {prepareEditorRenderSubmission}=await import('./editor-render-submit');
 const storage=fixture(),cache=new EditorRenderCache(storage,'pending-logout',id(1),id(2),id(3)),sha='a'.repeat(64);
 const frozen={document_id:id(9),draft_document_id:id(10),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(8),sha256:sha},canvas_hash:sha},recipe:{artifact_id:id(11),sha256:sha}};
 const command={project:id(4),command:prepareEditorRenderSubmission(frozen,sha,'inventory','render')};
 await cache.writePending(command);await clearEditorRenderCaches(storage,'pending-logout');
 await expect(cache.writePending(command)).rejects.toThrow('RENDER_CACHE_SCOPE_EXPIRED');
 expect(await cache.readPending()).toBeNull();expect(storage.values.has(cache.pendingKey)).toBe(false);
});

it('removes only the exact selected pending command and keeps accepted history',async()=>{
 const {prepareEditorRenderSubmission}=await import('./editor-render-submit');
 const storage=fixture(),cache=new EditorRenderCache(storage,'forget',id(1),id(2),id(3)),sha='a'.repeat(64);
 const frozen={document_id:id(9),draft_document_id:id(10),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(8),sha256:sha},canvas_hash:sha},recipe:{artifact_id:id(11),sha256:sha}};
 const pending={project:id(4),command:prepareEditorRenderSubmission(frozen,sha,'inventory','render')};
 await cache.writePending(pending);await cache.write(ticket());
 const different=structuredClone(pending);different.command.selection[0].recipe.sha256='b'.repeat(64);
 expect(await cache.clearPending(different)).toBe(false);expect(await cache.readPending()).toEqual(pending);
 expect(await cache.clearPending(pending)).toBe(true);expect(await cache.readPending()).toBeNull();
 expect(await cache.read()).toEqual(ticket());
});

it('retains a retryable command if accepted-ticket cleanup fails',async()=>{
 const {prepareEditorRenderSubmission}=await import('./editor-render-submit');
 const storage=fixture(),cache=new EditorRenderCache(storage,'accept-failure',id(1),id(2),id(3)),sha='a'.repeat(64);
 const frozen={document_id:id(9),draft_document_id:id(10),draft_revision:'1',source:{asset_version_id:id(3),source:{artifact_id:id(8),sha256:sha},canvas_hash:sha},recipe:{artifact_id:id(11),sha256:sha}};
 const pending={project:id(4),command:prepareEditorRenderSubmission(frozen,sha,'inventory','render')};
 await cache.writePending(pending);
 const remove=storage.removeItem;storage.removeItem=async key=>{if(key===cache.pendingKey)throw new Error('storage');return remove(key);};
 await expect(cache.acceptPending(pending,ticket())).rejects.toThrow('storage');
 expect(await cache.read()).toEqual(ticket());expect(await cache.readPending()).toEqual(pending);
 storage.removeItem=remove;await cache.acceptPending(pending,ticket());
 expect(await cache.readPending()).toBeNull();expect(await cache.read()).toEqual(ticket());
});
