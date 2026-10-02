// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {render,screen,fireEvent,cleanup} from '@testing-library/react';
import en from '../../../locales/en/common.json';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})}));
import {EditorDraftConflict} from './editor-draft-conflict';
const server={asset_version_id:'version',source_sha256:'a'.repeat(64),revision:'8',params:{serverRecipe:true},mask_refs:[{id:'server-mask'}]};
afterEach(cleanup);
it('shows both snapshots without choosing or writing automatically',()=>{
 const resolve=vi.fn();
 render(<EditorDraftConflict localRecipe='local recipe text' localMasks={[{id:'local-mask'}]} server={server} busy={false} onResolve={resolve}/>);
 expect(screen.getByRole('alert').textContent).toBe(en.editor_conflict_title);
 expect(screen.getByText('local recipe text')).toBeTruthy();
 expect(screen.getByText('[{"id":"local-mask"}]')).toBeTruthy();
 expect(screen.getByText(/"server-mask"/)).toBeTruthy();
 expect(resolve).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole('button',{name:en.editor_conflict_keep_local}));
 expect(resolve.mock.calls).toEqual([['local']]);
 fireEvent.click(screen.getByRole('button',{name:en.editor_conflict_use_server}));
 expect(resolve.mock.calls).toEqual([['local'],['server']]);
});
it('blocks both choices while a save is still pending',()=>{
 const resolve=vi.fn();
 render(<EditorDraftConflict localRecipe='local' localMasks={[]} server={server} busy onResolve={resolve}/>);
 for(const name of [en.editor_conflict_keep_local,en.editor_conflict_use_server]){
  const button=screen.getByRole('button',{name});expect(button.getAttribute('aria-disabled')).toBe('true');fireEvent.click(button);
 }
 expect(resolve).not.toHaveBeenCalled();
});
