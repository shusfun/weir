import { test } from "node:test";
import assert from "node:assert/strict";
import { parseExitText } from "../src/lib/exit-parse.ts";
const proxy = (text) => { const r = parseExitText(text); assert.equal(r.type, "proxy"); if (r.type !== "proxy") throw Error("expected proxy"); return r; };
test("供应商多行说明及 HTTP CONNECT 不误判为 HTTPS", () => {
  const r = proxy("供应商：Example\n协议：HTTP（支持 HTTPS CONNECT）\n代理入口：203.0.113.11\n端口：58245\n用户名：demo\n密码：p:a@ss，；");
  assert.deepEqual(r.proxy, { kind: "http", address: "203.0.113.11", port: 58245, username: "demo", password: "p:a@ss，；" });
});
test("英文标签、CRLF、明确协议、密码空格", () => {
  const r = proxy("Protocol: SOCKS5\r\nHost: proxy.example\r\nPort: 1080\r\nUsername: demo\r\nPassword: x y:＠  ");
  assert.equal(r.proxy.kind,"socks5"); assert.equal(r.proxy.password,"x y:＠  ");
  assert.equal(proxy("Protocol: HTTPS\nIP: proxy.example\nPort: 443").proxy.kind,"https");
});
test("单行格式、默认 HTTP、IPv6、URL 解码", () => {
  for (const s of ["proxy.example:8080", "proxy.example 8080", "proxy.example\t8080\tdemo\tp:a@ss", "proxy.example\n8080\ndemo\np:a@ss"]) assert.equal(proxy(s).proxy.kind,"http");
  assert.match(proxy("proxy.example:8080").notice,/未提供协议/);
  assert.equal(proxy("\nproxy.example:8080\r\n").proxy.port,8080);
  assert.equal(proxy("proxy.example:8080:demo:pass  \n").proxy.password,"pass  ");
  assert.match(proxy("proxy.example:8080").notice,/认证信息/);
  for (const s of ["proxy.example:8080:demo:p:a@ss", "demo:p:a@ss@proxy.example:8080"]) assert.equal(proxy(s).proxy.password,"p:a@ss");
  assert.equal(proxy("[2001:db8::1]:8080:demo:p:a@ss").proxy.address,"2001:db8::1");
  const r=proxy("https://demo:p%3Aa%40ss@proxy.example:443"); assert.equal(r.proxy.password,"p:a@ss"); assert.equal(r.proxy.kind,"https");
  assert.equal(proxy("socks5://demo:pass@[2001:db8::1]:1080").proxy.kind,"socks5");
});
test("无效、缺失、冲突、多个代理与歧义拒绝，错误不含原文", () => {
  for (const s of ["Host: proxy.example", "Port: 8080", "Host: proxy.example\nPort: 0", "Host: proxy.example\nPort: 65536", "Host: proxy.example\nPort: eight", "IP: one.example\nHost: two.example\nPort: 80", "Host: proxy.example:80\nPort: 81", "Host: proxy.example\nPort: 80\nhttp://other.example:80", "one.example:80\ntwo.example:81", "proxy.example 80 demo pass extra", "http://demo:%ZZ@proxy.example:80", "Protocol: maybe\nHost: proxy.example\nPort: 80"]) {
    const r=parseExitText(s); assert.equal(r.type,"error",s); if(r.type==='error') assert.ok(!r.message.includes('demo:'));
  }
});
test("节点链接与订阅判断保持", () => {
  assert.equal(parseExitText("trojan://fake@proxy.example:443").type,"link");
  assert.equal(parseExitText("https://example.com/sub/token").type,"subscription");
  assert.equal(parseExitText("https://example.com/?token=fake").type,"subscription");
});
