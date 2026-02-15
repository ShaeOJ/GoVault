<script lang="ts">
  import Icon from './Icon.svelte';
  import Info from './Info.svelte';

  export let label: string;
  export let value: string;
  export let subtext: string = '';
  export let icon: string = '';
  export let iconName: string = '';
  export let color: string = 'accent';
  export let tooltip: string = '';

  // Map color prop to CSS for the left accent bar
  const barColors: Record<string, string> = {
    accent: 'var(--accent)',
    green: 'var(--success)',
    gold: 'var(--warning)',
    red: 'var(--error)',
    gray: 'var(--text-secondary)',
    electric: 'var(--accent)',
  };

  $: barColor = barColors[color] || barColors.accent;
</script>

<div
  class="rounded-xl p-4 card-glow relative overflow-hidden"
  style="background-color: var(--bg-card);"
>
  <!-- Left accent bar -->
  <div
    class="absolute left-0 top-0 bottom-0 w-[3px]"
    style="background: {barColor}; box-shadow: 0 0 6px {barColor}40;"
  ></div>

  <div class="flex items-center justify-between mb-2 pl-2">
    <span class="text-xs font-medium uppercase tracking-wider inline-flex items-center gap-1" style="color: var(--text-secondary);">
      {label}
      {#if tooltip}<Info tip={tooltip} size={12} />{/if}
    </span>
    {#if iconName}
      <div style="color: {barColor}; opacity: 0.7;">
        <Icon name={iconName} size={16} />
      </div>
    {:else if icon}
      <svg class="w-4 h-4" style="color: {barColor}; opacity: 0.7;" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d={icon}/>
      </svg>
    {/if}
  </div>
  <div class="text-2xl font-bold data-readout truncate pl-2" style="color: {barColor}; text-shadow: 0 0 4px {barColor}40;">
    {value}
  </div>
  {#if $$slots.subtext}
    <div class="text-xs mt-1 pl-2" style="color: var(--text-secondary);">
      <slot name="subtext" />
    </div>
  {:else if subtext}
    <div class="text-xs mt-1 pl-2" style="color: var(--text-secondary);">{subtext}</div>
  {/if}
</div>
