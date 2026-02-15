<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  let canvas: HTMLCanvasElement;
  let container: HTMLDivElement;
  let ctx: CanvasRenderingContext2D;
  let rafId: number;
  let lastTs = 0;

  const HEX = '0123456789abcdef';
  const FONT_SIZE = 10;
  const COL_W = 13;
  const ROW_H = 14;

  interface Drop {
    y: number;           // head position in rows (float)
    speed: number;       // rows per second
    length: number;      // trail length in rows
    chars: string[];     // character pool (one per row)
    changeTimer: number; // ms until next random char swap
  }

  let drops: Drop[] = [];
  let accentRGB = [57, 255, 20]; // fallback
  let prevCols = 0;

  function randomHex(): string {
    return HEX[Math.floor(Math.random() * 16)];
  }

  function createDrop(rows: number, scatter: boolean): Drop {
    const length = 4 + Math.floor(Math.random() * Math.max(4, rows * 0.5));
    const chars = Array.from({ length: rows + length }, () => randomHex());
    return {
      y: scatter ? -length + Math.random() * (rows + length) : -Math.random() * 8,
      speed: 1 + Math.random() * 2.5,
      length,
      chars,
      changeTimer: 60 + Math.random() * 180,
    };
  }

  function updateAccentColor() {
    if (!container) return;
    const rgb = getComputedStyle(container).getPropertyValue('--accent-rgb').trim();
    if (rgb) {
      const parts = rgb.split(',').map(s => parseInt(s.trim()));
      if (parts.length === 3 && parts.every(n => !isNaN(n))) {
        accentRGB = parts;
      }
    }
  }

  function frame(ts: number) {
    if (!canvas || !ctx || !container) {
      rafId = requestAnimationFrame(frame);
      return;
    }

    const w = container.clientWidth;
    const h = container.clientHeight;
    if (w < 20 || h < 40) {
      rafId = requestAnimationFrame(frame);
      return;
    }

    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w;
      canvas.height = h;
      updateAccentColor();
    }

    const cols = Math.max(1, Math.floor(w / COL_W));
    const rows = Math.floor(h / ROW_H);

    // Re-init columns if count changed
    if (cols !== prevCols) {
      drops = Array.from({ length: cols }, () => createDrop(rows, true));
      prevCols = cols;
    }

    const dt = lastTs ? (ts - lastTs) / 1000 : 0;
    lastTs = ts;
    if (dt > 0.15) { rafId = requestAnimationFrame(frame); return; }

    const [cr, cg, cb] = accentRGB;

    ctx.clearRect(0, 0, w, h);
    ctx.font = `${FONT_SIZE}px "JetBrains Mono", "Share Tech Mono", monospace`;
    ctx.textAlign = 'center';

    for (let col = 0; col < cols; col++) {
      const drop = drops[col];

      // Advance head
      drop.y += drop.speed * dt;

      // Randomly swap a character in the trail (hashing feel)
      drop.changeTimer -= dt * 1000;
      if (drop.changeTimer <= 0) {
        const idx = Math.floor(Math.random() * drop.chars.length);
        drop.chars[idx] = randomHex();
        drop.changeTimer = 60 + Math.random() * 180;
      }

      const headRow = Math.floor(drop.y);
      const x = col * COL_W + COL_W / 2;

      for (let row = 0; row < rows; row++) {
        const dist = headRow - row;
        if (dist < 0 || dist >= drop.length) continue;

        const t = dist / drop.length;
        let alpha: number;
        if (dist === 0) {
          alpha = 0.55;
        } else if (dist <= 2) {
          alpha = 0.3;
        } else {
          alpha = Math.max(0.04, 0.2 * (1 - t));
        }

        ctx.fillStyle = `rgba(${cr}, ${cg}, ${cb}, ${alpha})`;
        ctx.fillText(drop.chars[row % drop.chars.length], x, row * ROW_H + ROW_H);
      }

      // Reset when trail fully off screen
      if (headRow - drop.length > rows) {
        drops[col] = createDrop(rows, false);
      }
    }

    rafId = requestAnimationFrame(frame);
  }

  let colorInterval: ReturnType<typeof setInterval>;

  onMount(() => {
    ctx = canvas?.getContext('2d')!;
    rafId = requestAnimationFrame(frame);
    colorInterval = setInterval(updateAccentColor, 2000);
  });

  onDestroy(() => {
    if (rafId) cancelAnimationFrame(rafId);
    if (colorInterval) clearInterval(colorInterval);
  });
</script>

<div
  bind:this={container}
  class="flex-1 relative overflow-hidden select-none pointer-events-none"
  style="min-height: 40px; -webkit-mask-image: linear-gradient(to bottom, transparent 0%, black 8%, black 92%, transparent 100%); mask-image: linear-gradient(to bottom, transparent 0%, black 8%, black 92%, transparent 100%);"
>
  <canvas bind:this={canvas} class="absolute inset-0 w-full h-full" />
</div>
