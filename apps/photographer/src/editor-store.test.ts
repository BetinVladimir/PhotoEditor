import{describe,expect,it}from"vitest";
import{acceptEditorDraftSave,acceptsEditorPreview,applyEditorCommand,beginEditorPreview,createEditorState,currentEditorSnapshot,prepareEditorDraftSave,resolveEditorDraftConflict,redoEditor,screenToSource,setEditorViewport,undoEditor}from"./editor-store";
import type{EditorDraft}from"./catalog-types";

const draft:EditorDraft={asset_version_id:"version",revision:"4",params:{schema_version:1,modules:{strength:1}},mask_refs:[],source_sha256:"a".repeat(64)};
describe("EditorStore",()=>{
 it("keeps immutable history and monotonic request revisions",()=>{const initial=createEditorState(draft),changed=applyEditorCommand(initial,{params:{schema_version:1,modules:{strength:.5}}});expect(currentEditorSnapshot(initial).params).toEqual(draft.params);expect(changed.requestRevision).toBe(1);const undone=undoEditor(changed),redone=redoEditor(undone);expect(undone.requestRevision).toBe(2);expect(currentEditorSnapshot(redone).params).toEqual(currentEditorSnapshot(changed).params)});
 it("accepts only the exact latest source and draft preview",()=>{const initial=createEditorState(draft),first=beginEditorPreview(initial),second=beginEditorPreview(first.state);expect(acceptsEditorPreview(second.state,first.token)).toBe(false);expect(acceptsEditorPreview(second.state,second.token)).toBe(true);expect(acceptsEditorPreview({...second.state,sourceSha:"b".repeat(64)},second.token)).toBe(false)});
 it("inverts zoom and pan into oriented source coordinates",()=>{const viewport={scale:2,offsetX:10,offsetY:-20};expect(screenToSource({x:110,y:30},{width:200,height:100},viewport)).toEqual({x:.5,y:.5});expect(setEditorViewport(createEditorState(draft),viewport).viewport).toEqual(viewport);expect(()=>setEditorViewport(createEditorState(draft),{...viewport,scale:17})).toThrow("VIEWPORT_INVALID")});
});

it("compacts history to full checkpoints and preserves branching undo",()=>{
 const initial=createEditorState(draft);
 let state=initial;
 for(let i=1;i<=250;i++)state=applyEditorCommand(state,{params:{value:i},maskRefs:[{value:i}],selectionObjectId:`object-${i}`});
 expect(state.history).toHaveLength(200);
 expect(state.historyIndex).toBe(199);
 expect(currentEditorSnapshot(initial).params).toEqual(draft.params);
 const checkpoint=state.history[0];
 expect(checkpoint).toEqual({params:{value:51},maskRefs:[{value:51}],selectionObjectId:"object-51"});
 const latest=state;
 for(let i=0;i<199;i++)state=undoEditor(state);
 expect(currentEditorSnapshot(state)).toEqual(checkpoint);
 expect(undoEditor(state)).toBe(state);
 for(let i=0;i<199;i++)state=redoEditor(state);
 expect(currentEditorSnapshot(state)).toEqual(currentEditorSnapshot(latest));
 state=undoEditor(undoEditor(state));
 const preview=beginEditorPreview(state);
 const branched=applyEditorCommand(preview.state,{params:{value:"branch"}});
 expect(branched.history).toHaveLength(199);
 expect(redoEditor(branched)).toBe(branched);
 expect(acceptsEditorPreview(branched,preview.token)).toBe(false);
 expect(currentEditorSnapshot(undoEditor(branched)).params).toEqual({value:248});
 expect(currentEditorSnapshot(latest).params).toEqual({value:250});
});


