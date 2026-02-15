<script lang="ts">
  import { tick, onDestroy } from 'svelte';

  export let text: string = '';
  export let position: 'top' | 'bottom' | 'left' | 'right' | 'auto' = 'auto';
  export let delay: number = 300;
  export let maxWidth: number = 260;

  let triggerEl: HTMLElement;
  let timer: ReturnType<typeof setTimeout>;
  let tooltipDiv: HTMLDivElement | null = null;

  function show() {
    timer = setTimeout(() => {
      createTooltip();
    }, delay);
  }

  function hide() {
    clearTimeout(timer);
    removeTooltip();
  }

  function createTooltip() {
    if (!triggerEl || !text) return;
    removeTooltip();

    const tr = triggerEl.getBoundingClientRect();

    // Create tooltip element on body
    tooltipDiv = document.createElement('div');
    tooltipDiv.className = 'gv-tooltip';
    tooltipDiv.textContent = text;
    tooltipDiv.style.maxWidth = `${maxWidth}px`;
    document.body.appendChild(tooltipDiv);

    // Create arrow
    const arrow = document.createElement('span');
    arrow.className = 'gv-tooltip-arrow';
    tooltipDiv.appendChild(arrow);

    // Measure
    const tp = tooltipDiv.getBoundingClientRect();

    // Resolve position
    let pos = position === 'auto' ? 'top' : position;
    if (position === 'auto' && tr.top - tp.height - 8 < 4) {
      pos = 'bottom';
    }

    let top: number, left: number;
    if (pos === 'top') {
      top = tr.top - tp.height - 8;
      left = tr.left + tr.width / 2 - tp.width / 2;
    } else if (pos === 'bottom') {
      top = tr.bottom + 8;
      left = tr.left + tr.width / 2 - tp.width / 2;
    } else if (pos === 'left') {
      top = tr.top + tr.height / 2 - tp.height / 2;
      left = tr.left - tp.width - 8;
    } else {
      top = tr.top + tr.height / 2 - tp.height / 2;
      left = tr.right + 8;
    }

    // Clamp horizontal
    if (left < 6) left = 6;
    if (left + tp.width > window.innerWidth - 6) left = window.innerWidth - tp.width - 6;

    tooltipDiv.style.top = `${top}px`;
    tooltipDiv.style.left = `${left}px`;
    tooltipDiv.classList.add(`gv-tt-${pos}`);
    tooltipDiv.classList.add('gv-tooltip-visible');
  }

  function removeTooltip() {
    if (tooltipDiv) {
      tooltipDiv.remove();
      tooltipDiv = null;
    }
  }

  onDestroy(() => {
    clearTimeout(timer);
    removeTooltip();
  });
</script>

<span
  class="gv-tooltip-trigger"
  bind:this={triggerEl}
  on:mouseenter={show}
  on:mouseleave={hide}
  on:focus={show}
  on:blur={hide}
>
  <slot />
</span>

<style>
  .gv-tooltip-trigger {
    position: relative;
    display: inline-flex;
    align-items: center;
  }

  /* Portal styles must be global since element is appended to body */
  :global(.gv-tooltip) {
    position: fixed;
    z-index: 99999;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 11px;
    line-height: 1.45;
    font-family: var(--font-mono, 'JetBrains Mono', monospace);
    color: var(--text-primary);
    background: var(--bg-card);
    border: 1px solid rgba(var(--accent-rgb), 0.25);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.45), 0 0 8px rgba(var(--accent-rgb), 0.08);
    white-space: normal;
    word-wrap: break-word;
    pointer-events: none;
    opacity: 0;
    transition: opacity 150ms ease-out;
  }

  :global(.gv-tooltip.gv-tooltip-visible) {
    opacity: 1;
  }

  :global(.gv-tooltip-arrow) {
    position: absolute;
    width: 8px;
    height: 8px;
    background: var(--bg-card);
    border: 1px solid rgba(var(--accent-rgb), 0.25);
    transform: rotate(45deg);
  }

  :global(.gv-tt-top .gv-tooltip-arrow) {
    bottom: -5px;
    left: 50%;
    margin-left: -4px;
    border-top: none;
    border-left: none;
  }

  :global(.gv-tt-bottom .gv-tooltip-arrow) {
    top: -5px;
    left: 50%;
    margin-left: -4px;
    border-bottom: none;
    border-right: none;
  }

  :global(.gv-tt-left .gv-tooltip-arrow) {
    right: -5px;
    top: 50%;
    margin-top: -4px;
    border-bottom: none;
    border-left: none;
  }

  :global(.gv-tt-right .gv-tooltip-arrow) {
    left: -5px;
    top: 50%;
    margin-top: -4px;
    border-top: none;
    border-right: none;
  }
</style>
