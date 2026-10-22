// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {ComponentType} from 'react';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
vi.mock('react-native',async()=>import('react-native-web'));
import {LocalPortraitApplyAction} from './local-portrait-apply-action';
import {OpenEyesApplyAction} from './open-eyes-apply-action';
import {GlareApplyAction} from './glare-apply-action';
import {RelightApplyAction} from './relight-apply-action';
import {ReflectionApplyAction} from './reflection-apply-action';
import {NegativeApplyAction} from './negative-apply-action';
const actions=[['portrait.hair','applyLocalPortraitSelected',LocalPortraitApplyAction],['portrait.open_eyes','applyOpenEyesSelected',OpenEyesApplyAction],['portrait.glare','applyGlareSelected',GlareApplyAction],['image.relight','applyRelightSelected',RelightApplyAction],['image.reflection','applyReflectionSelected',ReflectionApplyAction],['optical.negative','applyNegativeSelected',NegativeApplyAction]] as const;
afterEach(()=>{cleanup();vi.useRealTimers();});
it.each(actions)('recovers %s selected Apply after cancel without opening a late stale receipt',async(kind,method,Action)=>{
 let finish!:(value:unknown)=>void;const apply=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValueOnce({asset_id:'asset',asset_version_id:'same-version'}),onOpen=vi.fn();
 const api={me:vi.fn().mockResolvedValue({id:'actor'}),[method]:apply} as unknown as OwnerAPI,review={id:'review',kind,state:'decided',decision:'accept',decision_hash:'a'.repeat(64)} as Review;
 const Component=Action as ComponentType<any>;render(<Component api={api} review={review} onOpen={onOpen}/>);
 fireEvent.click(screen.getByRole('button',{name:'Save result and open editor'}));await waitFor(()=>expect(apply).toHaveBeenCalledOnce());
 fireEvent.click(screen.getByRole('button',{name:'Cancel saving result'}));fireEvent.click(screen.getByRole('button',{name:'Save result and open editor'}));await waitFor(()=>expect(onOpen).toHaveBeenCalledOnce());
 expect(apply.mock.calls[1][0]).toBe(review);expect(apply.mock.calls[0][1].aborted).toBe(true);
 await act(async()=>finish({asset_id:'asset',asset_version_id:'stale-version'}));expect(onOpen).toHaveBeenCalledOnce();expect(onOpen).toHaveBeenCalledWith({asset_id:'asset',asset_version_id:'same-version'});
});
it.each(actions)('bounds a noncooperative %s selected Apply while keeping explicit safe retry',async(kind,method,Action)=>{
 vi.useFakeTimers();const apply=vi.fn(()=>new Promise<never>(()=>{})),api={me:vi.fn().mockResolvedValue({id:'actor'}),[method]:apply} as unknown as OwnerAPI,onOpen=vi.fn(),review={id:'review',kind,state:'decided',decision:'accept',decision_hash:'a'.repeat(64)} as Review;
 const Component=Action as ComponentType<any>;render(<Component api={api} review={review} onOpen={onOpen}/>);fireEvent.click(screen.getByRole('button',{name:'Save result and open editor'}));await act(async()=>{});
 await act(async()=>vi.advanceTimersByTimeAsync(15000));expect(screen.getByRole('alert')).toBeTruthy();expect(screen.queryByRole('button',{name:'Cancel saving result'})).toBeNull();expect(onOpen).not.toHaveBeenCalled();
});
