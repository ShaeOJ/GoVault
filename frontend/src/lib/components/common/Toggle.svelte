<script lang="ts">
  export let checked: boolean = false;
  export let label: string = '';
  export let disabled: boolean = false;
</script>

<label class="flex items-center cursor-pointer {disabled ? 'opacity-50 cursor-not-allowed' : ''}">
  <div
    class="toggle-switch {checked ? 'active' : ''}"
    on:click|preventDefault={() => { if (!disabled) checked = !checked; }}
    on:keydown|preventDefault={(e) => { if (e.key === ' ' && !disabled) checked = !checked; }}
    role="switch"
    aria-checked={checked}
    tabindex="0"
  ></div>
  {#if label}
    <span class="ml-3 text-sm text-gray-300">{label}</span>
  {/if}
</label>
