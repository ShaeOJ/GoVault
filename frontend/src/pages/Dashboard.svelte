<script lang="ts">
  import { onMount, onDestroy, afterUpdate } from 'svelte';
  import { Chart, LineController, LineElement, PointElement, LinearScale, TimeScale, Filler, Tooltip } from 'chart.js';
  import 'chartjs-adapter-date-fns';
  import StatCard from '../lib/components/common/StatCard.svelte';
  import Icon from '../lib/components/common/Icon.svelte';
  import Info from '../lib/components/common/Info.svelte';
  import ThemedSpinner from '../lib/components/common/ThemedSpinner.svelte';
  import { dashboardStats, blockFound } from '../lib/stores/stats';
  import { miners } from '../lib/stores/miners';
  import type { MinerInfo } from '../lib/stores/miners';
  import { formatHashrate, formatDifficulty, formatDuration, formatNumber, formatChance, formatRatio } from '../lib/utils/format';
  import type { DashboardStats, HashratePoint } from '../lib/stores/stats';
  import { EventsOn } from '../../wailsjs/runtime/runtime';

  Chart.register(LineController, LineElement, PointElement, LinearScale, TimeScale, Filler, Tooltip);

  let stats: DashboardStats;
  let showBlockBanner = false;
  let blockInfo: { hash: string; height: number } | null = null;
  let chartCanvas: HTMLCanvasElement;
  let chart: Chart | null = null;
  let hashrateData: HashratePoint[] = [];
  let selectedPeriod = '1h';
  let unsubStats: () => void;
  let unsubBlock: () => void;
  let chartRefreshInterval: ReturnType<typeof setInterval>;
  let coinName = 'Bitcoin';
  let coinSymbol = 'BTC';
  let stratumToggling = false;
  let stratumError = '';
  let clearingRejects = false;
  let reconnecting = false;
  let reconnectResult = '';

  // Subscribe to store
  const unsub = dashboardStats.subscribe(s => stats = s);

  // Read CSS vars for chart theming
  function getThemeColor(varName: string): string {
    return getComputedStyle(document.documentElement).getPropertyValue(varName).trim();
  }

  onMount(async () => {
    // Listen for stats updates from Go
    unsubStats = EventsOn('stats:updated', (data: DashboardStats) => {
      dashboardStats.set(data);
    });

    unsubBlock = EventsOn('stratum:block-found', (data: { hash: string; height: number }) => {
      blockInfo = data;
      showBlockBanner = true;
      blockFound.set(data);
      setTimeout(() => showBlockBanner = false, 30000);
    });

    // Load initial stats and coin info
    try {
      const { GetDashboardStats, GetConfig, GetCoinList } = await import('../../wailsjs/go/main/App');
      const s = await GetDashboardStats();
      dashboardStats.set(s);
      // Get active coin info
      const cfg = await GetConfig();
      const coins = await GetCoinList() || [];
      const activeCoin = coins.find((c: any) => c.id === (cfg?.mining?.coin || 'btc'));
      if (activeCoin) {
        coinName = activeCoin.name;
        coinSymbol = activeCoin.symbol;
      }
    } catch {}

    loadHashrateHistory();

    // Refresh chart data every 60s (new data points are recorded every 60s)
    chartRefreshInterval = setInterval(loadHashrateHistory, 60000);
  });

  onDestroy(() => {
    unsub();
    if (unsubStats) unsubStats();
    if (unsubBlock) unsubBlock();
    if (chartRefreshInterval) clearInterval(chartRefreshInterval);
    if (chart) { chart.destroy(); chart = null; }
  });

  async function loadHashrateHistory() {
    try {
      const { GetHashrateHistory } = await import('../../wailsjs/go/main/App');
      hashrateData = await GetHashrateHistory(selectedPeriod) || [];
    } catch {}
  }

  // Determine the best unit for a hashrate range
  function bestUnit(maxH: number): { divisor: number; label: string } {
    const units = [
      { divisor: 1e18, label: 'EH/s' },
      { divisor: 1e15, label: 'PH/s' },
      { divisor: 1e12, label: 'TH/s' },
      { divisor: 1e9,  label: 'GH/s' },
      { divisor: 1e6,  label: 'MH/s' },
      { divisor: 1e3,  label: 'KH/s' },
      { divisor: 1,    label: 'H/s' },
    ];
    for (const u of units) {
      if (maxH >= u.divisor) return u;
    }
    return units[units.length - 1];
  }

  function renderChart() {
    if (!chartCanvas || hashrateData.length === 0) return;

    const accent = getThemeColor('--accent');
    const accentRgb = getThemeColor('--accent-rgb');
    const borderColor = getThemeColor('--border') || 'rgba(255,255,255,0.04)';
    const textSecondary = getThemeColor('--text-secondary');

    const maxH = Math.max(...hashrateData.map(p => p.h));
    const unit = bestUnit(maxH);

    const data = hashrateData.map(p => ({
      x: p.t * 1000,
      y: p.h / unit.divisor,
    }));

    if (chart) {
      chart.data.datasets[0].data = data;
      chart.data.datasets[0].borderColor = accent;
      chart.data.datasets[0].backgroundColor = `rgba(${accentRgb}, 0.08)`;
      (chart.options.scales!.y as any).title.text = unit.label;
      (chart.options.scales!.y as any).title.color = textSecondary;
      (chart.options.scales!.y as any).ticks.color = textSecondary;
      (chart.options.scales!.y as any).grid.color = borderColor;
      (chart.options.scales!.x as any).ticks.color = textSecondary;
      (chart.options.scales!.x as any).grid.color = borderColor;
      chart.update('none');
      return;
    }

    chart = new Chart(chartCanvas, {
      type: 'line',
      data: {
        datasets: [{
          data,
          borderColor: accent,
          backgroundColor: `rgba(${accentRgb}, 0.08)`,
          borderWidth: 1.5,
          fill: true,
          tension: 0.3,
          pointRadius: 0,
          pointHitRadius: 8,
        }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: 'index', intersect: false },
        plugins: {
          tooltip: {
            backgroundColor: 'rgba(0,0,0,0.85)',
            borderColor: accent,
            borderWidth: 1,
            titleFont: { family: 'JetBrains Mono', size: 11 },
            bodyFont: { family: 'JetBrains Mono', size: 11 },
            callbacks: {
              label: (ctx) => formatHashrate(ctx.parsed.y * unit.divisor),
              title: (items) => {
                if (!items.length) return '';
                return new Date(items[0].parsed.x).toLocaleTimeString();
              },
            },
          },
        },
        scales: {
          x: {
            type: 'time',
            ticks: { color: textSecondary, maxTicksLimit: 6, font: { size: 10, family: 'JetBrains Mono' } },
            grid: { color: borderColor },
            border: { display: false },
          },
          y: {
            ticks: { color: textSecondary, font: { size: 10, family: 'JetBrains Mono' } },
            grid: { color: borderColor },
            border: { display: false },
            title: { display: true, text: unit.label, color: textSecondary, font: { size: 10, family: 'JetBrains Mono' } },
            beginAtZero: true,
          },
        },
      },
    });
  }

  afterUpdate(() => {
    renderChart();
  });

  async function toggleStratum() {
    stratumToggling = true;
    stratumError = '';
    try {
      if (stats.stratumRunning) {
        const { StopStratum } = await import('../../wailsjs/go/main/App');
        await StopStratum();
      } else {
        const { StartStratum } = await import('../../wailsjs/go/main/App');
        await StartStratum();
      }
    } catch (e: any) {
      stratumError = e?.message || String(e);
    }
    stratumToggling = false;
  }

  async function clearRejectedShares() {
    clearingRejects = true;
    try {
      const { ClearRejectedShares } = await import('../../wailsjs/go/main/App');
      await ClearRejectedShares();
      dashboardStats.update(s => ({ ...s, sharesRejected: 0 }));
    } catch (e) {
      console.error('Failed to clear rejected shares:', e);
    }
    clearingRejects = false;
  }

  async function reconnectMiners() {
    reconnecting = true;
    reconnectResult = '';
    try {
      const { ReconnectMiners } = await import('../../wailsjs/go/main/App');
      const result = await ReconnectMiners();
      if (result.error) {
        reconnectResult = result.error;
      } else if (result.attempted === 0) {
        reconnectResult = 'All recent miners already connected';
      } else {
        reconnectResult = `Nudged ${result.success}/${result.attempted} miners`;
      }
      setTimeout(() => reconnectResult = '', 5000);
    } catch (e: any) {
      reconnectResult = e?.message || String(e);
      setTimeout(() => reconnectResult = '', 5000);
    }
    reconnecting = false;
  }

  function selectPeriod(period: string) {
    selectedPeriod = period;
    if (chart) { chart.destroy(); chart = null; }
    loadHashrateHistory();
  }
