// Unchanged fixed MiSub template renderer (MIT), local Node oracle only.
import fs from 'node:fs';
import vm from 'node:vm';
const proxy=JSON.parse(fs.readFileSync(0,'utf8'));
const source=fs.readFileSync(new URL('./render-loon.js',import.meta.url),'utf8')
  .replace(/^import .*;\r?\n/gm,'').replace(/^export /gm,'');
const sandbox={proxy};
vm.runInNewContext(source+'\nresult=buildProxyLine(proxy);',sandbox,{timeout:1000});
process.stdout.write(JSON.stringify(sandbox.result));
