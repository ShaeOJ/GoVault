<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import logoUrl from '../../../assets/images/logo.png';

  let isMaximised = false;
  let WindowMinimise: () => void;
  let WindowToggleMaximise: () => void;
  let WindowIsMaximised: () => Promise<boolean>;
  let Quit: () => void;

  async function refreshMaximised() {
    try { isMaximised = await WindowIsMaximised(); } catch {}
  }

  function onResize() {
    refreshMaximised();
  }

  onMount(async () => {
    const rt = await import('../../../../wailsjs/runtime/runtime');
    WindowMinimise = rt.WindowMinimise;
    WindowToggleMaximise = rt.WindowToggleMaximise;
    WindowIsMaximised = rt.WindowIsMaximised;
    Quit = rt.Quit;
    refreshMaximised();
    window.addEventListener('resize', onResize);
  });

  onDestroy(() => {
    window.removeEventListener('resize', onResize);
  });

  function minimise() { WindowMinimise?.(); }
  function toggleMaximise() { WindowToggleMaximise?.(); setTimeout(refreshMaximised, 80); }
  function close() { Quit?.(); }
</script>

<div
  class="h-8 flex items-center flex-shrink-0 wails-drag select-none"
  style="background-color: var(--bg-secondary); border-bottom: 1px solid var(--border);"
  on:dblclick={toggleMaximise}
>
  <div class="flex items-center gap-2 px-3 pointer-events-none">
    <img src={logoUrl} alt="" class="w-4 h-4 logo-tint" />
    <span class="text-[11px] font-tech uppercase tracking-wider" style="color: var(--text-secondary);">TheVault</span>
  </div>

  <div class="flex-1"></div>

  <div class="flex items-stretch h-full no-drag">
    <button
      class="titlebar-btn"
      on:click={minimise}
      aria-label="Minimise"
      title="Minimise"
    >
      <svg width="10" height="10" viewBox="0 0 10 10"><rect x="0" y="4.5" width="10" height="1" fill="currentColor" /></svg>
    </button>
    <button
      class="titlebar-btn"
      on:click={toggleMaximise}
      aria-label={isMaximised ? 'Restore' : 'Maximise'}
      title={isMaximised ? 'Restore' : 'Maximise'}
    >
      {#if isMaximised}
        <svg width="10" height="10" viewBox="0 0 10 10">
          <rect x="2" y="0" width="8" height="8" fill="none" stroke="currentColor" stroke-width="1" />
          <rect x="0" y="2" width="8" height="8" fill="var(--bg-secondary)" stroke="currentColor" stroke-width="1" />
        </svg>
      {:else}
        <svg width="10" height="10" viewBox="0 0 10 10"><rect x="0" y="0" width="10" height="10" fill="none" stroke="currentColor" stroke-width="1" /></svg>
      {/if}
    </button>
    <button
      class="titlebar-btn titlebar-btn-close"
      on:click={close}
      aria-label="Close"
      title="Close"
    >
      <svg width="10" height="10" viewBox="0 0 10 10">
        <line x1="0" y1="0" x2="10" y2="10" stroke="currentColor" stroke-width="1" />
        <line x1="10" y1="0" x2="0" y2="10" stroke="currentColor" stroke-width="1" />
      </svg>
    </button>
  </div>
</div>

<style>
  .titlebar-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    color: var(--text-secondary);
    background: transparent;
    transition: background-color 120ms ease, color 120ms ease;
  }
  .titlebar-btn:hover {
    background-color: rgba(var(--accent-rgb), 0.15);
    color: var(--accent);
  }
  .titlebar-btn-close:hover {
    background-color: var(--error, #ff3333);
    color: #fff;
  }
</style>