it("saves the selected mask checkpoint without changing CAS authority",()=>{
 const initial=createEditorState({...draft,mask_refs:[{id:"old"}]}),changed=applyEditorCommand(initial,{maskRefs:[{id:"new"}]});
 const server={...draft,mask_refs:[{id:"new"}]};
 const params={schema_version:1,modules:{strength:.2}};
 const saved=prepareEditorDraftSave(undoEditor(changed),server,params);
 expect(saved.mask_refs).toEqual([{id:"old"}]);
 expect(saved.revision).toBe(server.revision);
 saved.mask_refs.push({id:"mutated"});saved.params.modules={strength:9};
 expect(currentEditorSnapshot(initial).maskRefs).toEqual([{id:"old"}]);
 expect(params.modules.strength).toBe(.2);
 expect(()=>prepareEditorDraftSave({...initial,sourceSha:"b".repeat(64)},server,params)).toThrow("EDITOR_SOURCE_CHANGED");
 expect(()=>prepareEditorDraftSave({...initial,versionId:"other"},server,params)).toThrow("EDITOR_SOURCE_CHANGED");
 expect(()=>prepareEditorDraftSave(initial,{...server,revision:"5"},params)).toThrow("DRAFT_REVISION_CHANGED");
});


it("preserves newer edits when a save response arrives late",()=>{
 const submitted=createEditorState(draft),saved={...draft,revision:"5",params:{value:"saved"}};
 const newer=applyEditorCommand(submitted,{params:{value:"newer"}});
 const accepted=acceptEditorDraftSave(newer,submitted,saved);
 expect(accepted.replaceRecipe).toBe(false);
 expect(currentEditorSnapshot(accepted.state).params).toEqual({value:"newer"});
 expect(accepted.state.draftRevision).toBe(5);
 expect(accepted.state.history).toBe(newer.history);
 const clean=acceptEditorDraftSave(submitted,submitted,saved);
 expect(clean.replaceRecipe).toBe(true);
 expect(currentEditorSnapshot(clean.state).params).toEqual(saved.params);
 expect(()=>acceptEditorDraftSave(accepted.state,submitted,saved)).toThrow("DRAFT_REVISION_CHANGED");
 expect(()=>acceptEditorDraftSave({...newer,sourceSha:"other"},submitted,saved)).toThrow("EDITOR_SOURCE_CHANGED");
});


it("resolves conflicts only by explicit source-bound choice",()=>{
 const local=applyEditorCommand(createEditorState(draft),{params:{value:"local"},maskRefs:[{id:"local"}]}),server={...draft,revision:"8",params:{value:"server"},mask_refs:[{id:"server"}]};
 const kept=resolveEditorDraftConflict(local,server,"local");
 expect(currentEditorSnapshot(kept).params).toEqual({value:"local"});
 expect(prepareEditorDraftSave(kept,server,currentEditorSnapshot(kept).params)).toMatchObject({revision:"8",mask_refs:[{id:"local"}]});
 const accepted=resolveEditorDraftConflict(local,server,"server");
 expect(currentEditorSnapshot(accepted)).toMatchObject({params:{value:"server"},maskRefs:[{id:"server"}]});
 expect(currentEditorSnapshot(undoEditor(accepted)).params).toEqual({value:"local"});
 expect(local.draftRevision).toBe(4);
 expect(()=>resolveEditorDraftConflict(local,{...server,source_sha256:"other"},"local")).toThrow("EDITOR_SOURCE_CHANGED");
 expect(()=>resolveEditorDraftConflict(local,{...server,revision:"3"},"server")).toThrow("DRAFT_REVISION_CHANGED");
});

it("keeps local masks until an explicit choice after a late mask save",()=>{
 const submitted=createEditorState({...draft,mask_refs:[{id:"base"}]}),newer=applyEditorCommand(submitted,{params:{value:"newer"},maskRefs:[]});
 const persisted={...draft,revision:"5",mask_refs:[{id:"base"},{id:"generated"}]};
 const accepted=acceptEditorDraftSave(newer,submitted,persisted);
 expect(accepted.replaceRecipe).toBe(false);
 expect(currentEditorSnapshot(accepted.state).maskRefs).toEqual([]);
 const local=resolveEditorDraftConflict(accepted.state,persisted,"local");
 expect(prepareEditorDraftSave(local,persisted,currentEditorSnapshot(local).params).mask_refs).toEqual([]);
 const server=resolveEditorDraftConflict(accepted.state,persisted,"server");
 expect(currentEditorSnapshot(server).maskRefs).toEqual(persisted.mask_refs);
 expect(currentEditorSnapshot(undoEditor(server)).params).toEqual({value:"newer"});
});
