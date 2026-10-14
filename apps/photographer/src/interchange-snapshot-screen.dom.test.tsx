// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
import {InterchangeSnapshotScreen} from './interchange-snapshot-screen';
import {InterchangeSnapshotAttemptStore} from './interchange-snapshot-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const body={items:[{ordinal:0,binding:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},working_image:{artifact_id:id(4),sha256:'a'.repeat(64)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)},captured_at:null,camera_hash:null}]};
const hash=(v:unknown)=>bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v))));
const unsigned={schema_version:1,snapshot_id:id(6),items:body.items},digest=hash(unsigned);
const receipt={id:id(6),digest,item_count:1,artifact:{artifact_id:id(7),sha256:hash({...unsigned,snapshot_digest:digest})}};
function storage(){const values=new Map<string,string>();return {getItem:vi.fn(async(k:string)=>values.get(k)??null),setItem:vi.fn(async(k:string,v:string)=>{values.set(k,v)}),removeItem:vi.fn(async(k:string)=>{values.delete(k)})};}
const journal=(s:ReturnType<typeof storage>)=>new InterchangeSnapshotAttemptStore(s,'https://snapshot-screen.test',id(9),id(1),id(2),body,async(_origin,action)=>action());
const props=(request:ReturnType<typeof vi.fn>,store:InterchangeSnapshotAttemptStore,onPrepared=vi.fn())=>({client:{request} as never,tenant:id(1),project:id(2),request:body,journal:store,onPrepared});
afterEach(cleanup);
async function prepare(){await waitFor(()=>expect(screen.getByRole('checkbox').getAttribute('aria-disabled')).not.toBe('true'));fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText('interchange_snapshot_submit'));}
it('replays saved original source/recipe/key after lost reply and remount',async()=>{
 const s=storage(),request=vi.fn().mockRejectedValueOnce(new Error('lost reply')).mockResolvedValueOnce(receipt),onPrepared=vi.fn();
 const view=render(<InterchangeSnapshotScreen {...props(request,journal(s),onPrepared)}/>);await prepare();await screen.findByRole('alert');const first=request.mock.calls[0][1];
 view.unmount();render(<InterchangeSnapshotScreen {...props(request,journal(s),onPrepared)}/>);fireEvent.click(await screen.findByText('interchange_snapshot_retry'));
 await screen.findByText('interchange_snapshot_open');expect(request.mock.calls[1][1]).toMatchObject({body:first.body,idempotencyKey:first.idempotencyKey});expect(onPrepared).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText('interchange_snapshot_open'));await waitFor(()=>expect(onPrepared).toHaveBeenCalledWith(receipt));
});
it('refuses POST when intent persistence fails',async()=>{
 const s=storage();s.setItem.mockRejectedValue(new Error('disk full'));const request=vi.fn();render(<InterchangeSnapshotScreen {...props(request,journal(s))}/>);
 await prepare();await screen.findByRole('alert');expect(request).not.toHaveBeenCalled();
});
it('reuses acknowledged receipt without POST and refuses removed acknowledgement',async()=>{
 const s=storage(),store=journal(s);await store.write({version:1,key:'original',body,receipt});const request=vi.fn(),onPrepared=vi.fn();
 render(<InterchangeSnapshotScreen {...props(request,store,onPrepared)}/>);await screen.findByText('interchange_snapshot_open');await store.clear('original');
 fireEvent.click(screen.getByText('interchange_snapshot_open'));await screen.findByRole('alert');expect(request).not.toHaveBeenCalled();expect(onPrepared).not.toHaveBeenCalled();
});

it('stops waiting while retaining the original snapshot key for retry',async()=>{
 const s=storage(),request=vi.fn().mockImplementationOnce(async(_path:string,options:{signal:AbortSignal})=>await new Promise((_resolve,reject)=>options.signal.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError')),{once:true}))).mockResolvedValueOnce(receipt);
 render(<InterchangeSnapshotScreen {...props(request,journal(s))}/>);await prepare();
 await screen.findByText('interchange_snapshot_stop');fireEvent.click(screen.getByText('interchange_snapshot_stop'));
 await waitFor(()=>expect(screen.getByText('interchange_snapshot_retry').getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(screen.getByText('interchange_snapshot_retry'));await screen.findByText('interchange_snapshot_open');
 expect(request.mock.calls[1][1].idempotencyKey).toBe(request.mock.calls[0][1].idempotencyKey);
});

it('retries cancelled journal restoration without preparing a snapshot',async()=>{
 const s=storage(),store=journal(s),request=vi.fn();let release!:(value:null)=>void;
 vi.spyOn(store,'read').mockImplementationOnce(()=>new Promise(resolve=>{release=resolve}));
 render(<InterchangeSnapshotScreen {...props(request,store)}/>);
 await screen.findByText('interchange_snapshot_stop');fireEvent.click(screen.getByText('interchange_snapshot_stop'));release(null);
 await screen.findByText('interchange_snapshot_restore');fireEvent.click(screen.getByText('interchange_snapshot_restore'));
 await waitFor(()=>expect(screen.getByRole('checkbox').getAttribute('aria-disabled')).not.toBe('true'));
 expect(request).not.toHaveBeenCalled();
});
it('ignores a late snapshot response after unmount and restores original pending key',async()=>{
 const s=storage(),store=journal(s),onPrepared=vi.fn();let release!:(value:typeof receipt)=>void;
 const request=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{release=resolve}));
 const view=render(<InterchangeSnapshotScreen {...props(request,store,onPrepared)}/>);await prepare();
 await waitFor(()=>expect(request).toHaveBeenCalledTimes(1));const key=request.mock.calls[0][1].idempotencyKey;
 view.unmount();release(receipt);await Promise.resolve();
 const saved=await store.read();expect(saved?.key).toBe(key);expect(saved?.receipt).toBeNull();expect(onPrepared).not.toHaveBeenCalled();
 render(<InterchangeSnapshotScreen {...props(vi.fn().mockResolvedValue(receipt),journal(s),onPrepared)}/>);
 await screen.findByText('interchange_snapshot_retry');expect(onPrepared).not.toHaveBeenCalled();
});

it('stop is immediately responsive when journal restoration never settles',async()=>{
 const store=journal(storage()),request=vi.fn();vi.spyOn(store,'read').mockImplementationOnce(()=>new Promise(()=>{}));
 render(<InterchangeSnapshotScreen {...props(request,store)}/>);
 await screen.findByText('interchange_snapshot_stop');fireEvent.click(screen.getByText('interchange_snapshot_stop'));
 await screen.findByText('interchange_snapshot_restore');expect(request).not.toHaveBeenCalled();
 expect(screen.queryByText('interchange_snapshot_stop')).toBeNull();
});
