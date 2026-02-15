<script lang="ts">
  import Tooltip from './Tooltip.svelte';

  export let tip: string = '';
  export let position: 'top' | 'bottom' | 'left' | 'right' | 'auto' = 'auto';
  export let size: number = 14;
</script>

<Tooltip text={tip} {position}>
  <svg
    class="info-icon"
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
  >
    <circle cx="12" cy="12" r="10" />
    <line x1="12" y1="16" x2="12" y2="12" />
    <line x1="12" y1="8" x2="12.01" y2="8" />
  </svg>
</Tooltip>

<style>
  .info-icon {
    color: var(--text-secondary);
    opacity: 0.6;
    cursor: help;
    flex-shrink: 0;
    transition: color 150ms, opacity 150ms;
  }
  .info-icon:hover {
    color: var(--accent);
    opacity: 1;
  }
</style>