</script>

<!-- Block Found Banner -->
{#if showBlockBanner}
  <div class="block-banner fixed top-0 left-0 right-0 z-50 p-4 text-center animate-slide-in-up shadow-2xl">
    <div class="font-bold text-lg font-tech" style="color: var(--bg-primary);">BLOCK FOUND!</div>
    {#if blockInfo}
      <div class="text-sm" style="color: var(--bg-primary); opacity: 0.8;">Height: {blockInfo.height} | Hash: {blockInfo.hash?.substring(0, 16)}...</div>
    {/if}
    <button class="absolute top-2 right-4 opacity-60 hover:opacity-100" style="color: var(--bg-primary);" on:click={() => showBlockBanner = false}>
      <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
      </svg>
    </button>
  </div>
{/if}

<div class="space-y-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-bold font-tech uppercase tracking-wide" style="color: var(--text-primary);">Dashboard</h1>
      <p class="text-sm" style="color: var(--text-secondary);">Solo mining {coinSymbol}</p>
    </div>
    <div class="flex items-center gap-2">
      {#if reconnectResult}
        <span class="text-xs font-data" style="color: var(--text-secondary);">{reconnectResult}</span>
      {/if}
      {#if stats?.stratumRunning}
        <button
          class="px-3 py-2 rounded-lg font-medium text-sm font-tech uppercase tracking-wider transition-all duration-200 glow-border-hover flex items-center gap-2"
          style="background: rgba(var(--accent-rgb), 0.05); color: var(--text-secondary); border: 1px solid var(--border);"
          style:opacity={reconnecting ? '0.7' : '1'}
          on:click={reconnectMiners}
          disabled={reconnecting}
          title="Nudge disconnected AxeOS miners to reconnect"
        >
          {#if reconnecting}
            <ThemedSpinner size={16} />
          {:else}
            <Icon name="refresh" size={16} />
          {/if}
          Reconnect
        </button>
      {/if}
      <button
        class="px-4 py-2 rounded-lg font-medium text-sm font-tech uppercase tracking-wider transition-all duration-200 glow-border-hover flex items-center gap-2"
        style={stats?.stratumRunning
          ? `background: rgba(var(--accent-rgb), 0.05); color: var(--error); border: 1px solid var(--error);`
          : `background: rgba(var(--accent-rgb), 0.05); color: var(--accent); border: 1px solid var(--accent);`}
        style:opacity={stratumToggling ? '0.7' : '1'}
        on:click={toggleStratum}
        disabled={stratumToggling}
      >
        {#if stratumToggling}
          <ThemedSpinner size={16} />
          {stats?.stratumRunning ? 'Stopping...' : 'Starting...'}
        {:else}
          {stats?.stratumRunning ? 'Stop Server' : 'Start Server'}
        {/if}
      </button>
    </div>
  </div>

  {#if stratumError}
    <div class="rounded-lg p-3" style="background: rgba(255,50,50,0.05); border: 1px solid rgba(255,50,50,0.2);">
      <div class="text-sm font-medium mb-1" style="color: var(--error);">Server Error</div>
      <div class="text-xs font-data" style="color: var(--text-secondary);">{stratumError}</div>
    </div>
  {/if}

  <!-- Stat Cards -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
    <StatCard
      label="Total Hashrate"
      value={formatHashrate(stats?.totalHashrate || 0)}
      iconName="bolt"
      color="accent"
      tooltip="Combined hash rate of all connected miners"
    />
    <StatCard
      label="Active Miners"
      value={String(stats?.activeMiners || 0)}
      iconName="waves"
      color="green"
      tooltip="Mining devices currently connected to your stratum server"
    />
    <StatCard
      label="Best Difficulty"
      value={formatDifficulty(stats?.bestDifficulty || 0)}
      subtext={stats?.bestDifficulty > 0 && stats?.networkDifficulty > 0
        ? `1 in ${formatRatio(stats.networkDifficulty / stats.bestDifficulty)} vs network`
        : ''}
      iconName="hexshield"
      color="gold"
      tooltip="Highest difficulty share ever submitted. Ratio shows proximity to network difficulty"
    />
    <StatCard
      label="Est. Time to Block"
      value={formatDuration(stats?.estTimeToBlock || 0)}
      iconName="gauge"
      color="gray"
      tooltip="Statistical average based on your hashrate vs network difficulty. Solo mining is probabilistic"
    />
  </div>

  <!-- Second Row -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
    <StatCard
      label="Blocks Found"
      value={String(stats?.blocksFound || 0)}
      subtext="solo blocks discovered"
      iconName="cube"
      color="accent"
      tooltip="{coinName} blocks mined and submitted to the network by your pool"
    />
    <StatCard
      label="Block Chance"
      value={formatChance(stats?.blockChance || 0)}
      subtext="per day"
      iconName="dice"
      color="gold"
      tooltip="Probability of finding at least one block in the next 24 hours"
    />
    <StatCard
      label="Pool Shares"
      value={formatNumber(stats?.poolShares || 0)}
      iconName="nodes"
      color="green"
      tooltip="Accepted shares count toward hashrate. Rejected indicate stale or invalid work"
    >
      <svelte:fragment slot="subtext">
        {formatNumber(stats?.sharesAccepted || 0)} accepted · {formatNumber(stats?.sharesRejected || 0)} rejected
        {#if (stats?.sharesRejected || 0) > 0}
          <button
            class="ml-1 px-1 py-0 rounded text-[10px] transition-colors"
            style="color: var(--text-secondary); background: rgba(255,255,255,0.08);"
            style:opacity={clearingRejects ? '0.5' : '1'}
            on:click={clearRejectedShares}
            disabled={clearingRejects}
            title="Clear rejected share history"
          >
            {clearingRejects ? '...' : 'Clear'}
          </button>
        {/if}
      </svelte:fragment>
    </StatCard>
    <StatCard
      label="Network Difficulty"
      value={formatDifficulty(stats?.networkDifficulty || 0)}
      subtext="Height: {formatNumber(stats?.blockHeight || 0)}"
      iconName="gauge"
      color="gray"
      tooltip="Global {coinName} mining difficulty, adjusted every ~2016 blocks"
    />
  </div>

  <!-- Hashrate Chart -->
  <div class="rounded-xl p-5 card-glow" style="background-color: var(--bg-card);">
    <div class="flex items-center justify-between mb-4">
      <h3 class="text-sm font-medium font-tech uppercase tracking-wider" style="color: var(--text-secondary);">Hashrate Over Time</h3>
      <div class="flex gap-1">
        {#each ['1h', '6h', '24h', '7d'] as period}
          <button
            class="px-3 py-1 text-xs rounded-md transition-colors font-data"
            style={selectedPeriod === period
              ? `background: rgba(var(--accent-rgb), 0.15); color: var(--accent);`
              : `color: var(--text-secondary);`}
            on:click={() => selectPeriod(period)}
          >
            {period}
          </button>
        {/each}
      </div>
    </div>

    <div class="h-80 flex items-center justify-center">
      {#if hashrateData.length > 0}
        <canvas bind:this={chartCanvas} class="w-full h-full"></canvas>
      {:else}
        <div class="text-sm font-data" style="color: var(--text-secondary);">
          {stats?.stratumRunning ? 'Waiting for hashrate data...' : 'Start the stratum server to see hashrate data'}
        </div>
      {/if}
    </div>
  </div>

  <!-- Best Share Difficulty per Miner -->
  {#if $miners.length > 0}
    {@const topMiners = [...$miners].filter(m => m.bestDifficulty > 0).sort((a, b) => b.bestDifficulty - a.bestDifficulty).slice(0, 3)}
    {#if topMiners.length > 0}
      <div class="rounded-xl p-5 card-glow" style="background-color: var(--bg-card);">
        <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-3 inline-flex items-center gap-1" style="color: var(--text-secondary);">Top Share Difficulty <Info tip="Miners ranked by their highest difficulty share" size={13} /></h3>
        <div class="space-y-2">
          {#each topMiners as m, i}
            <div class="flex items-center gap-3 rounded-lg px-3 py-2" style="background-color: var(--bg-secondary);">
              <span class="text-xs font-tech w-5 text-center" style="color: {i === 0 ? 'var(--warning)' : 'var(--text-secondary)'};">#{i + 1}</span>
              <div class="flex-1 min-w-0">
                <span class="text-sm font-medium truncate block" style="color: var(--text-primary);">{m.workerName || m.id}</span>
              </div>
              <span class="text-sm font-bold data-readout" style="color: {i === 0 ? 'var(--warning)' : 'var(--accent)'};">{formatDifficulty(m.bestDifficulty)}</span>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  {/if}

</div>
