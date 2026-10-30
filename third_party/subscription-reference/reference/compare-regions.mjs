import {readFileSync} from 'node:fs';
import {extractNodeRegion} from './geo-utils.mjs';
for (const sample of JSON.parse(readFileSync(process.argv[2], 'utf8'))) {
 const actual=extractNodeRegion(sample.name);
 if(actual!==sample.expected) throw new Error(JSON.stringify({sample,actual}));
}
