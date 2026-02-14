<script lang="ts">
  import Icon from '../common/Icon.svelte';
  import logoUrl from '../../../assets/images/logo.png';

  export let currentPage: string = 'dashboard';

  const navItems = [
    { id: 'dashboard', label: 'Dashboard', icon: 'reactor' },
    { id: 'miners', label: 'Miners', icon: 'chip' },
    { id: 'node', label: 'Node', icon: 'server-rack' },
    { id: 'settings', label: 'Settings', icon: 'terminal' },
    { id: 'logs', label: 'Logs', icon: 'datastream' },
  ];
</script>

<aside
  class="w-16 lg:w-56 h-full flex flex-col flex-shrink-0"
  style="background-color: var(--bg-secondary); border-right: 1px solid var(--accent); box-shadow: 1px 0 8px var(--accent-glow);"
>
  <!-- Logo / Brand -->
  <div class="h-16 flex items-center px-3 lg:px-4 wails-drag" style="border-bottom: 1px solid var(--border);">
    <div class="flex items-center gap-2 flex-shrink-0">
      <img src={logoUrl} alt="GoVault" class="w-7 h-7" />
      <span class="hidden lg:block font-tech text-sm uppercase tracking-widest glow-text" style="color: var(--accent);">
        GOVAULT
      </span>
    </div>
  </div>

  <!-- Navigation -->
  <nav class="flex-1 py-4 px-2 space-y-1">
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

  <!-- Status indicator at bottom -->
  <div class="p-3" style="border-top: 1px solid var(--border);">
    <div class="flex items-center px-2">
      <div
        class="w-2 h-2 rounded-full status-pulse"
        style="background-color: var(--accent); box-shadow: 0 0 6px var(--accent-glow);"
      ></div>
      <span class="ml-2 text-xs font-data hidden lg:block" style="color: var(--text-secondary);">v0.1.0</span>
    </div>
  </div>
</aside>
