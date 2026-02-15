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
  {:else if currentTheme === 'bitcoin'}
    <!-- Coin-like dashed ring with ₿ center -->
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="2" stroke-dasharray="5 3" stroke-linecap="round" opacity="0.85"/>
    <circle cx="12" cy="12" r="5" stroke="currentColor" stroke-width="1" opacity="0.2" fill="none"/>
    <text x="12" y="16" text-anchor="middle" fill="currentColor" font-size="10" font-weight="bold" opacity="0.7">₿</text>
  {:else if currentTheme === 'monochrome'}
    <!-- Blinking terminal cursor -->
    <rect x="10" y="3" width="4" height="18" rx="1" fill="currentColor" opacity="0.85"/>
  {:else if currentTheme === 'steampunk'}
    <!-- Pressure gauge arc with needle -->
    <path d="M4 18a9.5 9.5 0 0 1 16 0" stroke="currentColor" stroke-width="2" fill="none" opacity="0.3" stroke-linecap="round"/>
    <path d="M6 16a7 7 0 0 1 12 0" stroke="currentColor" stroke-width="1.5" fill="none" opacity="0.15" stroke-linecap="round"/>
    <!-- Tick marks -->
    <line x1="4" y1="18" x2="5.5" y2="17" stroke="currentColor" stroke-width="1.5" opacity="0.5"/>
    <line x1="7" y1="11" x2="8.2" y2="12" stroke="currentColor" stroke-width="1.5" opacity="0.5"/>
    <line x1="12" y1="8.5" x2="12" y2="10" stroke="currentColor" stroke-width="1.5" opacity="0.5"/>
    <line x1="17" y1="11" x2="15.8" y2="12" stroke="currentColor" stroke-width="1.5" opacity="0.5"/>
    <line x1="20" y1="18" x2="18.5" y2="17" stroke="currentColor" stroke-width="1.5" opacity="0.5"/>
    <!-- Needle -->
    <line x1="12" y1="18" x2="8" y2="11" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.9"/>
    <circle cx="12" cy="18" r="2" fill="currentColor" opacity="0.6"/>
  {:else}
    <!-- Fallback: simple arc spinner -->
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="2" opacity="0.15"/>
    <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/>
  {/if}
</svg>
