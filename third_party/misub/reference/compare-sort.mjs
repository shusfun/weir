import {readFileSync} from 'node:fs';
import {makeComparator} from './comparator.mjs';
const samples=JSON.parse(readFileSync(process.argv[2],'utf8'));
// Model the center's documented deterministic en-US runtime rather than the
// operator workstation's OS locale. The upstream comparator is unchanged.
const compare=String.prototype.localeCompare;
String.prototype.localeCompare=function(other,locale,options){return compare.call(this,other,locale??'en-US',options)};
for(const sample of samples){
 const actual=[...sample.records].sort(makeComparator(sample.params)).map(r=>r._id);
 if(JSON.stringify(actual)!==JSON.stringify(sample.expected))throw Error(JSON.stringify({params:sample.params,actual,expected:sample.expected}));
}
console.log(samples.length+' pinned upstream sort configurations agree');
