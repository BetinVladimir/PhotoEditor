import {it,expect} from 'vitest';
import {readEditingRecipe} from './editing-recipe-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`,ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const recipe=()=>({schema_version:3,source:{asset_version_id:id(1),source:ref(2),canvas_hash:'b'.repeat(64)},base_style:ref(3),session_corrections:null,learning_version_id:null,overrides:[],layer_stack:null,renderer_version:'v2.2',parameter_delta:{ev:0,log_r:0,log_g:0,log_b:0,saturation_delta:0,retouch_delta:0}});
it('owns actual editable recipes with omitted or qualified protected masks',()=>{const raw=recipe(),value=readEditingRecipe(raw);raw.source.source.sha256='c'.repeat(64);expect(value.source.source.sha256).toBe('a'.repeat(64));expect(readEditingRecipe({...recipe(),protected_mask:ref(4)}).protected_mask).toEqual(ref(4));});
it('refuses other generic document types, malformed refs, excess fields and out-of-bound corrections',()=>{
 for(const value of [{schema_version:1,source:recipe().source,layers:[]},{...recipe(),runtime:'cuda'},{...recipe(),protected_mask:null},{...recipe(),base_style:{...ref(3),artifact_id:'bad'}},{...recipe(),parameter_delta:{...recipe().parameter_delta,log_r:0.223145}},{...recipe(),parameter_delta:{...recipe().parameter_delta,ev:NaN}},{...recipe(),overrides:Array.from({length:129},()=>ref(4))}])expect(()=>readEditingRecipe(value)).toThrow('EDITING_RECIPE_INVALID');
});
