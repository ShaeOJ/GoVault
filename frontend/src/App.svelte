<script lang="ts">
  import { onMount } from 'svelte';
  import './lib/stores/theme';  // ensure theme class applied early
  import Sidebar from './lib/components/layout/Sidebar.svelte';
  import Dashboard from './pages/Dashboard.svelte';
  import Miners from './pages/Miners.svelte';
  import Node from './pages/Node.svelte';
  import Settings from './pages/Settings.svelte';
  import Logs from './pages/Logs.svelte';
  import EventFlash from './lib/components/common/EventFlash.svelte';

  let currentPage = 'dashboard';

  // Update banner (fired by the backend's startup release check).
  let updateLatest = '';
  let updateDismissed = false;
  onMount(async () => {
    try {
      const { EventsOn } = await import('../wailsjs/runtime/runtime');
      EventsOn('update:available', (info: any) => { if (info?.latest) updateLatest = info.latest; });
    } catch {}
  });
</script>

<div class="flex h-screen overflow-hidden" style="background-color: var(--bg-primary); color: var(--text-primary);">
  <Sidebar bind:currentPage />

  <main class="flex-1 overflow-y-auto grid-overlay">
    {#if updateLatest && !updateDismissed}
      <div class="flex items-center justify-between gap-3 px-6 py-2 relative z-20" style="background: rgba(var(--accent-rgb), 0.12); border-bottom: 1px solid var(--accent);">
        <span class="text-xs font-tech uppercase tracking-wider" style="color: var(--accent);">GoVault {updateLatest} is available</span>
        <div class="flex items-center gap-2">
          <button class="px-3 py-1 rounded text-xs font-tech uppercase tracking-wider" style="background: var(--accent); color: var(--bg-primary);" on:click={() => { currentPage = 'settings'; updateDismissed = true; }}>Update</button>
          <button class="px-2 py-1 rounded text-xs" style="color: var(--text-secondary);" on:click={() => updateDismissed = true} aria-label="Dismiss">✕</button>
        </div>
      </div>
    {/if}
    <div class="p-6 h-full relative z-10">
      {#if currentPage === 'dashboard'}
        <Dashboard />
      {:else if currentPage === 'miners'}
        <Miners />
      {:else if currentPage === 'node'}
        <Node />
      {:else if currentPage === 'settings'}
        <Settings />
      {:else if currentPage === 'logs'}
        <Logs />
      {/if}
    </div>
  </main>

  <EventFlash />
</div>
