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
  import { formatHashrate, formatDifficulty, formatDuration, formatNumber, formatChance, formatRatio, timeAgo } from '../lib/utils/format';
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
  let coinList: any[] = [];
  // Live from the stats store (backend recomputes every tick), so a coin
  // switch or mode change is reflected immediately instead of waiting for a remount.
  $: coinSymbol = stats?.coinSymbol || (stats?.coinId || 'BTC').toUpperCase();
  $: coinName = coinList.find(c => c.id === stats?.coinId)?.name || 'Bitcoin';
  $: miningMode = stats?.miningMode || 'solo';
  let stratumToggling = false;
  let stratumError = '';
  let clearingRejects = false;
  let reconnecting = false;
  let reconnectResult = '';

  // Recent-share bar graph + last-accepted-share detail (expanded "Top Share
  // Difficulty" card — mirrors asicpool's wallet-page share history panel).
  type ShareBarPoint = { time: number; difficulty: number };
  type LastShareInfo = { header: string; nonce: string; hash: string; difficulty: number; time: number; worker: string };
  let shareBars: ShareBarPoint[] = [];
  let lastShare: LastShareInfo | null = null;
  let unsubShareAccepted: () => void;

  // decodeBlockHeader splits an 80-byte (160-hex) block header into its fields.
  // prevhash/merkleroot are stored internal little-endian, shown reversed (display).
  function decodeBlockHeader(hexStr: string) {
    if (!hexStr || hexStr.length < 160) return null;
    const rev = (h: string) => (h.match(/../g) || []).reverse().join('');
    const leU32 = (o: number) => parseInt(rev(hexStr.slice(o, o + 8)), 16) >>> 0;
    return {
      version: '0x' + leU32(0).toString(16).padStart(8, '0'),
      prevhash: rev(hexStr.slice(8, 72)),
      merkleroot: rev(hexStr.slice(72, 136)),
      ntime: leU32(136),
      bits: '0x' + leU32(144).toString(16).padStart(8, '0'),
      nonce: '0x' + leU32(152).toString(16).padStart(8, '0'),
    };
  }

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

    // Load initial stats and coin list (names only — symbol/mode stay live via the store)
    try {
      const { GetDashboardStats, GetCoinList } = await import('../../wailsjs/go/appcore/App');
      const s = await GetDashboardStats();
      dashboardStats.set(s);
      coinList = await GetCoinList() || [];
    } catch {}

    loadHashrateHistory();

    // Refresh chart data every 60s (new data points are recorded every 60s)
    chartRefreshInterval = setInterval(loadHashrateHistory, 60000);

    // Seed the recent-share bar graph + last-share panel, then keep both live
    // via the same event the globe listens to (no extra polling).
    try {
      const { GetShareBars, GetLastShare } = await import('../../wailsjs/go/appcore/App');
      shareBars = (await GetShareBars()) || [];
      lastShare = await GetLastShare();
    } catch {}

    unsubShareAccepted = EventsOn('stratum:share-accepted', (data: { difficulty: number; nonce: string; header: string; hash: string; worker: string; time: number }) => {
      if (!data.header) return; // defensive — header is always set, but don't chart garbage
      shareBars = [...shareBars, { time: data.time, difficulty: data.difficulty }].slice(-48);
      lastShare = { header: data.header, nonce: data.nonce, hash: data.hash, difficulty: data.difficulty, time: data.time, worker: data.worker };
    });
  });

  onDestroy(() => {
    unsub();
    if (unsubStats) unsubStats();
    if (unsubBlock) unsubBlock();
    if (unsubShareAccepted) unsubShareAccepted();
    if (chartRefreshInterval) clearInterval(chartRefreshInterval);
    if (chart) { chart.destroy(); chart = null; }
  });

  async function loadHashrateHistory() {
    try {
      const { GetHashrateHistory } = await import('../../wailsjs/go/appcore/App');
      hashrateData = await GetHashrateHistory(selectedPeriod) || [];
    } catch {}
  }

  // Reactive log-scale bar layout for the recent-share-difficulty graph
  // (ported from asicpool's shareGraphHTML). shareBars is oldest-first.
  type BarRect = { x: number; y: number; w: number; h: number; op: number; newest: boolean; title: string };
  let barRects: BarRect[] = [];
  const BAR_W = 300, BAR_H = 70, BAR_PAD = 2;
  $: {
    const bars = shareBars;
    if (bars.length === 0) {
      barRects = [];
    } else {
      const logs = bars.map(b => Math.log10(Math.max(b.difficulty, 1)));
      const min = Math.min(...logs), max = Math.max(...logs), span = (max - min) || 1;
      const n = bars.length;
      const bw = (BAR_W - BAR_PAD * 2) / n, gap = Math.min(bw * 0.18, 2);
      barRects = bars.map((b, i) => {
        const frac = 0.1 + 0.9 * (logs[i] - min) / span; // floor so the smallest bar is still visible
        const h = Math.max(1.5, frac * (BAR_H - BAR_PAD * 2));
        const x = BAR_PAD + i * bw, y = BAR_H - BAR_PAD - h;
        const newest = i === n - 1;
        const op = newest ? 1 : (0.45 + 0.45 * (logs[i] - min) / span);
        return { x, y, w: Math.max(0.6, bw - gap), h, op, newest, title: `${new Date(b.time).toLocaleTimeString()} · diff ${formatDifficulty(b.difficulty)}` };
      });
    }
  }
  $: decodedHeader = lastShare?.header ? decodeBlockHeader(lastShare.header) : null;

  type ShareDetailRow = { label: string; val: string; mono: boolean };
  function shareRow(label: string, val: string, mono = false): ShareDetailRow {
    return { label, val, mono };
  }
  $: lastShareRows = lastShare ? [
    shareRow('Difficulty', formatDifficulty(lastShare.difficulty)),
    shareRow('Nonce', lastShare.nonce, true),
    shareRow('Hash', lastShare.hash, true),
    ...(decodedHeader ? [
      shareRow('Version', decodedHeader.version, true),
      shareRow('Prev block', decodedHeader.prevhash, true),
      shareRow('Merkle root', decodedHeader.merkleroot, true),
      shareRow('nTime', `${decodedHeader.ntime} (${new Date(decodedHeader.ntime * 1000).toUTCString()})`, true),
      shareRow('Bits', decodedHeader.bits, true),
    ] : []),
    shareRow('Header', lastShare.header, true),
  ] : [];

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
    const secondary = getThemeColor('--success');
    const borderColor = getThemeColor('--border') || 'rgba(255,255,255,0.04)';
    const textSecondary = getThemeColor('--text-secondary');

    const maxH = Math.max(...hashrateData.map(p => p.h));
    const unit = bestUnit(maxH);

    const data = hashrateData.map(p => ({
      x: p.t * 1000,
      y: p.h / unit.divisor,
    }));
    const shareData = hashrateData.map(p => ({
      x: p.t * 1000,
      y: p.s,
    }));

    if (chart) {
      chart.data.datasets[0].data = data;
      chart.data.datasets[0].borderColor = accent;
      chart.data.datasets[0].backgroundColor = `rgba(${accentRgb}, 0.08)`;
      chart.data.datasets[1].data = shareData;
      chart.data.datasets[1].borderColor = secondary;
      (chart.options.scales!.y as any).title.text = unit.label;
      (chart.options.scales!.y as any).title.color = textSecondary;
      (chart.options.scales!.y as any).ticks.color = textSecondary;
      (chart.options.scales!.y as any).grid.color = borderColor;
      (chart.options.scales!.y1 as any).title.color = secondary;
      (chart.options.scales!.y1 as any).ticks.color = secondary;
      (chart.options.scales!.x as any).ticks.color = textSecondary;
      (chart.options.scales!.x as any).grid.color = borderColor;
      chart.update('none');
      return;
    }

    chart = new Chart(chartCanvas, {
      type: 'line',
      data: {
        datasets: [{
          label: 'Hashrate',
          data,
          borderColor: accent,
          backgroundColor: `rgba(${accentRgb}, 0.08)`,
          borderWidth: 1.5,
          fill: true,
          tension: 0.3,
          pointRadius: 0,
          pointHitRadius: 8,
          yAxisID: 'y',
        }, {
          label: 'Shares/sec',
          data: shareData,
          borderColor: secondary,
          backgroundColor: 'transparent',
          borderWidth: 1.25,
          borderDash: [4, 3],
          fill: false,
          tension: 0.3,
          pointRadius: 0,
          pointHitRadius: 8,
          yAxisID: 'y1',
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
              label: (ctx) => ctx.datasetIndex === 0
                ? `Hashrate: ${formatHashrate(ctx.parsed.y * unit.divisor)}`
                : `Shares/sec: ${ctx.parsed.y.toFixed(2)}`,
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
            position: 'left',
            ticks: { color: textSecondary, font: { size: 10, family: 'JetBrains Mono' } },
            grid: { color: borderColor },
            border: { display: false },
            title: { display: true, text: unit.label, color: textSecondary, font: { size: 10, family: 'JetBrains Mono' } },
            beginAtZero: true,
          },
          y1: {
            position: 'right',
            ticks: { color: secondary, font: { size: 10, family: 'JetBrains Mono' } },
            grid: { display: false },
            border: { display: false },
            title: { display: true, text: 'shares/s', color: secondary, font: { size: 10, family: 'JetBrains Mono' } },
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
        const { StopStratum } = await import('../../wailsjs/go/appcore/App');
        await StopStratum();
      } else {
        const { StartStratum } = await import('../../wailsjs/go/appcore/App');
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
      const { ClearRejectedShares } = await import('../../wailsjs/go/appcore/App');
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
      const { ReconnectMiners } = await import('../../wailsjs/go/appcore/App');
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
      <p class="text-sm" style="color: var(--text-secondary);">{miningMode === 'proxy' ? 'Proxy' : 'Solo'} mining {coinSymbol}</p>
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
    {#if miningMode === 'proxy' && stats?.split}
      <StatCard
        label="Pool A Difficulty"
        value={formatDifficulty(stats?.poolADiff || 0)}
        subtext={stats.activePool === 'A' ? `active · ${stats.splitPercentA}% target` : 'standby'}
        iconName="gauge"
        color={stats.activePool === 'A' ? 'gold' : 'gray'}
        tooltip="Pool A's current difficulty. 'Active' means it's the pool currently receiving your hashrate"
      />
      <StatCard
        label="Pool A Network Diff"
        value={formatDifficulty(stats?.networkDiffA || 0)}
        subtext={`${formatNumber(stats?.sharesFwdA || 0)} shares fwd${stats.poolAHealthy ? '' : ' · DISCONNECTED'}`}
        iconName="hexshield"
        color={stats.poolAHealthy ? 'accent' : 'red'}
        tooltip="Network difficulty as last reported by Pool A's own job — compare to Pool B's to spot a pool lagging behind on block height"
      />
      <StatCard
        label="Pool B Difficulty"
        value={formatDifficulty(stats?.poolBDiff || 0)}
        subtext={stats.activePool === 'B' ? `active · ${100 - stats.splitPercentA}% target` : 'standby'}
        iconName="gauge"
        color={stats.activePool === 'B' ? 'gold' : 'gray'}
        tooltip="Pool B's current difficulty. 'Active' means it's the pool currently receiving your hashrate"
      />
      <StatCard
        label="Pool B Network Diff"
        value={formatDifficulty(stats?.networkDiffB || 0)}
        subtext={`${formatNumber(stats?.sharesFwdB || 0)} shares fwd${stats.poolBHealthy ? '' : ' · DISCONNECTED'}`}
        iconName="hexshield"
        color={stats.poolBHealthy ? 'accent' : 'red'}
        tooltip="Network difficulty as last reported by Pool B's own job — compare to Pool A's to spot a pool lagging behind on block height"
      />
    {:else if miningMode === 'proxy'}
      <StatCard
        label="Pool Difficulty"
        value={formatDifficulty(stats?.upstreamDiff || 0)}
        subtext="upstream pool target"
        iconName="gauge"
        color="gold"
        tooltip="Current difficulty set by the upstream pool. All miners are locked to this difficulty"
      />
      <StatCard
        label="Upstream Shares"
        value={formatNumber(stats?.proxySharesFwd || 0)}
        iconName="nodes"
        color="green"
        tooltip="Shares forwarded to the upstream pool. Accepted count toward pool hashrate"
      >
        <svelte:fragment slot="subtext">
          {formatNumber(stats?.proxySharesAccepted || 0)} accepted · <span style="color: {(stats?.proxySharesRejected || 0) > 0 ? 'var(--error)' : 'inherit'};">{formatNumber(stats?.proxySharesRejected || 0)} rejected</span>
        </svelte:fragment>
      </StatCard>
    {:else}
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
    {/if}
    <StatCard
      label="Pool Shares"
      value={formatNumber(stats?.poolShares || 0)}
      iconName="nodes"
      color={miningMode === 'proxy' ? 'accent' : 'green'}
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

  <!-- Third Row -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
    <StatCard
      label="Stratum Uptime"
      value={stats?.stratumRunning ? formatDuration(stats?.stratumUptimeSec || 0) : 'Stopped'}
      iconName="power"
      color="gray"
      tooltip="How long the stratum server has been running continuously"
    />
    <StatCard
      label="Share Rate"
      value={`${(stats?.sharesPerSecond || 0).toFixed(2)}/s`}
      iconName="waves"
      color="green"
      tooltip="Live rate of accepted shares across all miners — a sudden drop can signal a stalled or disconnected fleet"
    />
    {#if miningMode === 'proxy'}
      <StatCard
        label="Pool Ping"
        value={stats?.poolPingMs > 0 ? `${Math.round(stats.poolPingMs)} ms` : 'N/A'}
        iconName="nodes"
        color="accent"
        tooltip="Smoothed round-trip time for shares forwarded to the upstream pool. High latency can cause stale-share rejects"
      />
    {/if}
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

        <!-- Recent share difficulty bar graph (last 48 accepted shares, log scale) -->
        <div class="mt-5 pt-4" style="border-top: 1px solid var(--border);">
          <div class="text-[11px] tracking-wider uppercase mb-2" style="color: var(--text-secondary);">
            Last {shareBars.length || 48} Accepted Shares · difficulty (log scale)
          </div>
          {#if barRects.length === 0}
            <div class="text-xs font-data" style="color: var(--text-secondary);">No accepted shares yet this session.</div>
          {:else}
            <svg viewBox="0 0 {BAR_W} {BAR_H}" preserveAspectRatio="none" class="w-full" style="height: 70px;">
              {#each barRects as r}
                <rect
                  x={r.x.toFixed(1)} y={r.y.toFixed(1)} width={r.w.toFixed(1)} height={r.h.toFixed(1)} rx="1"
                  fill="rgba(var(--accent-rgb), {r.op.toFixed(2)})"
                  stroke={r.newest ? 'var(--accent)' : 'none'} stroke-width={r.newest ? 1 : 0}
                >
                  <title>{r.title}</title>
                </rect>
              {/each}
            </svg>
          {/if}
        </div>

        <!-- Last accepted share: decoded nonce + 80-byte block header -->
        <div class="mt-4 pt-4" style="border-top: 1px solid var(--border);">
          <div class="flex items-center justify-between mb-2">
            <span class="text-[11px] tracking-wider uppercase inline-flex items-center gap-1" style="color: var(--text-secondary);">
              <Icon name="bolt" size={12} />Last Accepted Share
            </span>
            {#if lastShare}
              <span class="text-xs font-data" style="color: var(--text-secondary);" title={new Date(lastShare.time).toLocaleString()}>
                {timeAgo(new Date(lastShare.time).toISOString())}{lastShare.worker ? ' · ' + lastShare.worker : ''}
              </span>
            {/if}
          </div>
          {#if !lastShare}
            <div class="text-xs font-data" style="color: var(--text-secondary);">No accepted share recorded yet this session — submit a share and it'll appear here.</div>
          {:else}
            <div class="space-y-1 text-xs">
              {#each lastShareRows as r}
                <div class="flex gap-2">
                  <span class="w-20 shrink-0" style="color: var(--text-secondary);">{r.label}</span>
                  <span class="truncate {r.mono ? 'font-data' : ''}" style="color: {r.label === 'Difficulty' ? 'var(--accent)' : 'var(--text-primary)'}; font-weight: {r.label === 'Difficulty' ? '700' : '400'};" title={r.val}>{r.val}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    {/if}
  {/if}

</div>
