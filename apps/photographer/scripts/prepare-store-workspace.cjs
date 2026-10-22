/** Create an isolated, minimal monorepo snapshot; never copy secrets or backend data. */
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const root = path.resolve(__dirname, '../../..');
const target = path.resolve(process.argv[2] || '');
if (!process.argv[2] || !target.startsWith(root + path.sep + 'work' + path.sep)) throw Error('Destination must be a NEW directory inside repository/work');
if (fs.existsSync(target)) throw Error('Refusing to overwrite workspace');
const selected = [];
const rootFiles = ['package.json', 'package-lock.json', 'app.json', 'app.config.js', 'eas.json', 'metro.config.js', 'tsconfig.json', 'expo-env.d.ts'];
function add(rel) {
  const source = path.join(root, rel);
  const stat = fs.lstatSync(source);
  if (stat.isSymbolicLink() || !stat.isFile()) throw Error('Only regular source files allowed: ' + rel);
  if (/(^|\/)(\.env[^/]*|credentials[^/]*|secrets?)(\/|$)|\.(p8|p12|pem|jks|keystore|mobileprovision)$/i.test(rel)) throw Error('Credential-shaped file excluded: ' + rel);
  selected.push({path:rel,sha256:crypto.createHash('sha256').update(fs.readFileSync(source)).digest('hex'),bytes:stat.size});
}
function tree(rel, extensions) {
  for (const entry of fs.readdirSync(path.join(root,rel), {withFileTypes:true})) {
    const child = rel + '/' + entry.name;
    if (entry.isSymbolicLink()) throw Error('Source symlink requires explicit review: ' + child);
    if (entry.isDirectory()) { if (!['node_modules','.expo','dist','__pycache__'].includes(entry.name)) tree(child,extensions); }
    else if (extensions.has(path.extname(entry.name)) && !/\.(test|spec)\.[^.]+$/.test(entry.name)) add(child);
  }
}
for (const f of rootFiles) add('apps/photographer/' + f);
for (const dir of ['app','screens','src','scripts']) tree('apps/photographer/' + dir,new Set(['.ts','.tsx','.js','.cjs','.json']));
tree('apps/photographer/assets',new Set(['.png','.svg','.jpg','.jpeg','.webp','.ttf','.otf']));
add('packages/api-client/package.json');tree('packages/api-client/src',new Set(['.ts','.tsx','.json']));
for(const locale of ['en','ru','bg']) add('locales/'+locale+'/common.json');
fs.mkdirSync(target,{recursive:true});
for(const row of selected){const p=path.join(target,row.path);fs.mkdirSync(path.dirname(p),{recursive:true});fs.copyFileSync(path.join(root,row.path),p);}
fs.writeFileSync(path.join(target,'.gitignore'),'node_modules/\n.expo/\ndist/\n.env*\ncredentials.json\n*.p8\n*.p12\n*.jks\n*.keystore\n');
fs.writeFileSync(path.join(target,'source-manifest.json'),JSON.stringify({purpose:'Framevela internal store testing',publicRelease:false,files:selected},null,2)+'\n');
console.log(JSON.stringify({workspace:target,files:selected.length,bytes:selected.reduce((n,r)=>n+r.bytes,0),uploaded:false}));
