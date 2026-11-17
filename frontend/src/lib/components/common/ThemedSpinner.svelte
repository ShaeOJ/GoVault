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
  {:else if currentTheme === 'synthwave'}
    <!-- Retro sun: half-disc crossed by horizontal bars -->
    <path d="M4 14a8 8 0 0 1 16 0z" fill="currentColor" opacity="0.5"/>
    <line x1="4.5" y1="11" x2="19.5" y2="11" stroke="currentColor" stroke-width="1.3" opacity="0.8"/>
    <line x1="3.3" y1="14" x2="20.7" y2="14" stroke="currentColor" stroke-width="1.3" opacity="0.8"/>
    <circle cx="12" cy="14" r="10" stroke="currentColor" stroke-width="1.5" opacity="0.2" fill="none"/>
  {:else if currentTheme === 'mono'}
    <!-- Blinking terminal cursor -->
    <rect x="10" y="3" width="4" height="18" rx="1" fill="currentColor" opacity="0.85"/>
  {:else if currentTheme === 'light'}
    <!-- Sun with rays -->
    <circle cx="12" cy="12" r="5" fill="currentColor" opacity="0.85"/>
    <line x1="12" y1="1" x2="12" y2="4" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.6"/>
    <line x1="12" y1="20" x2="12" y2="23" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.6"/>
    <line x1="1" y1="12" x2="4" y2="12" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.6"/>
    <line x1="20" y1="12" x2="23" y2="12" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.6"/>
    <line x1="4.2" y1="4.2" x2="6.3" y2="6.3" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.6"/>
    <line x1="17.7" y1="17.7" x2="19.8" y2="19.8" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.6"/>
    <line x1="4.2" y1="19.8" x2="6.3" y2="17.7" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.6"/>
    <line x1="17.7" y1="6.3" x2="19.8" y2="4.2" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.6"/>
    <circle cx="12" cy="18" r="2" fill="currentColor" opacity="0.6"/>
  {:else}
    <!-- Fallback: simple arc spinner -->
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="2" opacity="0.15"/>
    <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"/>
  {/if}
</svg>
