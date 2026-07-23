<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { EventsOn } from '../../../../wailsjs/runtime/runtime';
  import { GetMiners } from '../../../../wailsjs/go/main/App';

  // ─── Canvas + container ───────────────────────────────────────────────────
  let container: HTMLDivElement;
  let canvas: HTMLCanvasElement;
  let ctx: CanvasRenderingContext2D;
  let animFrameId = 0;
  let W = 0, H = 0;

  // ─── Globe geometry ───────────────────────────────────────────────────────
  let R = 60;
  let CX = 0, CY = 0;
  let rotX = 0.50;   // ~29° tilt — NA prominent, FirePool in upper face of globe
  let rotY = 3.72;   // Vancouver/FirePool centred horizontally (π/2 - FP_LNG)

  // Mouse drag
  let dragging = false;
  let lastMouseX = 0;

  // ─── Theme accent ─────────────────────────────────────────────────────────
  let accentR = 57, accentG = 255, accentB = 20; // nuclear green default

  function refreshAccent() {
    const v = getComputedStyle(document.documentElement).getPropertyValue('--accent-rgb').trim();
    if (v) {
      const parts = v.split(',').map(Number);
      if (parts.length === 3) { [accentR, accentG, accentB] = parts; }
    }
  }

  // ─── FirePool anchor (Vancouver BC) ───────────────────────────────────────
  const FP_LAT = 49.28 * Math.PI / 180;
  const FP_LNG = -123.12 * Math.PI / 180;
  const FP_COLOR = '#ffd700';

  // ─── Miner state ──────────────────────────────────────────────────────────
  interface MinerDot {
    id: string;
    label: string;
    lat: number;
    lng: number;
    hashrate: number;
    r: number; g: number; b: number;  // unique per-miner color
  }
  let miners: Map<string, MinerDot> = new Map();

  // Golden-angle hue wheel — spreads colors maximally apart
  let colorIndex = 0;
  function nextMinerColor(): { r: number; g: number; b: number } {
    const hue = (colorIndex * 137.508) % 360;
    colorIndex++;
    // HSL → RGB (s=85%, l=62% — vivid but readable on dark bg)
    const s = 0.85, l = 0.62;
    const c = (1 - Math.abs(2*l - 1)) * s;
    const x = c * (1 - Math.abs((hue / 60) % 2 - 1));
    const m = l - c/2;
    let r = 0, g = 0, b = 0;
    if      (hue < 60)  { r=c; g=x; b=0; }
    else if (hue < 120) { r=x; g=c; b=0; }
    else if (hue < 180) { r=0; g=c; b=x; }
    else if (hue < 240) { r=0; g=x; b=c; }
    else if (hue < 300) { r=x; g=0; b=c; }
    else                { r=c; g=0; b=x; }
    return { r: Math.round((r+m)*255), g: Math.round((g+m)*255), b: Math.round((b+m)*255) };
  }

  // ─── Random globe placement ───────────────────────────────────────────────
  // Uniform random point on sphere surface (avoids polar crowding)
  function randomGeoPoint(): { lat: number; lng: number } {
    const lng = (Math.random() * 2 - 1) * Math.PI;           // -π .. π
    const lat = Math.asin(Math.random() * 2 - 1);            // uniform on sphere
    return { lat, lng };
  }

  // ─── Share pulses ─────────────────────────────────────────────────────────
  interface Pulse {
    lat0: number; lng0: number;
    t: number;
    spd: number;
    r: number; g: number; b: number;
  }
  let pulses: Pulse[] = [];

  // ─── FirePool impact bursts ───────────────────────────────────────────────
  // Shockwave rings that expand from FirePool across the globe surface
  let bursts: Burst[] = [];
  let fpFlash = 0;  // 0→1, decays to 0; bumped to 1 on each impact

  // Precompute orthonormal basis at FirePool for shockwave ring math
  // FP unit vector
  const FPX = Math.cos(FP_LAT) * Math.cos(FP_LNG);
  const FPY = Math.cos(FP_LAT) * Math.sin(FP_LNG);
  const FPZ = Math.sin(FP_LAT);
  // u = normalize(cross(FP, north_pole))
  let _ux = FPY, _uy = -FPX, _uz = 0;
  const _ul = Math.sqrt(_ux*_ux + _uy*_uy);
  const FP_UX = _ux/_ul, FP_UY = _uy/_ul, FP_UZ = 0;
  // v = cross(u, FP)
  const FP_VX = FP_UY*FPZ - FP_UZ*FPY;
  const FP_VY = FP_UZ*FPX - FP_UX*FPZ;
  const FP_VZ = FP_UX*FPY - FP_UY*FPX;

  // Store burst color as [r,g,b] so we can easily apply variable alpha
  interface Burst {
    theta: number;
    alpha: number;
    spd: number;
    r: number; g: number; b: number;
  }

  function triggerBurst(r: number, g: number, b: number) {
    // Fast miner-colored ring
    bursts.push({ theta: 0.01, alpha: 1.0, spd: 1.6, r, g, b });
    // Slower gold ring
    bursts.push({ theta: 0.01, alpha: 0.7, spd: 0.9, r: 255, g: 215, b: 0 });
    if (bursts.length > 20) bursts.splice(0, bursts.length - 20);
    fpFlash = 1.0;
  }

  // ─── World atlas geometry ─────────────────────────────────────────────────
  let landRings: [number, number][][] = [];  // array of rings, each ring is [lng_rad, lat_rad][]

  async function loadAtlas() {
    try {
      const resp = await fetch('https://cdn.jsdelivr.net/npm/world-atlas@2/land-110m.json');
      const topo = await resp.json();
      landRings = topoRings(topo, 'land');
    } catch {
      // globe draws without continents — fine
    }
  }

  // Inline minimal TopoJSON decoder
  function topoRings(topo: any, objectName: string): [number, number][][] {
    const obj = topo.objects[objectName];
    const arcs: number[][][] = topo.arcs;
    const scale = topo.transform?.scale ?? [1, 1];
    const translate = topo.transform?.translate ?? [0, 0];

    // Decode arc coordinates
    const decodedArcs: [number, number][][] = arcs.map((arc) => {
      let x = 0, y = 0;
      return arc.map(([dx, dy]) => {
        x += dx; y += dy;
        const lng = (x * scale[0] + translate[0]) * Math.PI / 180;
        const lat = (y * scale[1] + translate[1]) * Math.PI / 180;
        return [lng, lat] as [number, number];
      });
    });

    const rings: [number, number][][] = [];

    function collectGeometry(geom: any) {
      if (!geom) return;
      if (geom.type === 'GeometryCollection') {
        geom.geometries.forEach(collectGeometry);
      } else if (geom.type === 'Polygon') {
        for (const arcIdxs of geom.arcs) rings.push(resolveRing(arcIdxs));
      } else if (geom.type === 'MultiPolygon') {
        for (const poly of geom.arcs)
          for (const arcIdxs of poly) rings.push(resolveRing(arcIdxs));
      }
    }

    function resolveRing(arcIdxs: number[]): [number, number][] {
      const pts: [number, number][] = [];
      for (const idx of arcIdxs) {
        const decoded = idx >= 0 ? decodedArcs[idx] : [...decodedArcs[~idx]].reverse();
        pts.push(...decoded);
      }
      return pts;
    }

    collectGeometry(obj);
    return rings;
  }

  // ─── Projection helpers ───────────────────────────────────────────────────
  // Convert geo coords to 3D unit sphere, apply rotX+rotY, project orthographically
  function latLngTo3D(lat: number, lng: number): [number, number, number] {
    const cosLat = Math.cos(lat);
    let x = cosLat * Math.cos(lng + rotY);
    let y = cosLat * Math.sin(lng + rotY);
    let z = Math.sin(lat);
    // rotX tilt (around X axis)
    const yr = y * Math.cos(rotX) - z * Math.sin(rotX);
    const zr = y * Math.sin(rotX) + z * Math.cos(rotX);
    return [x, yr, zr];
  }

  function project(lat: number, lng: number): { x: number; y: number; depth: number } {
    const [x, y, z] = latLngTo3D(lat, lng);
    // outside-view: subtract x to avoid mirror flip
    return { x: CX - x * R, y: CY - z * R, depth: y };
  }

  function isVisible(lat: number, lng: number): boolean {
    const [, y] = latLngTo3D(lat, lng);
    return y >= 0;
  }

  // ─── SLERP along great circle ─────────────────────────────────────────────
  function slerp(lat0: number, lng0: number, lat1: number, lng1: number, t: number): { lat: number; lng: number } {
    // 3D unit vectors
    const c0l = Math.cos(lat0), c1l = Math.cos(lat1);
    const ax = c0l * Math.cos(lng0), ay = c0l * Math.sin(lng0), az = Math.sin(lat0);
    const bx = c1l * Math.cos(lng1), by = c1l * Math.sin(lng1), bz = Math.sin(lat1);
    const dot = Math.max(-1, Math.min(1, ax * bx + ay * by + az * bz));
    const omega = Math.acos(dot);
    if (omega < 1e-6) return { lat: lat0, lng: lng0 };
    const s = Math.sin(omega);
    const fa = Math.sin((1 - t) * omega) / s;
    const fb = Math.sin(t * omega) / s;
    const cx = fa * ax + fb * bx;
    const cy = fa * ay + fb * by;
    const cz = fa * az + fb * bz;
    return { lat: Math.asin(Math.max(-1, Math.min(1, cz))), lng: Math.atan2(cy, cx) };
  }

  // ─── Hashrate → color ─────────────────────────────────────────────────────
  function hrColor(hashrate: number, alpha = 1): string {
    // Intensity 0–1 based on log scale: 0=1MH/s, 1=10TH/s
    const lo = Math.log10(1e6), hi = Math.log10(10e12);
    const hr = Math.max(hashrate, 1e6);
    const t = Math.min((Math.log10(hr) - lo) / (hi - lo), 1);

    // Lerp from dim (35% accent) to bright white through accent
    if (t < 0.5) {
      const f = t * 2;         // 0→1 over low half
      const dim = 0.35 + f * 0.65;
      return `rgba(${Math.round(accentR * dim)},${Math.round(accentG * dim)},${Math.round(accentB * dim)},${alpha})`;
    } else {
      const f = (t - 0.5) * 2; // 0→1 over high half
      // Blend accent → white
      const r = Math.round(accentR + f * (255 - accentR));
      const g = Math.round(accentG + f * (255 - accentG));
      const b = Math.round(accentB + f * (255 - accentB));
      return `rgba(${r},${g},${b},${alpha})`;
    }
  }

  // ─── Draw ─────────────────────────────────────────────────────────────────
  let lastTime = 0;

  function drawGlobe(ts: number) {
    const dt = Math.min((ts - lastTime) / 1000, 0.05);
    lastTime = ts;

    if (!ctx) { animFrameId = requestAnimationFrame(drawGlobe); return; }

    ctx.clearRect(0, 0, W, H);

    // Auto-rotate when not dragging
    if (!dragging) rotY += 0.003;

    // ── Globe outline ──
    ctx.beginPath();
    ctx.arc(CX, CY, R, 0, Math.PI * 2);
    ctx.strokeStyle = `rgba(${accentR},${accentG},${accentB},0.18)`;
    ctx.lineWidth = 0.8;
    ctx.stroke();

    // ── Land wireframe – two passes (back then front) ──
    for (const pass of [0, 1]) {
      ctx.lineWidth = 0.55;
      for (const ring of landRings) {
        let started = false;
        ctx.beginPath();
        for (const [lng, lat] of ring) {
          const [, y] = latLngTo3D(lat, lng);
          const front = y >= 0;
          if ((pass === 0 && front) || (pass === 1 && !front)) continue;
          const p = project(lat, lng);
          const alpha = pass === 1 ? 0.55 : 0.15; // back hemisphere dimmer
          ctx.strokeStyle = `rgba(${accentR},${accentG},${accentB},${alpha})`;
          if (!started) { ctx.moveTo(p.x, p.y); started = true; }
          else ctx.lineTo(p.x, p.y);
        }
        ctx.stroke();
      }
    }

    // ── Latitude / longitude grid lines (sparse, subtle) ──
    ctx.lineWidth = 0.4;
    ctx.strokeStyle = `rgba(${accentR},${accentG},${accentB},0.07)`;
    for (let latDeg = -60; latDeg <= 60; latDeg += 30) {
      const lat = latDeg * Math.PI / 180;
      ctx.beginPath();
      let started = false;
      for (let ldeg = 0; ldeg <= 360; ldeg += 4) {
        const lng = ldeg * Math.PI / 180;
        const p = project(lat, lng);
        if (!started) { ctx.moveTo(p.x, p.y); started = true; }
        else ctx.lineTo(p.x, p.y);
      }
      ctx.stroke();
    }
    for (let lngDeg = 0; lngDeg < 360; lngDeg += 45) {
      const lng = lngDeg * Math.PI / 180;
      ctx.beginPath();
      let started = false;
      for (let latDeg = -90; latDeg <= 90; latDeg += 3) {
        const lat = latDeg * Math.PI / 180;
        const p = project(lat, lng);
        if (!started) { ctx.moveTo(p.x, p.y); started = true; }
        else ctx.lineTo(p.x, p.y);
      }
      ctx.stroke();
    }

    // ── Advance pulses ──
    for (const p of pulses) p.t += p.spd * dt;
    pulses = pulses.filter(p => {
      if (p.t >= 1) { triggerBurst(p.r, p.g, p.b); return false; }
      return true;
    });

    // ── Advance burst shockwave rings ──
    for (const b of bursts) {
      b.theta += b.spd * dt;
      b.alpha = Math.max(0, 1 - b.theta / Math.PI);
    }
    bursts = bursts.filter(b => b.alpha > 0);

    // ── Draw shockwave rings (before FirePool dot so dot renders on top) ──
    const RING_STEPS = 72;
    for (const burst of bursts) {
      const ct = Math.cos(burst.theta), st = Math.sin(burst.theta);
      ctx.beginPath();
      let rStarted = false;
      for (let i = 0; i <= RING_STEPS; i++) {
        const phi = (i / RING_STEPS) * Math.PI * 2;
        const cp = Math.cos(phi), sp = Math.sin(phi);
        const px = ct*FPX + st*(cp*FP_UX + sp*FP_VX);
        const py = ct*FPY + st*(cp*FP_UY + sp*FP_VY);
        const pz = ct*FPZ + st*(cp*FP_UZ + sp*FP_VZ);
        const lat = Math.asin(Math.max(-1, Math.min(1, pz)));
        const lng = Math.atan2(py, px);
        const [, depth] = latLngTo3D(lat, lng);
        if (depth < 0) { rStarted = false; continue; }
        const rp = project(lat, lng);
        if (!rStarted) { ctx.moveTo(rp.x, rp.y); rStarted = true; }
        else ctx.lineTo(rp.x, rp.y);
      }
      ctx.strokeStyle = `rgba(${burst.r},${burst.g},${burst.b},${burst.alpha * 0.85})`;
      ctx.lineWidth = 1.5 * burst.alpha + 0.3;
      ctx.shadowColor = `rgb(${burst.r},${burst.g},${burst.b})`;
      ctx.shadowBlur = burst.alpha * 12;
      ctx.stroke();
      ctx.shadowBlur = 0;
    }

    // ── Draw arcs + traveling particles ──
    const STEPS = 32;
    for (const pulse of pulses) {
      const col = `rgba(${pulse.r},${pulse.g},${pulse.b}`;

      // Fading trail arc up to current t
      ctx.beginPath();
      let started = false;
      for (let i = 0; i <= STEPS; i++) {
        const frac = (i / STEPS) * pulse.t;
        const mid = slerp(pulse.lat0, pulse.lng0, FP_LAT, FP_LNG, frac);
        const p2 = project(mid.lat, mid.lng);
        const alpha = (i / STEPS) * 0.65 * pulse.t;
        ctx.strokeStyle = `${col},${alpha})`;
        if (!started) { ctx.moveTo(p2.x, p2.y); started = true; }
        else ctx.lineTo(p2.x, p2.y);
      }
      ctx.lineWidth = 1.3;
      ctx.stroke();

      // Traveling particle at head
      const head = slerp(pulse.lat0, pulse.lng0, FP_LAT, FP_LNG, pulse.t);
      if (isVisible(head.lat, head.lng)) {
        const hp = project(head.lat, head.lng);
        ctx.beginPath();
        ctx.arc(hp.x, hp.y, 2.4, 0, Math.PI * 2);
        ctx.fillStyle = `${col},1)`;
        ctx.shadowColor = `rgb(${pulse.r},${pulse.g},${pulse.b})`;
        ctx.shadowBlur = 8;
        ctx.fill();
        ctx.shadowBlur = 0;
      }
    }

    // ── Decay flash ──
    fpFlash = Math.max(0, fpFlash - dt * 2.2);

    // ── FirePool dot ──
    const fp = project(FP_LAT, FP_LNG);
    const fpVisible = isVisible(FP_LAT, FP_LNG);
    if (fpVisible) {
      // Impact bloom — radial gradient that flares on each share arrival
      if (fpFlash > 0) {
        const bloomR = R * 0.55 * fpFlash;
        const grad = ctx.createRadialGradient(fp.x, fp.y, 0, fp.x, fp.y, bloomR);
        grad.addColorStop(0,   `rgba(255,215,0,${fpFlash * 0.7})`);
        grad.addColorStop(0.3, `rgba(255,215,0,${fpFlash * 0.25})`);
        grad.addColorStop(1,   `rgba(255,215,0,0)`);
        ctx.beginPath();
        ctx.arc(fp.x, fp.y, bloomR, 0, Math.PI * 2);
        ctx.fillStyle = grad;
        ctx.fill();
      }

      // Steady slow pulse ring
      const ring = (ts / 1200) % 1;
      ctx.beginPath();
      ctx.arc(fp.x, fp.y, 3 + ring * 7, 0, Math.PI * 2);
      ctx.strokeStyle = `rgba(255,215,0,${0.5 * (1 - ring)})`;
      ctx.lineWidth = 1;
      ctx.stroke();

      // Core dot
      ctx.beginPath();
      ctx.arc(fp.x, fp.y, 3.5 + fpFlash * 2, 0, Math.PI * 2);
      ctx.fillStyle = FP_COLOR;
      ctx.shadowColor = FP_COLOR;
      ctx.shadowBlur = 10 + fpFlash * 20;
      ctx.fill();
      ctx.shadowBlur = 0;

      if (W > 120) {
        ctx.font = `bold ${Math.max(7, R * 0.13)}px monospace`;
        ctx.fillStyle = FP_COLOR;
        ctx.fillText('FIREPOOL', fp.x + 6, fp.y - 5);
      }
    }

    // ── Miner dots ──
    for (const miner of miners.values()) {
      const mp = project(miner.lat, miner.lng);
      const vis = isVisible(miner.lat, miner.lng);
      const alpha = vis ? 1 : 0.25;
      const colStr = `rgba(${miner.r},${miner.g},${miner.b}`;

      ctx.beginPath();
      ctx.arc(mp.x, mp.y, vis ? 2.5 : 1.5, 0, Math.PI * 2);
      ctx.fillStyle = `${colStr},${alpha})`;
      if (vis) {
        ctx.shadowColor = `rgb(${miner.r},${miner.g},${miner.b})`;
        ctx.shadowBlur = 8;
      }
      ctx.fill();
      ctx.shadowBlur = 0;

      if (W > 120 && vis) {
        ctx.font = `${Math.max(7, R * 0.12)}px monospace`;
        ctx.fillStyle = `${colStr},0.9)`;
        ctx.fillText(miner.label, mp.x + 5, mp.y + 4);
      }
    }

    animFrameId = requestAnimationFrame(drawGlobe);
  }

  // ─── Resize ───────────────────────────────────────────────────────────────
  function resize() {
    if (!container || !canvas) return;
    W = container.clientWidth || 64;
    H = container.clientHeight || 200;
    canvas.width = W;
    canvas.height = H;
    canvas.style.width = W + 'px';
    canvas.style.height = H + 'px';
    CX = W / 2;
    CY = H * 0.38;   // upper-third of panel — globe body centred so FirePool sits near top of it
    R = Math.min(W, H) * 0.40;
  }

  // ─── Miner list polling ───────────────────────────────────────────────────
  let minerPollId: ReturnType<typeof setInterval>;

  async function updateMiners() { // async kept for try/await GetMiners
    try {
      const list = await GetMiners();
      const seen = new Set<string>();
      for (const m of list) {
        seen.add(m.id);
        const hr = m.hashrate ?? 0;
        if (miners.has(m.id)) {
          miners.get(m.id)!.hashrate = hr;
        } else {
          const geo = randomGeoPoint();
          const col = nextMinerColor();
          miners.set(m.id, {
            id: m.id,
            label: m.workerName ?? m.id,
            lat: geo.lat,
            lng: geo.lng,
            hashrate: hr,
            ...col,
          });
        }
      }
      // Remove disconnected miners
      for (const id of miners.keys()) {
        if (!seen.has(id)) miners.delete(id);
      }
    } catch {
      // ignore
    }
  }

  // ─── Wails event listeners ────────────────────────────────────────────────
  let cleanupFns: (() => void)[] = [];

  // ─── Lifecycle ────────────────────────────────────────────────────────────
  onMount(() => {
    ctx = canvas.getContext('2d')!;
    refreshAccent();

    resize();
    const ro = new ResizeObserver(resize);
    ro.observe(container);

    loadAtlas();
    updateMiners();
    minerPollId = setInterval(updateMiners, 5000);

    const accentInterval = setInterval(refreshAccent, 2000);

    // Share-accepted event → fire a pulse from that miner
    const offShare = EventsOn('stratum:share-accepted', (data: { minerId: string; difficulty: number }) => {
      const miner = miners.get(data.minerId);
      if (!miner) return;
      pulses.push({ lat0: miner.lat, lng0: miner.lng, t: 0, spd: 0.22 + Math.random() * 0.1, r: miner.r, g: miner.g, b: miner.b });
      if (pulses.length > 40) pulses.splice(0, pulses.length - 40);
    });

    // Miner connected: place at random position with unique color
    const offConnect = EventsOn('stratum:miner-connected', (m: any) => {
      if (!miners.has(m.id)) {
        const geo = randomGeoPoint();
        const col = nextMinerColor();
        miners.set(m.id, {
          id: m.id,
          label: m.workerName ?? m.id,
          lat: geo.lat,
          lng: geo.lng,
          hashrate: m.hashrate ?? 0,
          ...col,
        });
      }
    });

    // Miner disconnected: remove
    const offDisconnect = EventsOn('stratum:miner-disconnected', (data: { id: string }) => {
      miners.delete(data.id);
    });

    cleanupFns = [offShare, offConnect, offDisconnect];

    lastTime = performance.now();
    animFrameId = requestAnimationFrame(drawGlobe);

    return () => {
      ro.disconnect();
      clearInterval(accentInterval);
      clearInterval(minerPollId);
      cancelAnimationFrame(animFrameId);
      cleanupFns.forEach(fn => fn());
    };
  });

  onDestroy(() => {
    cancelAnimationFrame(animFrameId);
    clearInterval(minerPollId);
    cleanupFns.forEach(fn => fn());
  });

  // ─── Mouse interaction ────────────────────────────────────────────────────
  function onMousedown(e: MouseEvent) { dragging = true; lastMouseX = e.clientX; }
  function onMousemove(e: MouseEvent) {
    if (!dragging) return;
    rotY += (e.clientX - lastMouseX) * 0.008;
    lastMouseX = e.clientX;
  }
  function onMouseup() { dragging = false; }
  function onTouchstart(e: TouchEvent) { dragging = true; lastMouseX = e.touches[0].clientX; }
  function onTouchmove(e: TouchEvent) {
    if (!dragging) return;
    rotY += (e.touches[0].clientX - lastMouseX) * 0.008;
    lastMouseX = e.touches[0].clientX;
  }
  function onTouchend() { dragging = false; }
</script>

<!-- Matches BlockchainAnimation container exactly -->
<div
  bind:this={container}
  class="flex-1 relative overflow-hidden select-none pointer-events-auto"
  style="mask-image: linear-gradient(to bottom, transparent 0%, black 12%, black 88%, transparent 100%);
         -webkit-mask-image: linear-gradient(to bottom, transparent 0%, black 12%, black 88%, transparent 100%);"
>
  <canvas
    bind:this={canvas}
    style="display:block; position:absolute; inset:0; cursor:{dragging ? 'grabbing' : 'grab'};"
    on:mousedown={onMousedown}
    on:mousemove={onMousemove}
    on:mouseup={onMouseup}
    on:mouseleave={onMouseup}
    on:touchstart|passive={onTouchstart}
    on:touchmove|passive={onTouchmove}
    on:touchend={onTouchend}
  />
</div>
