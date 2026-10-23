// Сканер соответствия Paper (только чтение). Запуск из apps/photographer:
//   node ../../docs/ui-audit/scan_paper.mjs > /tmp/paper-scan.json
// Проверяет app/, screens/, src/ (без *.test.*): запрещённые примитивы React Native (прямой импорт, пространство имён, require,
// Animated.Text), сторонние UI-библиотеки, DOM-теги вне *.web.tsx, жёсткие цвета и шрифты в стилях, Alert, подписи доступности,
// непереведённые строки в JSX. Результат — JSON; ничего не правит.
import {createRequire} from 'node:module';
import {readdirSync, readFileSync} from 'node:fs';
import {join, resolve, relative} from 'node:path';
const require = createRequire(join(process.cwd(), 'package.json'));
const ts = require('typescript');
const root = process.cwd();
const FORBIDDEN = new Set(['Text', 'TextInput', 'ActivityIndicator', 'Pressable', 'Button', 'Switch', 'Modal', 'TouchableOpacity', 'TouchableHighlight', 'TouchableWithoutFeedback', 'TouchableNativeFeedback', 'Touchable', 'RefreshControl', 'Picker', 'SegmentedControlIOS', 'ProgressBarAndroid', 'ProgressViewIOS', 'StatusBar']);
const FORBIDDEN_STATUSBAR_OK = new Set(['StatusBar']); // системная панель — платформенный API
const OTHER_UI = ['react-native-elements', '@rneui/', 'native-base', 'react-native-ui-lib', '@ui-kitten', 'react-native-gesture-handler', '@expo/vector-icons', 'react-native-vector-icons', 'react-native-picker', '@react-native-picker', 'react-native-modal', 'react-native-toast', 'react-native-element-dropdown', '@shopify/flash-list', 'react-native-svg'];
const files = [];
(function walk(d) {
  for (const e of readdirSync(d, {withFileTypes: true})) {
    const f = join(d, e.name);
    if (e.isDirectory()) { if (!['node_modules', 'dist', 'assets', '.expo'].includes(e.name)) walk(f); }
    else if (/\.tsx?$/.test(f) && !/\.test\./.test(f) && !f.endsWith('.d.ts')) files.push(f);
  }
})(root);
const scoped = files.filter(f => /\/(app|screens|src)\//.test(f.slice(root.length)));
const out = {files: scoped.length, findings: [], perFile: {}};
const add = (file, rule, line, detail) => out.findings.push({file: relative(root, file), rule, line, detail});
const HEX = /#[0-9a-fA-F]{3,8}\b/;
const RGB = /\b(?:rgba?|hsla?)\(/;
for (const file of scoped) {
  const text = readFileSync(file, 'utf8');
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith('x') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const isWeb = /\.web\.tsx?$/.test(file);
  const isTheme = /paper-theme|paper-readable-fonts/.test(file);
  const rn = {named: new Map(), ns: new Set()};
  const line = n => sf.getLineAndCharacterOfPosition(n.getStart()).line + 1;
  const stats = {jsx: 0, paperImports: 0, hex: 0, fontSize: 0, alert: 0, dom: 0, rawText: 0, iconNoLabel: 0};
  for (const st of sf.statements) {
    if (!ts.isImportDeclaration(st) || !ts.isStringLiteral(st.moduleSpecifier)) continue;
    const m = st.moduleSpecifier.text;
    const b = st.importClause?.namedBindings;
    if (m === 'react-native-paper') stats.paperImports++;
    if (m === 'react-native') {
      if (b && ts.isNamedImports(b)) for (const el of b.elements) {
        const imported = (el.propertyName ?? el.name).text;
        rn.named.set(el.name.text, imported);
        if (FORBIDDEN.has(imported) && !FORBIDDEN_STATUSBAR_OK.has(imported)) add(file, 'rn-forbidden-import', line(el), imported);
        if (imported === 'Alert') { stats.alert++; }
      }
      if (b && ts.isNamespaceImport(b)) rn.ns.add(b.name.text);
    }
    if (OTHER_UI.some(p => m === p || m.startsWith(p))) add(file, 'other-ui-lib', line(st), m);
  }
  const visit = n => {
    if (ts.isCallExpression(n) && n.expression.getText() === 'require' && n.arguments[0] && ts.isStringLiteral(n.arguments[0]) && n.arguments[0].text === 'react-native' && ts.isPropertyAccessExpression(n.parent)) {
      const name = n.parent.name.text; if (FORBIDDEN.has(name) && name !== 'StatusBar') add(file, 'rn-forbidden-require', line(n), name);
    }
    if ((ts.isJsxOpeningElement(n) || ts.isJsxSelfClosingElement(n))) {
      stats.jsx++;
      const tag = n.tagName.getText();
      const base = tag.split('.')[0];
      if (rn.ns.has(base) && FORBIDDEN.has(tag.split('.')[1] ?? '')) add(file, 'rn-forbidden-namespace', line(n), tag);
      if (/^Animated\.Text$/.test(tag)) add(file, 'animated-text', line(n), tag);
      if (/^[a-z]/.test(tag) && !isWeb) { stats.dom++; add(file, 'dom-tag-outside-web', line(n), tag); }
      if (/^[a-z]/.test(tag) && isWeb) { stats.dom++; add(file, 'dom-tag-in-web-file', line(n), tag); }
      if (['IconButton'].includes(tag)) {
        const has = n.attributes.properties.some(p => p.name && ['accessibilityLabel', 'aria-label'].includes(p.name.getText()));
        if (!has) { stats.iconNoLabel++; add(file, 'iconbutton-no-label', line(n), tag); }
      }
      if (['TextInput', 'Searchbar'].includes(tag) && rn.named.get(tag) === undefined) {
        const has = n.attributes.properties.some(p => p.name && ['label', 'placeholder', 'accessibilityLabel', 'aria-label'].includes(p.name.getText()));
        if (!has) add(file, 'input-no-label', line(n), tag);
      }
    }
    if (ts.isJsxText(n)) {
      const t = n.getText().replace(/\s+/g, ' ').trim();
      if (t.length > 1 && /[A-Za-zА-Яа-я]{2,}/.test(t) && !/^[{}()\[\]·•—–\-|/:;,.!?%&+*0-9 ]+$/.test(t)) { stats.rawText++; add(file, 'hardcoded-text', line(n), t.slice(0, 60)); }
    }
    if (ts.isStringLiteralLike(n) && !isTheme) {
      const s = n.text;
      if ((HEX.test(s) && /^#[0-9a-fA-F]{3,8}$/.test(s)) || (RGB.test(s) && /^(rgba?|hsla?)\(/.test(s))) { stats.hex++; add(file, 'hardcoded-color', line(n), s); }
    }
    if (ts.isPropertyAssignment(n) && ['fontSize', 'fontWeight', 'fontFamily', 'lineHeight', 'letterSpacing'].includes(n.name.getText())) {
      stats.fontSize++; add(file, 'hardcoded-font', line(n), n.getText().slice(0, 40));
    }
    if (ts.isCallExpression(n) && /^Alert\.alert$/.test(n.expression.getText())) add(file, 'alert-dialog', line(n), 'Alert.alert');
    ts.forEachChild(n, visit);
  };
  visit(sf);
  out.perFile[relative(root, file)] = stats;
}
const byRule = {};
for (const f of out.findings) byRule[f.rule] = (byRule[f.rule] ?? 0) + 1;
out.byRule = byRule;
out.filesWithRnForbidden = [...new Set(out.findings.filter(f => f.rule.startsWith('rn-forbidden') || f.rule === 'animated-text').map(f => f.file))];
console.log(JSON.stringify(out));
