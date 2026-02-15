<script lang="ts">
  export let name: string;
  export let size: number = 24;

  // All paths designed for a 24x24 viewBox, geometric/tech style
  const icons: Record<string, { paths: string[]; filled?: string[] }> = {
    // Dashboard nav — Atom/reactor: circle with 3 elliptical orbits
    reactor: {
      paths: [
        'M12 12m-2 0a2 2 0 1 0 4 0a2 2 0 1 0 -4 0',
        'M12 3C7.03 3 3 7.03 3 12s4.03 9 9 9 9-4.03 9-9-4.03-9-9-9',
        'M12 7.5c3.5 0 6.5 1.5 6.5 4.5s-3 4.5-6.5 4.5S5.5 15 5.5 12 8.5 7.5 12 7.5',
        'M8.5 4.5c1.75 3.04 1.75 12.46 0 15.5',
        'M15.5 4.5c-1.75 3.04-1.75 12.46 0 15.5',
      ],
    },
    // Miners nav — Chip/CPU: rectangle with pin lines
    chip: {
      paths: [
        'M7 7h10v10H7z',
        'M9 7V4', 'M12 7V4', 'M15 7V4',
        'M9 17v3', 'M12 17v3', 'M15 17v3',
        'M7 9H4', 'M7 12H4', 'M7 15H4',
        'M17 9h3', 'M17 12h3', 'M17 15h3',
        'M9.5 9.5h5v5h-5z',
      ],
    },
    // Node nav — Server rack: stacked rectangles with dots
    'server-rack': {
      paths: [
        'M4 4h16v5H4z',
        'M4 11h16v5H4z',
        'M7 6.5h.01', 'M7 13.5h.01',
        'M17 6.5h.01', 'M17 13.5h.01',
        'M10 19h4', 'M12 16v3',
      ],
    },
    // Settings nav — Terminal: rectangle with >_ cursor
    terminal: {
      paths: [
        'M3 5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5z',
        'M7 10l3 2-3 2',
        'M13 14h4',
      ],
    },
    // Logs nav — Datastream: parallel lines with flowing dots
    datastream: {
      paths: [
        'M4 7h16', 'M4 12h16', 'M4 17h16',
      ],
      filled: [
        'M18 7a1 1 0 1 0 0-2 1 1 0 0 0 0 2z',
        'M14 12a1 1 0 1 0 0-2 1 1 0 0 0 0 2z',
        'M20 17a1 1 0 1 0 0-2 1 1 0 0 0 0 2z',
      ],
    },
    // Hashrate — Bolt: angular geometric lightning
    bolt: {
      paths: [
        'M13 2L4 14h7l-1 8 9-12h-7l1-8z',
      ],
    },
    // Sidebar brand — Pickaxe: crossed pickaxes forming X
    pickaxe: {
      paths: [
        'M5 5l14 14', 'M19 5L5 19',
        'M2 2l4.5 1.5L5 5 3.5 6.5z',
        'M22 2l-4.5 1.5L19 5l1.5 1.5z',
        'M2 22l4.5-1.5L5 19l-1.5-1.5z',
        'M22 22l-4.5-1.5L19 19l1.5-1.5z',
      ],
    },
    // Best Difficulty — Hexshield: hexagon outline
    hexshield: {
      paths: [
        'M12 2l8 4.5v7L12 22l-8-8.5v-7z',
        'M12 8l4 2.25v3.5L12 16l-4-2.25v-3.5z',
      ],
    },
    // Est Time to Block — Gauge: circle with needle/tick marks
    gauge: {
      paths: [
        'M12 21a9 9 0 1 1 0-18 9 9 0 0 1 0 18z',
        'M12 12l-3-4.5',
        'M5.5 16h.01', 'M18.5 16h.01',
        'M7 8h.01', 'M17 8h.01',
        'M12 5v1',
      ],
    },
    // Network/Active — Nodes: 3 circles connected by lines
    nodes: {
      paths: [
        'M6 6l6 6', 'M18 6l-6 6', 'M12 12v6',
      ],
      filled: [
        'M6 6a2 2 0 1 0 0-4 2 2 0 0 0 0 4z',
        'M18 6a2 2 0 1 0 0-4 2 2 0 0 0 0 4z',
        'M12 22a2 2 0 1 0 0-4 2 2 0 0 0 0 4z',
        'M12 14a2 2 0 1 0 0-4 2 2 0 0 0 0 4z',
      ],
    },
    // Blocks Found — Cube: 3D wireframe cube
    cube: {
      paths: [
        'M12 2l8 4v8l-8 4-8-4V6z',
        'M12 10v12',
        'M4 6l8 4',
        'M20 6l-8 4',
      ],
    },
    // Block Chance — Dice: die face with dots
    dice: {
      paths: [
        'M4 4h16v16H4z',
      ],
      filled: [
        'M8 8a1 1 0 1 0 0-2 1 1 0 0 0 0 2z',
        'M12 13a1 1 0 1 0 0-2 1 1 0 0 0 0 2z',
        'M16 18a1 1 0 1 0 0-2 1 1 0 0 0 0 2z',
      ],
    },
    // Reconnect — Refresh: circular arrows
    refresh: {
      paths: [
        'M1 4v6h6',
        'M23 20v-6h-6',
        'M20.49 9A9 9 0 0 0 5.64 5.64L1 10',
        'M23 14l-4.64 4.36A9 9 0 0 1 3.51 15',
      ],
    },
    // Signal/Active miners — Waves: concentric arcs
    waves: {
      paths: [
        'M12 18h.01',
        'M8.5 14.5a5 5 0 0 1 7 0',
        'M5.5 11.5a9 9 0 0 1 13 0',
        'M2.5 8.5a13 13 0 0 1 19 0',
      ],
    },
  };

  $: icon = icons[name];
</script>

{#if icon}
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    class="flex-shrink-0"
  >
    {#each icon.paths as d}
      <path {d} />
    {/each}
    {#if icon.filled}
      {#each icon.filled as d}
        <path d={d} fill="currentColor" stroke="none" />
      {/each}
    {/if}
  </svg>
{/if}
