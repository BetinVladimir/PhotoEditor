// @vitest-environment jsdom
import React from 'react';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render as renderUI,screen,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
function render(ui:any){if(!ui.props.api.reconcileCollectionVersion)ui.props.api.reconcileCollectionVersion=vi.fn().mockRejectedValue(new Error('Unexpected collection reconciliation'));return renderUI(ui);}
const saved=vi.hoisted(()=>({producer:'other',selection:null as null|{collection:string},restore:vi.fn()}));
const lifecycle=vi.hoisted(()=>({listeners:new Set<(state:string)=>void>()}));
vi.mock('react-native',async()=>({...await import('react-native-web'),AppState:{currentState:'active',addEventListener:(_event:string,listener:(state:string)=>void)=>{lifecycle.listeners.add(listener);return{remove:()=>lifecycle.listeners.delete(listener)};}}}));
import {APIError} from '@photoeditor/api-client';
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:string)=>key})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./perspective-apply-store',()=>({PerspectiveApplyStore:class{async read(){return {review:{proposal:{producer_task_id:saved.producer}},selection:saved.selection};}}}));
vi.mock('./perspective-apply-attempt',()=>({PerspectiveApplyAttempt:{restore:saved.restore}}));
import {PerspectiveApplyRecovery} from './perspective-apply-recovery';
afterEach(()=>{cleanup();vi.clearAllMocks();saved.producer='other';saved.selection=null;});
it('withdraws parent recovery projection on denied resume without clearing the journal',async()=>{
 saved.producer='current';const onAccessDenied=vi.fn(),onApplied=vi.fn();saved.restore.mockReturnValue({snapshot:{applied:null,selection:null},resume:async()=>{throw new APIError(403,'FORBIDDEN')}});
 render(<PerspectiveApplyRecovery api={{me:async()=>({id:'user'})} as unknown as OwnerAPI} origin="https://photo.test" tenant="tenant" project="project" version="version" producer="current" collection="" onApplied={onApplied} onAccessDenied={onAccessDenied}/>);
 fireEvent.click(await screen.findByText('perspective_apply_resume'));await waitFor(()=>expect(onAccessDenied).toHaveBeenCalledOnce());expect(onApplied).not.toHaveBeenCalled();expect(screen.queryByText('perspective_apply_resume')).toBeNull();
});
it('does not restore or submit a journal belonging to another replacement producer',async()=>{
 const api={me:vi.fn().mockResolvedValue({id:'user'})} as unknown as OwnerAPI;
 render(<PerspectiveApplyRecovery api={api} origin="https://photo.test" tenant="tenant" project="project" version="version" producer="current" onApplied={vi.fn()}/>);
 await screen.findByText('perspective_apply_failed');expect(saved.restore).not.toHaveBeenCalled();expect(screen.queryByText('perspective_apply_resume')).toBeNull();
});

it('refuses a saved collection replacement in a standalone replacement review',async()=>{
 saved.producer='current';saved.selection={collection:'foreign-collection'};
 const api={me:vi.fn().mockResolvedValue({id:'user'})} as unknown as OwnerAPI;
 render(<PerspectiveApplyRecovery api={api} origin="https://photo.test" tenant="tenant" project="project" version="version" producer="current" collection="" onApplied={vi.fn()}/>);
 await screen.findByText('perspective_apply_failed');expect(saved.restore).not.toHaveBeenCalled();expect(screen.queryByText('perspective_apply_resume')).toBeNull();
});

it('does not navigate recovered result after actor changes',async()=>{
 saved.producer='current';const onApplied=vi.fn(),resume=vi.fn().mockResolvedValue({asset_version_id:'result'});
 saved.restore.mockReturnValue({snapshot:{applied:null,selection:null},resume});
 const api={me:vi.fn().mockResolvedValueOnce({id:'user'}).mockResolvedValueOnce({id:'user'}).mockResolvedValueOnce({id:'other'})} as unknown as OwnerAPI;
 render(<PerspectiveApplyRecovery api={api} origin="https://photo.test" tenant="tenant" project="project" version="version" producer="current" onApplied={onApplied}/>);
 fireEvent.click(await screen.findByText('perspective_apply_resume'));await screen.findByText('perspective_apply_failed');
 expect(resume).toHaveBeenCalledTimes(1);expect(onApplied).not.toHaveBeenCalled();expect(screen.queryByText('perspective_apply_resume')).toBeNull();
});
it('refuses recovery before mutation when the current actor differs from the journal owner',async()=>{
 saved.producer='current';const resume=vi.fn();saved.restore.mockReturnValue({snapshot:{applied:null,selection:null},resume});
 const api={me:vi.fn().mockResolvedValueOnce({id:'user'}).mockResolvedValueOnce({id:'other'})} as unknown as OwnerAPI;
 render(<PerspectiveApplyRecovery api={api} origin="https://photo.test" tenant="tenant" project="project" version="version" producer="current" onApplied={vi.fn()}/>);
 fireEvent.click(await screen.findByText('perspective_apply_resume'));await screen.findByText('perspective_apply_failed');expect(resume).not.toHaveBeenCalled();
});

it.each([401,403,404,410])('blocks recovery retry after definitive access denial %s',async status=>{
 saved.producer='current';const resume=vi.fn().mockRejectedValue(new APIError(status,'FORBIDDEN'));saved.restore.mockReturnValue({snapshot:{applied:null,selection:null},resume});
 const api={me:vi.fn().mockResolvedValue({id:'user'})} as unknown as OwnerAPI;
 render(<PerspectiveApplyRecovery api={api} origin="https://photo.test" tenant="tenant" project="project" version="version" producer="current" onApplied={vi.fn()}/>);
 fireEvent.click(await screen.findByText('perspective_apply_resume'));await screen.findByText('perspective_apply_failed');
 fireEvent.click(screen.getByText('perspective_apply_reload'));expect(resume).toHaveBeenCalledTimes(1);expect(screen.queryByText('perspective_apply_resume')).toBeNull();
});
it('suppresses late recovery navigation in background',async()=>{
 saved.producer='current';let finish!:(value:unknown)=>void;const resume=vi.fn(()=>new Promise(resolve=>{finish=resolve})),onApplied=vi.fn();saved.restore.mockReturnValue({snapshot:{applied:null,selection:null},resume});
 const api={me:vi.fn().mockResolvedValue({id:'user'})} as unknown as OwnerAPI;
 render(<PerspectiveApplyRecovery api={api} origin="https://photo.test" tenant="tenant" project="project" version="version" producer="current" onApplied={onApplied}/>);
 fireEvent.click(await screen.findByText('perspective_apply_resume'));await waitFor(()=>expect(resume).toHaveBeenCalled());
 act(()=>{for(const listener of lifecycle.listeners)listener('background');});await act(async()=>{finish({asset_version_id:'result'});});expect(onApplied).not.toHaveBeenCalled();
});
