// Executed by ego-browser; use the production measurement module and TLS checks.
export async function measure({ control, base, output, name, space }) {
  const task = await taskSpace(space ?? name);
  const page = task.page('p1');
  await page.goto(`${base}/gates`);
  const result = await page.evaluate(async ({ control }) => {
    const mod = await import('/src/lib/browser-test.ts');
    const outcomes = [], ids = {};
    async function run(session) {
      const abort = new AbortController();
      const timer = setTimeout(() => abort.abort(), 15000);
      try { return await mod.measureBrowserPath(session, abort.signal); }
      catch (error) { return { status: 'failed', reason: error instanceof Error ? error.message : String(error), bytes: 0 }; }
      finally { clearTimeout(timer); }
    }
    for (const purpose of ['https', 'webrtc', 'webtransport']) {
      const response = await fetch(`${control}/session?purpose=${purpose}`, { cache: 'no-store' });
      if (!response.ok) throw new Error(`session status ${response.status}`);
      const session = mod.browserSession(await response.json());
      ids[purpose] = session.id;
      if (purpose === 'webtransport') {
        if (!session.certificate_hash) throw new Error('QUIC fixture needs a short-lived P-256 certificate');
        const wrong = session.certificate_hash[0] === '0' ? '1' : '0';
        const rejected = await run({ ...session, certificate_hash: wrong + session.certificate_hash.slice(1) });
        outcomes.push({ purpose: 'wrong-certificate', status: rejected.status === 'failed' ? 'passed' : 'failed', reason: rejected.status === 'failed' ? '证书指纹不符被拒绝' : '错误证书未拒绝', bytes: rejected.bytes });
      }
      outcomes.push({ purpose, ...await run(session) });
    }
    const response = await fetch(`${control}/usage`, { cache: 'no-store' });
    if (!response.ok) throw new Error(`usage status ${response.status}`);
    const usage = await response.json();
    const expected = { https: 4194368, webrtc: 72, webtransport: 128 };
    const accounting = Object.entries(expected).every(([purpose, bytes]) => usage.sessions.some(row => row.id === ids[purpose] && row.bytes === bytes));
    return { origin: location.origin, secure_context: isSecureContext, outcomes, accounting, usage };
  }, { control });
  const fs = await import('node:fs/promises');
  await fs.writeFile(output, JSON.stringify(result, null, 2));
  console.log({ space: task.spaceId, ...result });
  // An existing task space can still be needed by the rest of the acceptance.
  if (space === undefined) await task.finish({ keep: [] });
  if (!result.accounting || result.outcomes.some(row => row.status !== 'passed')) throw new Error('Browser protocol scenario failed; see retained results');
}
