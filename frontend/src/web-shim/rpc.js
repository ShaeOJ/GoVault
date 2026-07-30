// Web-build RPC transport. Replaces the Wails generated bindings: every bound
// App method becomes a POST /api/rpc/<Method> with a JSON array of args. Used
// only in the headless web build (see vite.config.web.ts alias).

export async function rpc(method, args) {
  const res = await fetch('/api/rpc/' + method, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(args || []),
  });
  if (!res.ok) {
    let msg = 'RPC ' + method + ' failed (' + res.status + ')';
    try {
      const j = await res.json();
      if (j && j.error) msg = j.error;
    } catch { /* non-JSON error body */ }
    throw new Error(msg);
  }
  const text = await res.text();
  return text ? JSON.parse(text) : undefined;
}
