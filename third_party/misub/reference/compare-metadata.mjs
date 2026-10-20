import {extractNodeMetadata} from "./metadata-extractor.mjs";
let s="";for await(const c of process.stdin)s+=c;console.log(JSON.stringify(JSON.parse(s).map(name=>{const m=extractNodeMetadata(name);return {originalName:m.originalName,cleanName:m.cleanName,multiplier:m.multiplier};})));
