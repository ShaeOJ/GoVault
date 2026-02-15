<script lang="ts">
  import { theme } from '../../stores/theme';

  export let size: number = 16;
  export let mode: 'inline' | 'block' = 'inline';

  $: currentTheme = $theme;
</script>

<svg
  class="themed-spinner"
  class:block={mode === 'block'}
  class:mx-auto={mode === 'block'}
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  style="color: currentColor;"
>
  {#if currentTheme === 'nuclear'}
    <!-- Radiation trefoil (3 blades) -->
    <circle cx="12" cy="12" r="3" fill="currentColor" opacity="0.6"/>
    <path d="M12 2a10 10 0 0 1 8.66 5l-4.33 2.5A4.97 4.97 0 0 0 12 7V2z" fill="currentColor" opacity="0.75"/>
    <path d="M20.66 7a10 10 0 0 1 0 10l-4.33-2.5A4.97 4.97 0 0 0 17 12h5.66z" fill="currentColor" opacity="0.75" transform="rotate(0 12 12)"/>
    <path d="M3.34 7a10 10 0 0 0 0 10l4.33-2.5A4.97 4.97 0 0 1 7 12H1.34z" fill="currentColor" opacity="0.75"/>
  {:else if currentTheme === 'tron'}
    <!-- Identity disc (half-ring arc) -->
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="2" opacity="0.15"/>
    <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/>
  {:else if currentTheme === 'vault-tec'}
    <!-- Gear cog with inner circle -->
    <circle cx="12" cy="12" r="4" stroke="currentColor" stroke-width="1.5" fill="none" opacity="0.6"/>
    <path d="M12 1v3M12 20v3M1 12h3M20 12h3M4.22 4.22l2.12 2.12M17.66 17.66l2.12 2.12M4.22 19.78l2.12-2.12M17.66 6.34l2.12-2.12"
      stroke="currentColor" stroke-width="2" stroke-linecap="round"/>
    <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.5" fill="none" opacity="0.25"/>
  {:else if currentTheme === 'crimson'}
    <!-- Pulsing ember ring (dashed) -->
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="2.5" stroke-dasharray="8 4" stroke-linecap="round" opacity="0.85"/>
    <circle cx="12" cy="12" r="5" stroke="currentColor" stroke-width="1" opacity="0.2" fill="none"/>
  {:else if currentTheme === 'ultraviolet'}
    <!-- Ring with orbiting dot -->
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="1.5" opacity="0.15" fill="none"/>
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="1.5" stroke-dasharray="4 58.8" stroke-linecap="round" opacity="0.4" fill="none"/>
    <circle cx="12" cy="2" r="2" fill="currentColor" opacity="0.9"/>
  {:else}
    <!-- Plasma: Ring with zigzag lightning path -->
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="1.5" opacity="0.2" fill="none"/>
    <path d="M12 2a10 10 0 0 1 8.66 5" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/>
    <path d="M14 7l-3 4h4l-3 4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" opacity="0.5"/>
  {/if}
</svg>
