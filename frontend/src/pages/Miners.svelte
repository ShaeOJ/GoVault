<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { miners, selectedMiner, discoveredMiners } from '../lib/stores/miners';
  import { formatHashrate, formatDifficulty, timeAgo } from '../lib/utils/format';
  import type { MinerInfo, DiscoveredMiner } from '../lib/stores/miners';
  import { EventsOn } from '../../wailsjs/runtime/runtime';
  import Icon from '../lib/components/common/Icon.svelte';

  let minerList: MinerInfo[] = [];
  let showPanel = false;
  let selected: MinerInfo | null = null;
  let scanning = false;
  let discovered: DiscoveredMiner[] = [];
  let showDiscovery = false;
  let unsubs: (() => void)[] = [];
  let refreshInterval: ReturnType<typeof setInterval>;

  const unsubMiners = miners.subscribe(m => {
    minerList = [...m].sort((a, b) => a.connectedAt.localeCompare(b.connectedAt) || a.id.localeCompare(b.id));
  });
  const unsubSelected = selectedMiner.subscribe(m => selected = m);
  const unsubDiscovered = discoveredMiners.subscribe(d => discovered = d);

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

    // Load existing miners
    await refreshMiners();

    // Refresh miner data every 5 seconds to pick up share counts + hashrate
    refreshInterval = setInterval(refreshMiners, 5000);
  });

  async function refreshMiners() {
    try {
      const { GetMiners } = await import('../../wailsjs/go/main/App');
      const m = await GetMiners();
      if (m) miners.set(m);
    } catch {}
  }

  onDestroy(() => {
    unsubMiners();
    unsubSelected();
    unsubDiscovered();
    unsubs.forEach(fn => fn());
    if (refreshInterval) clearInterval(refreshInterval);
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

    // Dynamic stale threshold: scale with difficulty and hashrate.
    // expectedTime = diff * 2^32 / hashrate. Use 3x expected time or 120s minimum.
    let staleThreshold = 120;
    if (m.hashrate > 0 && m.currentDiff > 0) {
      const expectedTime = (m.currentDiff * 4294967296) / m.hashrate;
      staleThreshold = Math.max(120, expectedTime * 3);
    } else {
      staleThreshold = 180; // generous default when no hashrate data yet
    }
    const deadThreshold = staleThreshold * 3;

    if (elapsed < staleThreshold) return { color: 'var(--success)', label: 'Active', glow: true };
    if (elapsed < deadThreshold) return { color: 'var(--warning)', label: 'Stale', glow: false };
    return { color: 'var(--error)', label: 'Dead', glow: false };
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
        <svg class="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
        </svg>
        Scanning...
      {:else}
        <Icon name="waves" size={16} />
        Scan Network
      {/if}
    </button>
  </div>

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
        <button
          class="rounded-xl p-4 card-glow text-left w-full"
          style="background-color: var(--bg-card);"
          on:click={() => selectMiner(m)}
        >
          <div class="flex items-center justify-between mb-3">
            <div class="flex items-center gap-2">
              <div
                class="w-2.5 h-2.5 rounded-full {status.glow ? 'status-pulse' : ''}"
                style="background-color: {status.color}; {status.glow ? `box-shadow: 0 0 6px ${status.color};` : ''}"
              ></div>
              <span class="text-sm font-medium truncate max-w-[180px]" style="color: var(--text-primary);">{m.workerName || m.id}</span>
            </div>
            <span class="text-xs font-data glow-text" style="color: {status.color}; text-shadow: 0 0 4px {status.color}40;">{status.label}</span>
          </div>

          <div class="grid grid-cols-2 gap-3">
            <div>
              <div class="text-xs" style="color: var(--text-secondary);">Hashrate</div>
              <div class="text-sm font-medium data-readout">{formatHashrate(m.hashrate)}</div>
            </div>
            <div>
              <div class="text-xs" style="color: var(--text-secondary);">Difficulty</div>
              <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatDifficulty(m.currentDiff)}</div>
            </div>
            <div>
              <div class="text-xs" style="color: var(--text-secondary);">Accepted</div>
              <div class="text-sm font-medium font-data" style="color: var(--success);">{m.sharesAccepted}</div>
            </div>
            <div>
              <div class="text-xs" style="color: var(--text-secondary);">Rejected</div>
              <div class="text-sm font-medium font-data" style="color: var(--error);">{m.sharesRejected}</div>
            </div>
          </div>

          <div class="mt-3 pt-3 flex justify-between text-xs" style="border-top: 1px solid var(--border); color: var(--text-secondary);">
            <span>{m.ipAddress}</span>
            <span>Last share: {timeAgo(m.lastShareTime)}</span>
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
          <div class="text-xs mb-1" style="color: var(--text-secondary);">Worker Name</div>
          <div class="text-sm font-medium font-data break-all" style="color: var(--text-primary);">{selected.workerName}</div>
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div class="rounded-lg p-3" style="background-color: var(--bg-card);">
            <div class="text-xs mb-1" style="color: var(--text-secondary);">Hashrate</div>
            <div class="text-sm font-medium data-readout">{formatHashrate(selected.hashrate)}</div>
          </div>
          <div class="rounded-lg p-3" style="background-color: var(--bg-card);">
            <div class="text-xs mb-1" style="color: var(--text-secondary);">Difficulty</div>
            <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatDifficulty(selected.currentDiff)}</div>
          </div>
          <div class="rounded-lg p-3" style="background-color: var(--bg-card);">
            <div class="text-xs mb-1" style="color: var(--text-secondary);">Accepted</div>
            <div class="text-sm font-medium font-data" style="color: var(--success);">{selected.sharesAccepted}</div>
          </div>
          <div class="rounded-lg p-3" style="background-color: var(--bg-card);">
            <div class="text-xs mb-1" style="color: var(--text-secondary);">Rejected</div>
            <div class="text-sm font-medium font-data" style="color: var(--error);">{selected.sharesRejected}</div>
          </div>
        </div>

        <div class="rounded-lg p-4" style="background-color: var(--bg-card);">
          <div class="text-xs mb-1" style="color: var(--text-secondary);">Best Difficulty</div>
          <div class="text-sm font-medium data-readout" style="color: var(--warning);">{formatDifficulty(selected.bestDifficulty)}</div>
        </div>

        <div class="rounded-lg p-4 space-y-2" style="background-color: var(--bg-card);">
          <div>
            <div class="text-xs" style="color: var(--text-secondary);">IP Address</div>
            <div class="text-sm font-data" style="color: var(--text-primary);">{selected.ipAddress}</div>
          </div>
          {#if selected.userAgent}
          <div>
            <div class="text-xs" style="color: var(--text-secondary);">User Agent</div>
            <div class="text-sm font-data" style="color: var(--text-primary);">{selected.userAgent}</div>
          </div>
          {/if}
          <div>
            <div class="text-xs" style="color: var(--text-secondary);">Connected</div>
            <div class="text-sm font-data" style="color: var(--text-primary);">{timeAgo(selected.connectedAt)}</div>
          </div>
          <div>
            <div class="text-xs" style="color: var(--text-secondary);">Last Share</div>
            <div class="text-sm font-data" style="color: var(--text-primary);">{timeAgo(selected.lastShareTime)}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}
