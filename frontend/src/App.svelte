<script lang="ts">
  import './lib/stores/theme';  // ensure theme class applied early
  import Sidebar from './lib/components/layout/Sidebar.svelte';
  import Dashboard from './pages/Dashboard.svelte';
  import Miners from './pages/Miners.svelte';
  import Node from './pages/Node.svelte';
  import Settings from './pages/Settings.svelte';
  import Logs from './pages/Logs.svelte';
  import EventFlash from './lib/components/common/EventFlash.svelte';

  let currentPage = 'dashboard';
</script>

<div class="flex h-screen overflow-hidden" style="background-color: var(--bg-primary); color: var(--text-primary);">
  <Sidebar bind:currentPage />

  <main class="flex-1 overflow-y-auto grid-overlay">
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
