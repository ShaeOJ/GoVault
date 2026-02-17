<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { miners, selectedMiner, discoveredMiners } from '../lib/stores/miners';
  import { formatHashrate, formatDifficulty, formatChance, formatPower, formatCurrency, formatEfficiency, timeAgo } from '../lib/utils/format';
  import type { MinerInfo, DiscoveredMiner } from '../lib/stores/miners';
  import { EventsOn } from '../../wailsjs/runtime/runtime';
  import Icon from '../lib/components/common/Icon.svelte';
  import Info from '../lib/components/common/Info.svelte';
  import ThemedSpinner from '../lib/components/common/ThemedSpinner.svelte';

  interface SparkPoint { t: number; h: number; }

  let minerList: MinerInfo[] = [];
  let showPanel = false;
  let selected: MinerInfo | null = null;
  let scanning = false;
  let discovered: DiscoveredMiner[] = [];
  let showDiscovery = false;
  let unsubs: (() => void)[] = [];
  let refreshInterval: ReturnType<typeof setInterval>;
  let sparklineInterval: ReturnType<typeof setInterval>;

  // Fleet overview stats
  interface FleetOverviewData {
    totalHashrate: number;
    blockChance: number;
    totalWatts: number;
    powerResponded: number;
    powerQueried: number;
    dailyCost: number;
    electricityCost: number;
    efficiency: number;
  }
  let fleet: FleetOverviewData | null = null;

  // Per-miner sparkline data cache
  let minerSparklines: Record<string, SparkPoint[]> = {};

  const unsubMiners = miners.subscribe(m => {
    minerList = [...m].sort((a, b) => a.connectedAt.localeCompare(b.connectedAt) || a.id.localeCompare(b.id));
  });
  const unsubSelected = selectedMiner.subscribe(m => selected = m);
  const unsubDiscovered = discoveredMiners.subscribe(d => discovered = d);

  // SVG sparkline helpers (no Chart.js)
  function sparklinePath(points: SparkPoint[], width: number, height: number): string {
    if (points.length < 2) return '';
    const maxH = Math.max(...points.map(p => p.h));
    if (maxH <= 0) return '';
    const step = width / (points.length - 1);
    return points.map((p, i) => {
      const x = i * step;
      const y = height - (p.h / maxH) * height * 0.85 - height * 0.05;
      return `${x},${y}`;
    }).join(' ');
  }

  function sparklineArea(points: SparkPoint[], width: number, height: number): string {
    if (points.length < 2) return '';
    const line = sparklinePath(points, width, height);
    if (!line) return '';
    return `0,${height} ${line} ${width},${height}`;
  }

  async function refreshSparklines() {
    if (minerList.length === 0) return;
    try {
      const { GetMinerHashrateHistory } = await import('../../wailsjs/go/main/App');
      const results = await Promise.all(
        minerList.map(async (m) => {
          const pts = await GetMinerHashrateHistory(m.id);
          return { id: m.id, pts: pts || [] };
        })
      );
      const next: Record<string, SparkPoint[]> = {};
      for (const r of results) {
        next[r.id] = r.pts;
      }
      minerSparklines = next;
    } catch {}
  }

  onMount(async () => {
    unsubs.push(EventsOn('stratum:miner-connected', (info: MinerInfo) => {
      miners.update(list => [...list.filter(m => m.id !== info.id), info]);
    }));

    unsubs.push(EventsOn('stratum:miner-disconnected', ({ id }: { id: string }) => {
      miners.update(list => list.filter(m => m.id !== id));
    }));

    unsubs.push(EventsOn('stratum:share-accepted', ({ minerId, difficulty }: { minerId: string; difficulty: number }) => {
      miners.update(list => list.map(m =>
        m.id === minerId ? { ...m, sharesAccepted: m.sharesAccepted + 1, bestDifficulty: Math.max(m.bestDifficulty, difficulty) } : m
      ));
    }));

    unsubs.push(EventsOn('stratum:share-rejected', ({ minerId }: { minerId: string }) => {
      miners.update(list => list.map(m =>
        m.id === minerId ? { ...m, sharesRejected: m.sharesRejected + 1 } : m
      ));
    }));

    await refreshMiners();
    await refreshSparklines();

    refreshInterval = setInterval(refreshMiners, 5000);
    sparklineInterval = setInterval(refreshSparklines, 30000);
  });

  async function refreshMiners() {
    try {
      const { GetMiners, GetFleetOverview } = await import('../../wailsjs/go/main/App');
      const [m, fo] = await Promise.all([GetMiners(), GetFleetOverview()]);
      if (m) miners.set(m);
      if (fo) fleet = fo;
    } catch {}
  }

  onDestroy(() => {
    unsubMiners();
    unsubSelected();
    unsubDiscovered();
    unsubs.forEach(fn => fn());
    if (refreshInterval) clearInterval(refreshInterval);
    if (sparklineInterval) clearInterval(sparklineInterval);
  });

  function selectMiner(m: MinerInfo) {
    selectedMiner.set(m);
    showPanel = true;
  }

  function closePanel() {
    showPanel = false;
    setTimeout(() => selectedMiner.set(null), 300);
  }

  async function scanNetwork() {
    scanning = true;
    try {
      const { ScanForMiners } = await import('../../wailsjs/go/main/App');
      const results = await ScanForMiners();
      discoveredMiners.set(results || []);
      showDiscovery = true;
    } catch (e) {
      console.error('Scan failed:', e);
    }
    scanning = false;
  }

  async function configureMiner(ip: string) {
    try {
      const { ConfigureMiner } = await import('../../wailsjs/go/main/App');
      await ConfigureMiner(ip);
    } catch (e) {
      console.error('Configure failed:', e);
    }
  }

  function getMinerStatus(m: MinerInfo): { color: string; label: string; glow: boolean } {
    if (!m.lastShareTime || m.lastShareTime === '0001-01-01T00:00:00Z') {
      return { color: 'var(--text-secondary)', label: 'Idle', glow: false };
    }
    const elapsed = (Date.now() - new Date(m.lastShareTime).getTime()) / 1000;

    let staleThreshold = 120;
    if (m.hashrate > 0 && m.currentDiff > 0) {
      const expectedTime = (m.currentDiff * 4294967296) / m.hashrate;
      staleThreshold = Math.max(120, expectedTime * 3);
    } else {
      staleThreshold = 180;
    }
    const deadThreshold = staleThreshold * 3;

    if (elapsed < staleThreshold) return { color: 'var(--success)', label: 'Active', glow: true };
    if (elapsed < deadThreshold) return { color: 'var(--warning)', label: 'Stale', glow: false };
    return { color: 'var(--error)', label: 'Dead', glow: false };
  }

  function getUptime(connectedAt: string): string {
    if (!connectedAt) return '';
    const diff = Math.floor((Date.now() - new Date(connectedAt).getTime()) / 1000);
    if (diff < 60) return `${diff}s`;
    if (diff < 3600) return `${Math.floor(diff / 60)}m`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ${Math.floor((diff % 3600) / 60)}m`;
    return `${Math.floor(diff / 86400)}d ${Math.floor((diff % 86400) / 3600)}h`;
  }
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-bold font-tech uppercase tracking-wide" style="color: var(--text-primary);">Miners</h1>
      <p class="text-sm" style="color: var(--text-secondary);">{minerList.length} connected</p>
    </div>
    <button
      class="px-4 py-2 rounded-lg font-medium text-sm font-tech uppercase tracking-wider transition-all duration-200 glow-border-hover flex items-center gap-2"
      style="background: rgba(var(--accent-rgb), 0.05); color: var(--accent); border: 1px solid var(--accent);"
      on:click={scanNetwork}
      disabled={scanning}
    >
      {#if scanning}
        <ThemedSpinner size={16} />
        Scanning...
      {:else}
        <Icon name="waves" size={16} />
        Scan Network
      {/if}
    </button>
  </div>

  <!-- Fleet Overview -->
  {#if minerList.length > 0 && fleet}
    <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
      <!-- Total Hashrate -->
      <div class="rounded-xl p-4 card-glow" style="background-color: var(--bg-card);">
        <div class="flex items-center gap-2 mb-2">
          <span style="color: var(--accent);"><Icon name="bolt" size={18} /></span>
          <span class="text-xs font-tech uppercase tracking-wider" style="color: var(--text-secondary);">Total Hashrate</span>
        </div>
        <div class="text-xl font-bold data-readout">{formatHashrate(fleet.totalHashrate)}</div>
      </div>

      <!-- Block Chance -->
      <div class="rounded-xl p-4 card-glow" style="background-color: var(--bg-card);">
        <div class="flex items-center gap-2 mb-2">
          <span style="color: var(--warning);"><Icon name="dice" size={18} /></span>
          <span class="text-xs font-tech uppercase tracking-wider" style="color: var(--text-secondary);">Block Chance</span>
        </div>
        <div class="text-xl font-bold font-data" style="color: var(--warning);">{formatChance(fleet.blockChance)}</div>
        <div class="text-xs mt-1" style="color: var(--text-secondary);">per day</div>
      </div>

      <!-- Fleet Power -->
      <div class="rounded-xl p-4 card-glow" style="background-color: var(--bg-card);">
        <div class="flex items-center gap-2 mb-2">
          <span style="color: var(--accent);"><Icon name="power" size={18} /></span>
          <span class="text-xs font-tech uppercase tracking-wider" style="color: var(--text-secondary);">Fleet Power</span>
        </div>
        <div class="text-xl font-bold font-data" style="color: var(--text-primary);">{formatPower(fleet.totalWatts)}</div>
        <div class="text-xs mt-1" style="color: var(--text-secondary);">
          {#if fleet.powerResponded > 0}
            {formatEfficiency(fleet.totalWatts, fleet.totalHashrate)} &middot; {fleet.powerResponded} of {fleet.powerQueried} miners
          {:else}
            N/A
          {/if}
        </div>
      </div>

      <!-- Daily Cost -->
      <div class="rounded-xl p-4 card-glow" style="background-color: var(--bg-card);">
        <div class="flex items-center gap-2 mb-2">
          <span style="color: var(--success);"><Icon name="dollar" size={18} /></span>
          <span class="text-xs font-tech uppercase tracking-wider" style="color: var(--text-secondary);">Daily Cost</span>
        </div>
        <div class="text-xl font-bold font-data" style="color: var(--success);">
          {fleet.powerResponded > 0 ? formatCurrency(fleet.dailyCost) : 'N/A'}
        </div>
        <div class="text-xs mt-1" style="color: var(--text-secondary);">
          {#if fleet.powerResponded > 0}
            at ${fleet.electricityCost.toFixed(2)}/kWh
          {:else}
            no AxeOS miners
          {/if}
        </div>
      </div>
    </div>
  {/if}

  <!-- Discovered Miners -->
  {#if showDiscovery && discovered.length > 0}
    <div class="rounded-xl p-5 card-glow" style="background-color: var(--bg-card); border-color: rgba(var(--accent-rgb), 0.2);">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium font-tech uppercase tracking-wider" style="color: var(--accent);">Discovered on Network</h3>
        <button style="color: var(--text-secondary);" class="hover:opacity-80" on:click={() => showDiscovery = false}>
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
          </svg>
        </button>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        {#each discovered as dm}
          <div class="rounded-lg p-3 flex items-center justify-between" style="background-color: var(--bg-secondary);">
            <div>
              <div class="text-sm font-medium" style="color: var(--text-primary);">{dm.hostname || dm.ip}</div>
              <div class="text-xs" style="color: var(--text-secondary);">{dm.model} | {dm.hashrate.toFixed(1)} GH/s | {dm.temperature.toFixed(0)}C</div>
              <div class="text-xs" style="color: var(--text-secondary); opacity: 0.7;">Pool: {dm.currentPool}</div>
            </div>
            <button
              class="px-3 py-1.5 text-xs rounded-lg font-tech uppercase tracking-wider transition-colors glow-border-hover"
              style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid rgba(var(--accent-rgb), 0.3);"
              on:click={() => configureMiner(dm.ip)}
            >
              Connect
            </button>
          </div>
        {/each}
      </div>
    </div>
  {/if}

  <!-- Connected Miners -->
  {#if minerList.length === 0}
    <div class="rounded-xl p-12 card-glow text-center" style="background-color: var(--bg-card);">
      <div class="mx-auto mb-4" style="color: var(--text-secondary); opacity: 0.5;">
        <Icon name="chip" size={48} />
      </div>
      <h3 class="font-medium mb-2 font-tech" style="color: var(--text-secondary);">No miners connected</h3>
      <p class="text-sm" style="color: var(--text-secondary); opacity: 0.7;">Point your miners to the stratum server to see them here</p>
    </div>
  {:else}
    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {#each minerList as m (m.id)}
        {@const status = getMinerStatus(m)}
        {@const points = minerSparklines[m.id] || []}
        <button
          class="rounded-xl card-glow text-left w-full overflow-hidden"
          style="background-color: var(--bg-card);"
          on:click={() => selectMiner(m)}
        >
          <!-- Header -->
          <div class="flex items-center justify-between px-4 pt-4 pb-2">
            <div class="flex items-center gap-2 min-w-0">
              <div
                class="w-2.5 h-2.5 rounded-full flex-shrink-0 {status.glow ? 'status-pulse' : ''}"
                style="background-color: {status.color}; {status.glow ? `box-shadow: 0 0 6px ${status.color};` : ''}"
              ></div>
              <span class="text-sm font-bold truncate" style="color: var(--text-primary);">{m.workerName || m.id}</span>
            </div>
            <span class="text-xs font-data glow-text flex-shrink-0 ml-2 inline-flex items-center gap-1" style="color: {status.color}; text-shadow: 0 0 4px {status.color}40;">{status.label} <Info tip="Active: recent shares. Stale: no shares for extended period. Dead: connection likely lost" size={11} /></span>
          </div>

          <!-- Hero Hashrate with Sparkline -->
          <div class="relative px-4 py-3">
            <!-- Sparkline background -->
            {#if points.length >= 2}
              <svg
                class="absolute inset-0 w-full h-full"
                preserveAspectRatio="none"
                viewBox="0 0 200 60"
              >
                <polygon
                  points={sparklineArea(points, 200, 60)}
                  fill="var(--accent)"
                  opacity="0.03"
                />
                <polyline
                  points={sparklinePath(points, 200, 60)}
                  fill="none"
                  stroke="var(--accent)"
                  stroke-width="1"
                  opacity="0.12"
                />
              </svg>
            {/if}
            <!-- Hashrate readout -->
            <div class="relative flex items-center gap-2">
              <span style="color: var(--accent); opacity: 0.6;"><Icon name="bolt" size={20} /></span>
              <span class="text-2xl font-bold data-readout">{formatHashrate(m.hashrate)}</span>
            </div>
          </div>

          <!-- Stats Row -->
          <div class="grid grid-cols-3 gap-2 px-4 pb-3">
            <div>
              <div class="text-xs inline-flex items-center gap-0.5" style="color: var(--text-secondary);">Best Diff <Info tip="Highest difficulty share submitted by this miner" size={10} /></div>
              <div class="text-sm font-medium font-data" style="color: var(--warning);">{formatDifficulty(m.bestDifficulty)}</div>
            </div>
            <div>
              <div class="text-xs inline-flex items-center gap-0.5" style="color: var(--text-secondary);">Accepted <Info tip="Valid shares accepted by the server" size={10} /></div>
              <div class="text-sm font-medium font-data" style="color: var(--success);">{m.sharesAccepted}</div>
            </div>
            <div>
              <div class="text-xs inline-flex items-center gap-0.5" style="color: var(--text-secondary);">Rejected <Info tip="Invalid or stale shares rejected" size={10} /></div>
              <div class="text-sm font-medium font-data" style="color: {m.sharesRejected > 0 ? 'var(--error)' : 'var(--text-secondary)'};">{m.sharesRejected}</div>
            </div>
          </div>

          <!-- Footer -->
          <div class="flex justify-between text-xs px-4 py-2.5" style="border-top: 1px solid var(--border); color: var(--text-secondary);">
            <span>{m.ipAddress}</span>
            <span>{getUptime(m.connectedAt)}</span>
          </div>
        </button>
      {/each}
    </div>
  {/if}
</div>

<!-- Detail Slide Panel -->
{#if selected}
  <!-- Backdrop -->
  <div
    class="fixed inset-0 bg-black/40 z-40 transition-opacity {showPanel ? 'opacity-100' : 'opacity-0 pointer-events-none'}"
    on:click={closePanel}
    on:keydown={(e) => e.key === 'Escape' && closePanel()}
    role="button"
    tabindex="-1"
  ></div>

  <!-- Panel -->
  <div
    class="fixed right-0 top-0 bottom-0 w-96 z-50 slide-panel {showPanel ? 'open' : ''} overflow-y-auto"
    style="background-color: var(--bg-secondary); border-left: 1px solid var(--border);"
  >
    <!-- Top accent line -->
    <div class="h-[2px] w-full" style="background: var(--accent); box-shadow: 0 0 6px var(--accent-glow);"></div>

    <div class="p-6">
      <div class="flex items-center justify-between mb-6">
        <h2 class="text-lg font-bold font-tech uppercase tracking-wider" style="color: var(--text-primary);">Miner Details</h2>
        <button style="color: var(--text-secondary);" class="hover:opacity-80" on:click={closePanel}>
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
          </svg>
        </button>
      </div>

      <div class="space-y-4">
        <div class="rounded-lg p-4" style="background-color: var(--bg-card);">
          <div class="text-xs mb-1 inline-flex items-center gap-1" style="color: var(--text-secondary);">Worker Name <Info tip="Identifier set by the miner firmware" size={11} /></div>
          <div class="text-sm font-medium font-data break-all" style="color: var(--text-primary);">{selected.workerName}</div>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div class="rounded-lg p-3" style="background-color: var(--bg-card);">
            <div class="text-xs mb-1 inline-flex items-center gap-1" style="color: var(--text-secondary);">Hashrate <Info tip="Estimated from share submission rate and difficulty" size={11} /></div>
            <div class="text-sm font-medium data-readout">{formatHashrate(selected.hashrate)}</div>
          </div>
          <div class="rounded-lg p-3" style="background-color: var(--bg-card);">
            <div class="text-xs mb-1 inline-flex items-center gap-1" style="color: var(--text-secondary);">Difficulty <Info tip="Current assigned difficulty, adjusted by vardiff" size={11} /></div>
            <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatDifficulty(selected.currentDiff)}</div>
          </div>
          <div class="rounded-lg p-3" style="background-color: var(--bg-card);">
            <div class="text-xs mb-1 inline-flex items-center gap-1" style="color: var(--text-secondary);">Accepted <Info tip="Valid shares accepted by the server" size={11} /></div>
            <div class="text-sm font-medium font-data" style="color: var(--success);">{selected.sharesAccepted}</div>
          </div>
          <div class="rounded-lg p-3" style="background-color: var(--bg-card);">
            <div class="text-xs mb-1 inline-flex items-center gap-1" style="color: var(--text-secondary);">Rejected <Info tip="Invalid or stale shares rejected" size={11} /></div>
            <div class="text-sm font-medium font-data" style="color: var(--error);">{selected.sharesRejected}</div>
          </div>
        </div>

        <div class="rounded-lg p-4" style="background-color: var(--bg-card);">
          <div class="text-xs mb-1 inline-flex items-center gap-1" style="color: var(--text-secondary);">Best Difficulty <Info tip="Highest difficulty share submitted by this miner" size={11} /></div>
          <div class="text-sm font-medium data-readout" style="color: var(--warning);">{formatDifficulty(selected.bestDifficulty)}</div>
        </div>

        <div class="rounded-lg p-4 space-y-2" style="background-color: var(--bg-card);">
          <div>
            <div class="text-xs" style="color: var(--text-secondary);">IP Address</div>
            <div class="text-sm font-data" style="color: var(--text-primary);">{selected.ipAddress}</div>
          </div>
          {#if selected.userAgent}
          <div>
            <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">User Agent <Info tip="Mining software/firmware identifier" size={11} /></div>
            <div class="text-sm font-data" style="color: var(--text-primary);">{selected.userAgent}</div>
          </div>
          {/if}
          <div>
            <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Connected <Info tip="Time since this miner first connected" size={11} /></div>
            <div class="text-sm font-data" style="color: var(--text-primary);">{timeAgo(selected.connectedAt)}</div>
          </div>
          <div>
            <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Last Share <Info tip="Time since most recent share submission" size={11} /></div>
            <div class="text-sm font-data" style="color: var(--text-primary);">{timeAgo(selected.lastShareTime)}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}
