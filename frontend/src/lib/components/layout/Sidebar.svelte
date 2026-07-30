<script lang="ts">
  import Icon from '../common/Icon.svelte';
  import MinerGlobe from '../common/MinerGlobe.svelte';
  import logoUrl from '../../../assets/images/logo.png';

  export let currentPage: string = 'dashboard';

  const navItems = [
    { id: 'dashboard', label: 'Dashboard', icon: 'reactor' },
    { id: 'miners', label: 'Miners', icon: 'chip' },
    { id: 'node', label: 'Setup', icon: 'server-rack' },
    { id: 'settings', label: 'Settings', icon: 'terminal' },
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
    <img src={logoUrl} alt="GoVault" class="w-10 h-10 lg:w-11 lg:h-11 logo-tint" />
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

  <!-- Shutdown + status at bottom -->
  <div class="p-3 space-y-2" style="border-top: 1px solid var(--border);">
    {#if confirmShutdown}
      <div class="flex gap-1.5">
        <button
          class="flex-1 flex items-center justify-center px-2 py-2 rounded-lg text-xs font-tech uppercase tracking-wider transition-all"
          style="background: rgba(255,50,50,0.14); color: var(--error); border: 1px solid var(--error);"
          on:click={doShutdown}
          title="Fully quit GoVault and stop mining"
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
        title="Closing the window (X) keeps GoVault running in the background so mining continues. Use Shutdown to fully quit; relaunch GoVault to reopen."
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
      <span class="ml-2 text-xs font-data hidden lg:block" style="color: var(--text-secondary);">v0.2.0-beta.5</span>
    </div>
  </div>
</aside>
