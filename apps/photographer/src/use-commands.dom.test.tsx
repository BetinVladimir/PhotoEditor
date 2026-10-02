// @vitest-environment jsdom
import {it,expect,vi,afterEach} from 'vitest';
import {render,fireEvent,cleanup} from '@testing-library/react';
vi.mock('react-native',()=>({Platform:{OS:'web'}}));
import {useCommands} from './use-commands';
afterEach(cleanup);
it('uses actual editable targets and latest handlers without leaking listeners',()=>{
 const first=vi.fn(),next=vi.fn(),save=vi.fn();
 function Harness({action}:{action:()=>void}){useCommands({'compare.next':action,'capture.save':save});return <><input aria-label="recipe"/><div contentEditable><span data-testid="editable-child">text</span></div></>;}
 const view=render(<Harness action={first}/>);
 fireEvent.keyDown(document,{key:'ArrowRight',ctrlKey:true});expect(first).toHaveBeenCalledOnce();
 fireEvent.keyDown(view.getByLabelText('recipe'),{key:'ArrowRight',ctrlKey:true});fireEvent.keyDown(view.getByTestId('editable-child'),{key:'ArrowRight',ctrlKey:true});expect(first).toHaveBeenCalledOnce();
 fireEvent.keyDown(view.getByLabelText('recipe'),{key:'s',ctrlKey:true});expect(save).toHaveBeenCalledOnce();
 view.rerender(<Harness action={next}/>);fireEvent.keyDown(document,{key:'ArrowRight',ctrlKey:true});expect(next).toHaveBeenCalledOnce();expect(first).toHaveBeenCalledOnce();
 view.unmount();fireEvent.keyDown(document,{key:'ArrowRight',ctrlKey:true});expect(next).toHaveBeenCalledOnce();
});
