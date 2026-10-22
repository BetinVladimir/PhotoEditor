// @vitest-environment jsdom
import {useState} from 'react';
import {it,expect,vi,afterEach} from 'vitest';
import {render,screen,fireEvent,cleanup} from '@testing-library/react';
import en from '../../../locales/en/common.json';
import {compareFrames,type FrameComparison} from './compare-frames';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {CompareFramesView} from './compare-frames-view';
afterEach(cleanup);
const frames=['a','b'].map(versionId=>({versionId,sourceSha:'a'.repeat(64),iccSha:'b'.repeat(64),previewURI:'https://photo.test/'+versionId}));
it('buttons and keyboard share selection/decision and viewport state',()=>{
 let current!:FrameComparison;
 function Harness(){const [state,setState]=useState(()=>compareFrames(frames));current=state;return <><input aria-label="recipe"/><CompareFramesView state={state} onChange={setState}/></>;}
 const view=render(<Harness/>);fireEvent.click(screen.getByRole('button',{name:en.compare_next}));expect(current.selected).toBe(1);
 fireEvent.keyDown(document,{key:'Backspace',ctrlKey:true});expect(current.decisions).toEqual({b:'reject'});expect(screen.getByText(en.compare_rejected)).toBeDefined();
 fireEvent.keyDown(view.getByLabelText('recipe'),{key:'ArrowLeft',ctrlKey:true});expect(current.selected).toBe(1);
 fireEvent.click(screen.getByRole('button',{name:en.compare_zoom_in}));fireEvent.click(screen.getByRole('button',{name:en.compare_pan_right}));expect(current.viewport).toEqual({scale:2,centerX:.6,centerY:.5});
 fireEvent.click(screen.getByRole('button',{name:'Frame 1'}));fireEvent.click(screen.getByRole('button',{name:en.compare_accept}));expect(current.decisions).toEqual({a:'accept',b:'reject'});expect(current.frames.map(frame=>frame.previewURI)).toEqual(frames.map(frame=>frame.previewURI));
});
