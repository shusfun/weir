const { spawnSync } = require('node:child_process');
const path = require('node:path');
const root=path.resolve(__dirname,'..');
const linux=process.platform==='linux';
const args=linux?['test','./...']:['run','--rm','--mount',`type=bind,source=${root},target=/src,readonly`,'-v','migate-go-mod-cache:/go/pkg/mod','-v','migate-go-build-cache:/root/.cache/go-build','-w','/src',process.env.MIGATE_GO_TEST_IMAGE||'golang:1.25','go','test','./...'];
const result=spawnSync(linux?'go':'docker',args,{cwd:root,stdio:'inherit'});
if(result.error)console.error(result.error.message);
process.exitCode=result.status??1;
