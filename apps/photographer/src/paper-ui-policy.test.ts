import {readdirSync,readFileSync} from 'node:fs';
import {dirname,join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import ts from 'typescript';
import {expect,it} from 'vitest';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
function sources(dir:string):string[]{return readdirSync(dir,{withFileTypes:true}).flatMap(entry=>{
 const file=join(dir,entry.name);
 return entry.isDirectory()?sources(file):/\.tsx?$/.test(file)&&!file.includes('.test.')?[file]:[];
});}
it('keeps ordinary interactive and typography primitives on Paper across every production route/component',()=>{
 const forbidden=new Set(['Text','TextInput','ActivityIndicator','Pressable','Button','Switch','Modal','TouchableOpacity','TouchableHighlight','TouchableWithoutFeedback']);
 const violations:string[]=[];
 for(const file of [...sources(join(root,'app')),...sources(join(root,'src'))]){
  const source=ts.createSourceFile(file,readFileSync(file,'utf8'),ts.ScriptTarget.Latest,true);
  for(const node of source.statements){
   if(!ts.isImportDeclaration(node)||!ts.isStringLiteral(node.moduleSpecifier)||node.moduleSpecifier.text!=='react-native')continue;
   const bindings=node.importClause?.namedBindings;
   if(!bindings||!ts.isNamedImports(bindings))continue;
   for(const item of bindings.elements){const imported=(item.propertyName??item.name).text;if(forbidden.has(imported))violations.push(`${file.slice(root.length+1)}: ${imported}`);}
  }
 }
 expect(violations,'Use Paper controls; keep React Native only for layout, platform APIs, images and custom image surfaces').toEqual([]);
});
