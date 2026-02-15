<script lang="ts">
  import { onMount } from 'svelte';
  import Toggle from '../lib/components/common/Toggle.svelte';
  import Info from '../lib/components/common/Info.svelte';
  import { theme } from '../lib/stores/theme';
  import type { ThemeName } from '../lib/stores/theme';

  let stratumPort = 10333;
  let maxConn = 100;
  let autoStart = false;
  let payoutAddress = '';
  let coinbaseTag = '/GoVault/';
  let minDiff = 0.001;
  let maxDiff = 0;
  let targetTimeSec = 15;
  let retargetTimeSec = 90;
  let variancePct = 30;
  let logLevel = 'info';
  let selectedCoin = 'btc';

  let saving = false;
  let saveMsg = '';
  let addressValid: boolean | null = null;
  let addressType = '';
  let stratumURL = '';
  let coinList: Array<{id: string; name: string; symbol: string; defaultRPCPort: number; defaultRPCUser: string; segwit: boolean}> = [];
  let dbPath = '';
  let dbSize = 0;

  // Theme options
  const themes: { id: ThemeName; label: string; accent: string; desc: string }[] = [
    { id: 'nuclear', label: 'NUCLEAR', accent: '#39ff14', desc: 'Radioactive green' },
    { id: 'tron', label: 'TRON', accent: '#00d4ff', desc: 'Neon cyan' },
    { id: 'vault-tec', label: 'VAULT-TEC', accent: '#f5a623', desc: 'Retro amber' },
    { id: 'bitcoin', label: 'BITCOIN', accent: '#f7931a', desc: 'Bitcoin orange' },
    { id: 'monochrome', label: 'MONO', accent: '#cccccc', desc: 'CRT terminal' },
    { id: 'steampunk', label: 'STEAMPUNK', accent: '#cd7f32', desc: 'Brass & iron' },
  ];

  // Address placeholder map per coin
  const addressPlaceholders: Record<string, string> = {
    btc: 'bc1q...',
    bch: 'bitcoincash:q...',
    dgb: 'D...',
    bc2: 'bc1q...',
    xec: 'ecash:q...',
  };

  onMount(async () => {
    try {
      const { GetConfig, GetStratumURL, GetCoinList, GetDatabaseInfo } = await import('../../wailsjs/go/main/App');
      coinList = await GetCoinList() || [];
      const cfg = await GetConfig();
      if (cfg) {
        stratumPort = cfg.stratum?.port || 10333;
        maxConn = cfg.stratum?.maxConn || 100;
        autoStart = cfg.stratum?.autoStart || false;
        selectedCoin = cfg.mining?.coin || 'btc';
        payoutAddress = cfg.mining?.payoutAddress || '';
        coinbaseTag = cfg.mining?.coinbaseTag || '/GoVault/';
        minDiff = cfg.vardiff?.minDiff || 0.001;
        maxDiff = cfg.vardiff?.maxDiff || 0;
        targetTimeSec = cfg.vardiff?.targetTimeSec || 15;
        retargetTimeSec = cfg.vardiff?.retargetTimeSec || 90;
        variancePct = cfg.vardiff?.variancePct || 30;
        logLevel = cfg.app?.logLevel || 'info';
      }
      stratumURL = await GetStratumURL();
      const dbInfo = await GetDatabaseInfo();
      if (dbInfo) {
        dbPath = dbInfo.path || '';
        dbSize = dbInfo.size || 0;
      }
    } catch {}

    if (payoutAddress) validateAddress();
  });

  function onCoinChange() {
    // Reset address validation when coin changes
    addressValid = null;
    addressType = '';
    if (payoutAddress) validateAddress();
  }

  async function validateAddress() {
    if (!payoutAddress) {
      addressValid = null;
      addressType = '';
      return;
    }
    try {
      const { ValidateAddress } = await import('../../wailsjs/go/main/App');
      const result = await ValidateAddress(payoutAddress, selectedCoin);
      addressValid = result?.valid || false;
      addressType = result?.type || '';
    } catch {
      addressValid = false;
    }
  }

  async function save() {
    saving = true;
    saveMsg = '';
    try {
      const { GetConfig, UpdateConfig } = await import('../../wailsjs/go/main/App');
      const cfg = await GetConfig();
      cfg.stratum = { port: stratumPort, maxConn, autoStart };
      cfg.mining = { coin: selectedCoin, payoutAddress, coinbaseTag };
      cfg.vardiff = { minDiff, maxDiff, targetTimeSec, retargetTimeSec, variancePct };
      cfg.app = { ...cfg.app, logLevel };
      await UpdateConfig(cfg);
      saveMsg = 'Settings saved!';
      setTimeout(() => saveMsg = '', 3000);
    } catch (e: any) {
      saveMsg = `Error: ${e?.message || e}`;
    }
    saving = false;
  }

  function selectTheme(t: ThemeName) {
    theme.set(t);
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB'];
    const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
    const val = bytes / Math.pow(1024, i);
    return `${val < 10 ? val.toFixed(2) : val < 100 ? val.toFixed(1) : val.toFixed(0)} ${units[i]}`;
  }

  $: currentCoinName = coinList.find(c => c.id === selectedCoin)?.name || 'Bitcoin';
  $: currentCoinSymbol = coinList.find(c => c.id === selectedCoin)?.symbol || 'BTC';
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-bold font-tech uppercase tracking-wide" style="color: var(--text-primary);">Settings</h1>
      <p class="text-sm" style="color: var(--text-secondary);">Configure your stratum server</p>
    </div>
    <div class="flex items-center gap-3">
      {#if saveMsg}
        <span class="text-sm font-data" style="color: {saveMsg.startsWith('Error') ? 'var(--error)' : 'var(--success)'};">{saveMsg}</span>
      {/if}
      <button
        class="px-4 py-2 rounded-lg text-sm font-medium font-tech uppercase tracking-wider transition-all glow-border-hover {saving ? 'opacity-50' : ''}"
        style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid var(--accent);"
        on:click={save}
        disabled={saving}
      >
        {saving ? 'Saving...' : 'Save Settings'}
      </button>
    </div>
  </div>

  <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
    <!-- Wallet / Mining -->
    <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
      <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Mining</h3>
      <div class="space-y-4">
        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="coin">Blockchain <Info tip="Select which cryptocurrency to solo mine" size={12} /></label>
          <select
            id="coin"
            bind:value={selectedCoin}
            on:change={onCoinChange}
            class="w-full rounded-lg px-3 py-2 text-sm select-themed"
          >
            {#each coinList as c}
              <option value={c.id}>{c.name} ({c.symbol})</option>
            {/each}
            {#if coinList.length === 0}
              <option value="btc">Bitcoin (BTC)</option>
            {/if}
          </select>
          <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">Select which coin to solo mine</div>
        </div>
        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="payout">{currentCoinSymbol} Payout Address <Info tip="Wallet address for block rewards. Must match selected blockchain" size={12} /></label>
          <input
            id="payout"
            bind:value={payoutAddress}
            on:blur={validateAddress}
            class="w-full rounded-lg px-3 py-2 text-sm input-themed"
            style={addressValid === true ? 'border-color: var(--success);' : addressValid === false ? 'border-color: var(--error);' : ''}
            placeholder={addressPlaceholders[selectedCoin] || 'bc1q...'}
          />
          {#if addressValid === true}
            <div class="text-xs mt-1" style="color: var(--success);">{addressType}</div>
          {:else if addressValid === false}
            <div class="text-xs mt-1" style="color: var(--error);">Invalid {currentCoinName} address format</div>
          {/if}
        </div>
        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="tag">Coinbase Tag <Info tip="Custom text embedded in mined blocks, visible on-chain" size={12} /></label>
          <input
            id="tag"
            bind:value={coinbaseTag}
            class="w-full rounded-lg px-3 py-2 text-sm input-themed"
            placeholder="/GoVault/"
          />
          <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">Embedded in blocks you mine</div>
        </div>
      </div>
    </div>

    <!-- Stratum Server -->
    <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
      <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Stratum Server</h3>
      <div class="space-y-4">
        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="sport">Port <Info tip="TCP port miners connect to. Restart server after changing" size={12} /></label>
          <input
            id="sport"
            bind:value={stratumPort}
            type="number"
            class="w-full rounded-lg px-3 py-2 text-sm input-themed"
          />
        </div>
        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="maxc">Max Connections <Info tip="Maximum simultaneous miner connections" size={12} /></label>
          <input
            id="maxc"
            bind:value={maxConn}
            type="number"
            class="w-full rounded-lg px-3 py-2 text-sm input-themed"
          />
        </div>
        <div class="inline-flex items-center gap-1">
          <Toggle bind:checked={autoStart} label="Auto-start on launch" />
          <Info tip="Start stratum server automatically when GoVault launches" size={12} />
        </div>
        {#if stratumURL}
          <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
            <div class="text-xs mb-1" style="color: var(--text-secondary);">Your Stratum URL</div>
            <div class="text-sm data-readout break-all">{stratumURL}</div>
          </div>
        {/if}
      </div>
    </div>

    <!-- Vardiff -->
    <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
      <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Variable Difficulty</h3>
      <div class="space-y-4">
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Min Difficulty <Info tip="Difficulty floor. Set to 0.001 for low-hashrate devices like NerdMiner" size={12} /></label>
            <input bind:value={minDiff} type="number" step="0.001" class="w-full rounded-lg px-3 py-2 text-sm input-themed" />
          </div>
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Max Difficulty <Info tip="Difficulty ceiling. 0 = use network difficulty" size={12} /></label>
            <input bind:value={maxDiff} type="number" class="w-full rounded-lg px-3 py-2 text-sm input-themed" />
            <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">0 = network diff</div>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Target Time (sec) <Info tip="Desired seconds between share submissions per miner" size={12} /></label>
            <input bind:value={targetTimeSec} type="number" class="w-full rounded-lg px-3 py-2 text-sm input-themed" />
          </div>
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Retarget Time (sec) <Info tip="How often to recalculate each miner's difficulty" size={12} /></label>
            <input bind:value={retargetTimeSec} type="number" class="w-full rounded-lg px-3 py-2 text-sm input-themed" />
          </div>
        </div>
        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Variance % <Info tip="Tolerance before triggering a difficulty adjustment" size={12} /></label>
          <input bind:value={variancePct} type="number" class="w-full rounded-lg px-3 py-2 text-sm input-themed" />
        </div>
      </div>
    </div>

    <!-- App Settings -->
    <div class="space-y-6">
      <!-- Theme Selector -->
      <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
        <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Theme</h3>
        <div class="grid grid-cols-3 gap-3">
          {#each themes as t}
            <button
              class="rounded-lg p-3 text-center transition-all duration-200 cursor-pointer"
              style="background-color: var(--bg-secondary); border: 2px solid {$theme === t.id ? t.accent : 'var(--border)'}; {$theme === t.id ? `box-shadow: 0 0 10px ${t.accent}40;` : ''}"
              on:click={() => selectTheme(t.id)}
            >
              <div class="w-5 h-5 rounded-full mx-auto mb-2" style="background-color: {t.accent}; {$theme === t.id ? `box-shadow: 0 0 8px ${t.accent};` : ''}"></div>
              <div class="text-xs font-tech font-bold tracking-wider" style="color: {$theme === t.id ? t.accent : 'var(--text-secondary)'};">{t.label}</div>
              <div class="text-xs mt-0.5" style="color: var(--text-secondary); opacity: 0.7;">{t.desc}</div>
            </button>
          {/each}
        </div>
      </div>

      <!-- Application -->
      <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
        <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Application</h3>
        <div class="space-y-4">
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Log Level <Info tip="Log verbosity. Debug = all events, Error = problems only" size={12} /></label>
            <select bind:value={logLevel} class="w-full rounded-lg px-3 py-2 text-sm select-themed">
              <option value="debug">Debug</option>
              <option value="info">Info</option>
              <option value="warn">Warning</option>
              <option value="error">Error</option>
            </select>
          </div>
          {#if dbPath}
            <div>
              <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Database <Info tip="SQLite storage including WAL and SHM files" size={12} /></label>
              <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                <div class="flex items-center justify-between mb-1">
                  <span class="text-xs" style="color: var(--text-secondary);">Disk Usage</span>
                  <span class="text-sm font-data" style="color: var(--accent);">{formatBytes(dbSize)}</span>
                </div>
                <div class="text-xs break-all" style="color: var(--text-secondary); opacity: 0.7;">{dbPath}</div>
              </div>
            </div>
          {/if}
        </div>
      </div>
    </div>
  </div>
</div>
