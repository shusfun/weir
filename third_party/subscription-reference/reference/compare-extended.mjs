// Local test oracle: unchanged parser from MiSub 4952c16 (MIT).
import fs from 'node:fs';
import vm from 'node:vm';
const input=JSON.parse(fs.readFileSync(0,'utf8'));
const source=fs.readFileSync(new URL('./url-to-clash.js',import.meta.url),'utf8')
 .replace(/^import .*;\r?\n/gm,'').replace(/^export /gm,'');
const sandbox={URL,URLSearchParams,atob,btoa,console:{error(){},warn(){}},link:input.link};
const method=input.link.startsWith('snell://')?'parseSnellUrl':'parseWireguardUrl';
vm.runInNewContext(source+`\nresult=${method}(link);`,sandbox,{timeout:1000});
process.stdout.write(JSON.stringify(sandbox.result));
