// Web-build runtime shim. Replaces wailsjs/runtime/runtime for the headless web
// build: Wails events are delivered over a single Server-Sent-Events stream
// (GET /api/events). Only the pieces the frontend uses are implemented; the rest
// are harmless stubs so any stray import still resolves.

let source = null;
const listeners = new Map(); // event name -> Set<callback>

function ensureSource() {
  if (source) return;
  source = new EventSource('/api/events');
  source.onerror = () => { /* EventSource auto-reconnects */ };
}

// EventsOn subscribes to a named backend event. The SSE payload is JSON; we
// parse it and pass it as the single callback arg (matching typical Wails use).
// Returns an unsubscribe function.
export function EventsOn(name, callback) {
  ensureSource();
  let set = listeners.get(name);
  if (!set) {
    set = new Set();
    listeners.set(name, set);
    source.addEventListener(name, (e) => {
      let data;
      try { data = e.data ? JSON.parse(e.data) : undefined; } catch { data = e.data; }
      const cbs = listeners.get(name);
      if (cbs) cbs.forEach((cb) => cb(data));
    });
  }
  set.add(callback);
  return () => EventsOff(name, callback);
}

export function EventsOnce(name, callback) {
  const off = EventsOn(name, (data) => { off(); callback(data); });
  return off;
}

export function EventsOff(name, callback) {
  const set = listeners.get(name);
  if (!set) return;
  if (callback) set.delete(callback);
  else set.clear();
}

// Frontend->backend emit is unused today; POST it best-effort so nothing breaks.
export function EventsEmit(name, ...data) {
  fetch('/api/emit/' + encodeURIComponent(name), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  }).catch(() => {});
}
