// @vitest-environment jsdom
import React from 'react';
import {it,expect,vi,afterEach} from 'vitest';
import {render,screen,waitFor,cleanup} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
import {PerspectiveReplacementReview} from './perspective-replacement-review';
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
vi.mock('react-native',()=>({View:({children}:any)=><div>{children}</div>,Text:({children,accessibilityRole}:any)=><span role={accessibilityRole}>{children}</span>,Pressable:({children,onPress}:any)=><button onClick={onPress}>{children}</button>}));
vi.mock('./perspective-preview-view',()=>({PerspectivePreviewView:()=>null}));
vi.mock('./perspective-quality-view',()=>({PerspectiveQualityView:()=>null}));
vi.mock('./perspective-replacement-result',()=>({readPerspectiveReplacementResult:vi.fn().mockResolvedValue({state:'pending',workflow_state:'running'})}));
afterEach(cleanup);
const props={origin:'https://example.test',tenant:'tenant',project:'project',review:'review',originalTask:'original',decisionHash:'hash'};
it('discovers current scope before observing replacement result',async()=>{
 const api={perspectiveReplacementScope:vi.fn().mockResolvedValue({tenant:'tenant'})} as unknown as OwnerAPI;
 render(<PerspectiveReplacementReview {...props} api={api}/>);
 await screen.findByText('perspective_quality_pending');
 expect(api.perspectiveReplacementScope).toHaveBeenCalledWith('project','review','original','hash',expect.any(AbortSignal));
});
it('discards old client discovery and aborts it on scope change',async()=>{
 let finish!:(value:unknown)=>void;
 const first={perspectiveReplacementScope:vi.fn().mockImplementation(()=>new Promise(resolve=>{finish=resolve;}))} as unknown as OwnerAPI;
 const next={perspectiveReplacementScope:vi.fn().mockRejectedValue(new Error('revoked'))} as unknown as OwnerAPI;
 const view=render(<PerspectiveReplacementReview {...props} api={first}/>);
 await waitFor(()=>expect(first.perspectiveReplacementScope).toHaveBeenCalledTimes(1));
 const signal=(first.perspectiveReplacementScope as any).mock.calls[0][4] as AbortSignal;
 view.rerender(<PerspectiveReplacementReview {...props} api={next}/>);
 expect(signal.aborted).toBe(true);finish({tenant:'tenant'});
 await screen.findByRole('alert');expect(screen.queryByText('perspective_quality_pending')).toBeNull();
});
