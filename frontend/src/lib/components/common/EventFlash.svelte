<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { EventsOn } from '../../../../wailsjs/runtime/runtime';

  let flashType: 'share' | 'block' | null = null;
  let flashKey = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let unsubs: (() => void)[] = [];

  function triggerFlash(type: 'block') {
    if (timer) clearTimeout(timer);
    flashType = type;
    flashKey++;
    timer = setTimeout(() => { flashType = null; }, 2000);
  }

  onMount(() => {
    unsubs.push(EventsOn('stratum:block-found', () => {
      triggerFlash('block');
    }));
  });

  onDestroy(() => {
    unsubs.forEach(fn => fn());
    if (timer) clearTimeout(timer);
  });
</script>

{#if flashType}
  {#key flashKey}
    <div
      class="event-flash--{flashType}"
      style="position: fixed; inset: 0; pointer-events: none; z-index: 100;"
    ></div>
  {/key}
{/if}
