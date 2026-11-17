<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Icon from '../common/Icon.svelte';
  import MinerGlobe from '../common/MinerGlobe.svelte';
  import logoUrl from '../../../assets/images/logo.png';
  import { dashboardStats } from '../../stores/stats';
  import { formatDuration, formatDifficulty } from '../../utils/format';
  import { EventsOn } from '../../../../wailsjs/runtime/runtime';

  export let currentPage: string = 'dashboard';

  // Real app version (injected at build time), not a hardcoded string.
  let appVersion = '';

  // Pool info panel — mirrors the dashboard's stats store so it stays live
  // without a second polling loop. Coin symbol comes from the stats store
  // itself (backend recomputes it every tick) rather than a one-time
  // GetConfig() fetch, so it updates immediately when the user switches coins.
  let unsubConfigStats: () => void;
  $: coinSymbol = $dashboardStats.coinSymbol || $dashboardStats.coinId.toUpperCase();

  onMount(async () => {
    try {
      const { GetVersion } = await import('../../../../wailsjs/go/appcore/App');
      const v = await GetVersion();
      appVersion = !v || v === 'dev' ? 'dev' : (v.startsWith('v') ? v : 'v' + v);
    } catch { appVersion = ''; }

    try {
      const { GetDashboardStats } = await import('../../../../wailsjs/go/appcore/App');
      dashboardStats.set(await GetDashboardStats());
    } catch {}

    unsubConfigStats = EventsOn('stats:updated', (data: any) => {
      dashboardStats.set(data);
    });
  });

  onDestroy(() => {
    if (unsubConfigStats) unsubConfigStats();
  });

  const navItems = [
    { id: 'dashboard', label: 'Dashboard', icon: 'reactor' },
    { id: 'miners', label: 'Miners', icon: 'chip' },
    { id: 'mining', label: 'Mining', icon: 'server-rack' },
    { id: 'logs', label: 'Logs', icon: 'datastream' },
  ];

  let confirmShutdown = false;
  async function doShutdown() {
    try {
      const { Shutdown } = await import('../../../../wailsjs/go/appcore/App');
      await Shutdown();
    } catch {}
  }
</script>

<aside
  class="w-16 lg:w-56 h-full flex flex-col flex-shrink-0"
  style="background-color: var(--bg-secondary); border-right: 1px solid var(--accent); box-shadow: 1px 0 8px var(--accent-glow);"
>
  <!-- Logo / Brand -->
  <div class="h-16 flex items-center justify-center wails-drag" style="border-bottom: 1px solid var(--border);">
    <span class="logo-wrap w-10 h-10 lg:w-11 lg:h-11" style="--logo-url: url({logoUrl});">
      <img src={logoUrl} alt="TheVault" class="w-full h-full logo-tint" />
    </span>
  </div>

  <!-- Navigation -->
  <nav class="py-4 px-2 space-y-1">
    {#each navItems as item}
      <button
        class="w-full flex items-center px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 relative"
        style={currentPage === item.id
          ? `color: var(--accent); background: rgba(var(--accent-rgb), 0.1);`
          : `color: var(--text-secondary);`}
        class:glow-text={currentPage === item.id}
        on:click={() => currentPage = item.id}
        on:mouseenter={(e) => { if (currentPage !== item.id) e.currentTarget.style.color = 'var(--text-primary)'; }}
        on:mouseleave={(e) => { if (currentPage !== item.id) e.currentTarget.style.color = 'var(--text-secondary)'; }}
      >
        <!-- Active indicator bar -->
        {#if currentPage === item.id}
          <div
            class="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-5 rounded-r"
            style="background: var(--accent); box-shadow: 0 0 6px var(--accent-glow);"
          ></div>
        {/if}

        <Icon name={item.icon} size={20} />
        <span class="ml-3 hidden lg:block">{item.label}</span>
      </button>
    {/each}
  </nav>

  <!-- Globe animation fills gap between nav and footer -->
  <MinerGlobe />

  <!-- Pool information — live stratum stats, collapsed-sidebar-safe -->
  <div class="hidden lg:block px-3 py-2 space-y-1" style="border-top: 1px solid var(--border);">
    <div class="text-[10px] font-tech uppercase tracking-wider mb-1" style="color: var(--text-secondary);">Pool Info</div>
    <div class="flex items-center justify-between text-[11px] font-data">
      <span style="color: var(--text-secondary);">Uptime</span>
      <span style="color: var(--text-primary);">{$dashboardStats.stratumRunning ? formatDuration($dashboardStats.stratumUptimeSec) : 'offline'}</span>
    </div>
    <div class="flex items-center justify-between text-[11px] font-data">
      <span style="color: var(--text-secondary);">Coin</span>
      <span style="color: var(--text-primary);">{coinSymbol || '—'}</span>
    </div>
    <div class="flex items-center justify-between text-[11px] font-data">
      <span style="color: var(--text-secondary);">Shares/sec</span>
      <span style="color: var(--text-primary);">{$dashboardStats.sharesPerSecond.toFixed(2)}</span>
    </div>
    <div class="flex items-center justify-between text-[11px] font-data">
      <span style="color: var(--text-secondary);">Best Diff</span>
      <span style="color: var(--accent);">{formatDifficulty($dashboardStats.bestDifficulty)}</span>
    </div>
  </div>

  <!-- Shutdown + status at bottom -->
  <div class="p-3 space-y-2" style="border-top: 1px solid var(--border);">
    {#if confirmShutdown}
      <div class="flex gap-1.5">
        <button
          class="flex-1 flex items-center justify-center px-2 py-2 rounded-lg text-xs font-tech uppercase tracking-wider transition-all"
          style="background: rgba(255,50,50,0.14); color: var(--error); border: 1px solid var(--error);"
          on:click={doShutdown}
          title="Fully quit TheVault and stop mining"
        >
          <span class="hidden lg:inline">Confirm Quit</span>
          <span class="lg:hidden">✕</span>
        </button>
        <button
          class="px-2 py-2 rounded-lg text-xs font-tech uppercase tracking-wider hidden lg:block"
          style="background-color: var(--bg-card); color: var(--text-secondary); border: 1px solid var(--border);"
          on:click={() => confirmShutdown = false}
        >Cancel</button>
      </div>
    {:else}
      <button
        class="w-full flex items-center px-3 py-2 rounded-lg text-sm font-medium transition-all duration-200"
        style="color: var(--text-secondary);"
        on:click={() => confirmShutdown = true}
        on:mouseenter={(e) => e.currentTarget.style.color = 'var(--error)'}
        on:mouseleave={(e) => e.currentTarget.style.color = 'var(--text-secondary)'}
        title="Closing the window (X) keeps TheVault running in the background so mining continues. Use Shutdown to fully quit; relaunch TheVault to reopen."
      >
        <Icon name="power" size={20} />
        <span class="ml-3 hidden lg:block">Shutdown</span>
      </button>
    {/if}

    <div class="flex items-center px-2">
      <div
        class="w-2 h-2 rounded-full status-pulse"
        style="background-color: var(--accent); box-shadow: 0 0 6px var(--accent-glow);"
      ></div>
      <span class="ml-2 text-xs font-data hidden lg:block" style="color: var(--text-secondary);">{appVersion}</span>
    </div>
  </div>
</aside>
