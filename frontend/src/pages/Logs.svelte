<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { logs } from '../lib/stores/logs';
  import type { LogEntry } from '../lib/stores/logs';
  import { EventsOn } from '../../wailsjs/runtime/runtime';

  let logList: LogEntry[] = [];
  let autoScroll = true;
  let filterLevel = 'all';
  let filterText = '';
  let logContainer: HTMLDivElement;
  let unsubLogs: () => void;
  let unsubEvent: () => void;

  const unsub = logs.subscribe(l => {
    logList = l;
    if (autoScroll && logContainer) {
      setTimeout(() => {
        logContainer.scrollTop = logContainer.scrollHeight;
      }, 10);
    }
  });

  onMount(async () => {
    unsubEvent = EventsOn('log:entry', (entry: LogEntry) => {
      logs.update(l => {
        const updated = [...l, entry];
        if (updated.length > 1000) return updated.slice(-1000);
        return updated;
      });
    });

    // Load existing logs
    try {
      const { GetRecentLogs } = await import('../../wailsjs/go/main/App');
      const existing = await GetRecentLogs(200);
      if (existing) logs.set(existing);
    } catch {}
  });

  onDestroy(() => {
    unsub();
    if (unsubEvent) unsubEvent();
  });

  $: filteredLogs = logList.filter(l => {
    if (filterLevel !== 'all' && l.level !== filterLevel) return false;
    if (filterText && !l.message.toLowerCase().includes(filterText.toLowerCase()) && !l.component.toLowerCase().includes(filterText.toLowerCase())) return false;
    return true;
  });

  function levelColor(level: string): string {
    switch (level) {
      case 'debug': return 'var(--text-secondary)';
      case 'info': return 'var(--accent)';
      case 'warn': return 'var(--warning)';
      case 'error': return 'var(--error)';
      default: return 'var(--text-secondary)';
    }
  }

  function clearLogs() {
    logs.set([]);
  }
</script>

<div class="flex flex-col h-full space-y-4">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-bold font-tech uppercase tracking-wide" style="color: var(--text-primary);">Logs</h1>
      <p class="text-sm font-data" style="color: var(--text-secondary);">{filteredLogs.length} entries</p>
    </div>
    <div class="flex items-center gap-3">
      <label class="flex items-center gap-2 text-sm cursor-pointer" style="color: var(--text-secondary);">
        <input type="checkbox" bind:checked={autoScroll} class="rounded" style="accent-color: var(--accent);" />
        Auto-scroll
      </label>
      <button
        class="px-3 py-1.5 text-xs rounded-lg font-tech uppercase tracking-wider transition-colors"
        style="background-color: var(--bg-card); color: var(--text-secondary); border: 1px solid var(--border);"
        on:click={clearLogs}
      >
        Clear
      </button>
    </div>
  </div>

  <!-- Filters -->
  <div class="flex items-center gap-3">
    <div class="flex gap-1">
      {#each ['all', 'debug', 'info', 'warn', 'error'] as level}
        <button
          class="px-2.5 py-1 text-xs rounded-md transition-all font-data"
          style={filterLevel === level
            ? `background: rgba(var(--accent-rgb), 0.15); color: var(--accent); box-shadow: 0 0 4px var(--accent-glow);`
            : `color: var(--text-secondary);`}
          on:click={() => filterLevel = level}
        >
          {level === 'all' ? 'All' : level.charAt(0).toUpperCase() + level.slice(1)}
        </button>
      {/each}
    </div>
    <input
      bind:value={filterText}
      class="flex-1 rounded-lg px-3 py-1.5 text-sm input-themed font-data"
      placeholder="Filter logs..."
    />
  </div>

  <!-- Log Entries -->
  <div
    bind:this={logContainer}
    class="flex-1 rounded-xl overflow-y-auto font-data text-xs scan-lines card-glow"
    style="background-color: var(--bg-card);"
  >
    {#if filteredLogs.length === 0}
      <div class="p-8 text-center" style="color: var(--text-secondary);">No log entries</div>
    {:else}
      {#each filteredLogs as entry}
        <div class="px-3 py-1.5 flex gap-3 transition-colors relative z-10 log-entry-hover" style="border-bottom: 1px solid rgba(var(--accent-rgb), 0.03);"
        >
          <span class="whitespace-nowrap flex-shrink-0" style="color: var(--text-secondary); opacity: 0.5;">{entry.timestamp}</span>
          <span
            class="w-14 flex-shrink-0 uppercase font-bold rounded px-1 text-center glow-text"
            style="color: {levelColor(entry.level)}; text-shadow: 0 0 4px {levelColor(entry.level)}40;"
          >{entry.level}</span>
          <span class="w-20 flex-shrink-0 truncate" style="color: var(--text-secondary); opacity: 0.7;">[{entry.component}]</span>
          <span style="color: var(--text-primary); word-break: break-all;">{entry.message}</span>
        </div>
      {/each}
    {/if}
  </div>
</div>
