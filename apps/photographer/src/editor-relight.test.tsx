// @vitest-environment jsdom
import React from 'react';
import {cleanup,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
vi.mock('react-native',async()=>await import('react-native-web'));
const journal=vi.hoisted(()=>({saved:null as unknown}));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./relight-attempt-store',()=>({RelightAttemptStore:class{read=async()=>journal.saved;write=async()=>{};}}));
vi.mock('./relight-controls',()=>({RelightControls:(props:any)=><div data-testid="relight-controls">{props.prepared.snapshot_id}</div>}));
import {EditorRelight} from './editor-relight';
import type {OwnerAPI} from './owner-api';
const id='11111111-1111-4111-8111-111111111111',other='22222222-2222-4222-8222-222222222222';
afterEach(()=>{cleanup();journal.saved=null;});
it('restores unknown saved body before discovering new source or snapshot',async()=>{
 journal.saved={body:{snapshot_id:other}};
 const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn(),createRenderInputSnapshot:vi.fn()};
 render(<EditorRelight api={api as unknown as OwnerAPI} origin="https://relight.test" tenant={id} project={id} asset={id} version={id} onSubmitted={vi.fn()}/>);
 expect((await screen.findByTestId('relight-controls')).textContent).toBe(other);
 expect(api.v2Binding).not.toHaveBeenCalled();expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
});
it('does not construct a new snapshot for a changed source version',async()=>{
 const api={me:vi.fn().mockResolvedValue({id}),v2Binding:vi.fn().mockResolvedValue({asset_version_id:other}),createRenderInputSnapshot:vi.fn()};
 render(<EditorRelight api={api as unknown as OwnerAPI} origin="https://relight.test" tenant={id} project={id} asset={id} version={id} onSubmitted={vi.fn()}/>);
 await waitFor(()=>expect(screen.getByRole('alert')).toBeTruthy());expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
});
