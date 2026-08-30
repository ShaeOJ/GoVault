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
  let electricityCost = 0.10;
  let selectedCoin = 'btc';

  let saving = false;
  let saveMsg = '';
  let addressValid: boolean | null = null;
  let addressType = '';
  let stratumURL = '';
  let coinList: Array<{id: string; name: string; symbol: string; defaultRPCPort: number; defaultRPCUser: string; segwit: boolean}> = [];
  let dbPath = '';
  let dbSize = 0;
  let dbShareRows = 0;
  let dbMaxSizeMB = 250;
  let dbBusy = '';        // '' | 'compact' | 'clear'
  let dbMsg = '';
  let confirmClear = false;

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
      const { GetConfig, GetStratumURL, GetCoinList, GetDatabaseInfo } = await import('../../wailsjs/go/appcore/App');
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
        electricityCost = cfg.app?.electricityCost ?? 0.10;
      }
      stratumURL = await GetStratumURL();
      const dbInfo = await GetDatabaseInfo();
      if (dbInfo) {
        dbPath = dbInfo.path || '';
        dbSize = dbInfo.size || 0;
        dbShareRows = dbInfo.shareRows || 0;
        dbMaxSizeMB = dbInfo.maxSizeMB ?? 250;
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
      const { ValidateAddress } = await import('../../wailsjs/go/appcore/App');
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
      const { GetConfig, UpdateConfig } = await import('../../wailsjs/go/appcore/App');
      const cfg = await GetConfig();
      cfg.stratum = { port: stratumPort, maxConn, autoStart };
      cfg.mining = { coin: selectedCoin, payoutAddress, coinbaseTag };
      cfg.vardiff = { minDiff, maxDiff, targetTimeSec, retargetTimeSec, variancePct };
      cfg.app = { ...cfg.app, logLevel, electricityCost, dbMaxSizeMB: Math.max(0, Math.round(dbMaxSizeMB) || 0) };
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

  async function refreshDbInfo() {
    try {
      const { GetDatabaseInfo } = await import('../../wailsjs/go/appcore/App');
      const info = await GetDatabaseInfo();
      if (info) {
        dbSize = info.size || 0;
        dbShareRows = info.shareRows || 0;
      }
    } catch {}
  }

  async function compactDb() {
    dbBusy = 'compact';
    dbMsg = '';
    try {
      const { CompactDatabase } = await import('../../wailsjs/go/appcore/App');
      const r = await CompactDatabase();
      await refreshDbInfo();
      const reclaimed = r?.reclaimed || 0;
      dbMsg = reclaimed > 0 ? `Reclaimed ${formatBytes(reclaimed)}.` : 'Already compact.';
    } catch (e: any) {
      dbMsg = `Error: ${e?.message || e}`;
    }
    dbBusy = '';
    setTimeout(() => dbMsg = '', 4000);
  }

  async function clearStats() {
    dbBusy = 'clear';
    dbMsg = '';
    confirmClear = false;
    try {
      const { ClearStatistics } = await import('../../wailsjs/go/appcore/App');
      const n = await ClearStatistics();
      await refreshDbInfo();
      dbMsg = `Cleared ${n} shares. Blocks & lifetime totals kept.`;
    } catch (e: any) {
      dbMsg = `Error: ${e?.message || e}`;
    }
    dbBusy = '';
    setTimeout(() => dbMsg = '', 5000);
  }

  $: currentCoinName = coinList.find(c => c.id === selectedCoin)?.name || 'Bitcoin';
  $: currentCoinSymbol = coinList.find(c => c.id === selectedCoin)?.symbol || 'BTC';

  // --- Updates ---
  let appVersion = '';
  let checkingUpdate = false;
  let updateInfo: any = null;
  let applyingUpdate = false;
  let updateMsg = '';

  onMount(async () => {
    try { const { GetVersion } = await import('../../wailsjs/go/appcore/App'); appVersion = await GetVersion(); } catch {}
  });

  async function checkUpdate() {
    checkingUpdate = true; updateMsg = ''; updateInfo = null;
    try {
      const { CheckForUpdate } = await import('../../wailsjs/go/appcore/App');
      updateInfo = await CheckForUpdate();
      if (updateInfo?.error) updateMsg = 'Check failed: ' + updateInfo.error;
      else if (!updateInfo?.available) updateMsg = 'You are on the latest version.';
    } catch (e: any) { updateMsg = 'Error: ' + (e?.message || e); }
    checkingUpdate = false;
  }

  async function applyUpdate() {
    if (!updateInfo?.available || !updateInfo?.selfApplies) return;
    applyingUpdate = true; updateMsg = 'Downloading & applying — GoVault will restart when done…';
    try {
      const { ApplyUpdate } = await import('../../wailsjs/go/appcore/App');
      await ApplyUpdate();
    } catch (e: any) { updateMsg = 'Update failed: ' + (e?.message || e); applyingUpdate = false; }
  }

  async function openRelease() {
    try { const { BrowserOpenURL } = await import('../../wailsjs/runtime/runtime'); if (updateInfo?.releaseUrl) BrowserOpenURL(updateInfo.releaseUrl); } catch {}
  }
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
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="coin">Blockchain <Info tip="Select which cryptocurrency to mine" size={12} /></label>
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
          <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">Select which coin to mine</div>
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
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Electricity Cost ($/kWh) <Info tip="Used to estimate daily power costs on the Miners page" size={12} /></label>
            <input
              bind:value={electricityCost}
              type="number"
              step="0.01"
              min="0"
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder="0.10"
            />
            <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">US average ~$0.10/kWh</div>
          </div>
          {#if dbPath}
            <div>
              <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">Database <Info tip="SQLite storage including WAL and SHM files" size={12} /></label>
              <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                <div class="flex items-center justify-between mb-1">
                  <span class="text-xs" style="color: var(--text-secondary);">Disk Usage</span>
                  <span class="text-sm font-data" style="color: var(--accent);">{formatBytes(dbSize)}{dbMaxSizeMB > 0 ? ` / ${dbMaxSizeMB} MB cap` : ''}</span>
                </div>
                <div class="flex items-center justify-between mb-1">
                  <span class="text-xs" style="color: var(--text-secondary);">Share Rows</span>
                  <span class="text-sm font-data" style="color: var(--text-primary);">{dbShareRows.toLocaleString()}</span>
                </div>
                <div class="text-xs break-all" style="color: var(--text-secondary); opacity: 0.7;">{dbPath}</div>
              </div>
            </div>

            <div>
              <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="dbcap">
                Size Cap (MB) <Info tip="When the DB grows past this, the oldest shares are pruned and the file is vacuumed back under the cap. 0 = no cap (30-day age pruning only). Applied on save." size={12} />
              </label>
              <input
                id="dbcap"
                bind:value={dbMaxSizeMB}
                type="number"
                min="0"
                class="w-full rounded-lg px-3 py-2 text-sm input-themed"
                placeholder="250"
              />
              <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">0 = unlimited. Enforced hourly and on save.</div>
            </div>

            <div class="flex gap-2 items-center flex-wrap">
              <button
                class="px-3 py-2 rounded-lg text-xs font-medium font-tech uppercase tracking-wider transition-colors flex items-center gap-2"
                style="background-color: var(--bg-secondary); border: 1px solid var(--border); color: var(--text-primary); {dbBusy ? 'opacity: 0.6;' : ''}"
                on:click={compactDb}
                disabled={!!dbBusy}
              >
                {dbBusy === 'compact' ? 'Compacting…' : 'Compact Now'}
              </button>

              {#if confirmClear}
                <button
                  class="px-3 py-2 rounded-lg text-xs font-medium font-tech uppercase tracking-wider transition-colors"
                  style="background: rgba(255,50,50,0.12); border: 1px solid var(--error); color: var(--error); {dbBusy ? 'opacity: 0.6;' : ''}"
                  on:click={clearStats}
                  disabled={!!dbBusy}
                >
                  {dbBusy === 'clear' ? 'Clearing…' : 'Confirm Clear'}
                </button>
                <button
                  class="px-3 py-2 rounded-lg text-xs font-tech uppercase tracking-wider"
                  style="background-color: var(--bg-secondary); border: 1px solid var(--border); color: var(--text-secondary);"
                  on:click={() => confirmClear = false}
                  disabled={!!dbBusy}
                >Cancel</button>
              {:else}
                <button
                  class="px-3 py-2 rounded-lg text-xs font-medium font-tech uppercase tracking-wider transition-colors"
                  style="background-color: var(--bg-secondary); border: 1px solid rgba(255,50,50,0.4); color: var(--error);"
                  on:click={() => confirmClear = true}
                  disabled={!!dbBusy}
                >Clear Stats</button>
              {/if}

              {#if dbMsg}
                <span class="text-xs font-data" style="color: {dbMsg.startsWith('Error') ? 'var(--error)' : 'var(--success)'};">{dbMsg}</span>
              {/if}
            </div>
            <div class="text-xs" style="color: var(--text-secondary); opacity: 0.7;">
              <span class="font-tech uppercase" style="color: var(--error);">Clear Stats</span> wipes share/hashrate/session history to shrink the DB. Found blocks and lifetime totals are kept.
            </div>
          {/if}
        </div>
      </div>

      <!-- Updates -->
      <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
        <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Updates</h3>
        <div class="space-y-4">
          <div class="flex items-center justify-between gap-3">
            <div>
              <div class="text-xs" style="color: var(--text-secondary); opacity: 0.7;">Current version</div>
              <div class="text-sm font-data" style="color: var(--text-primary);">{appVersion || '—'}</div>
            </div>
            <button
              class="px-3 py-2 rounded-lg text-xs font-medium font-tech uppercase tracking-wider transition-colors"
              style="background-color: var(--bg-secondary); border: 1px solid var(--border); color: var(--text-primary);"
              on:click={checkUpdate}
              disabled={checkingUpdate || applyingUpdate}
            >{checkingUpdate ? 'Checking…' : 'Check for Updates'}</button>
          </div>

          {#if updateInfo?.available}
            <div class="rounded-lg p-3" style="background-color: var(--bg-secondary); border: 1px solid var(--accent);">
              <div class="text-sm font-tech uppercase tracking-wider" style="color: var(--accent);">Update available — {updateInfo.latest}</div>
              {#if updateInfo.notes}
                <div class="text-xs mt-2 font-data" style="color: var(--text-secondary); white-space: pre-wrap; max-height: 8rem; overflow: auto;">{updateInfo.notes}</div>
              {/if}
              <div class="flex gap-2 mt-3">
                {#if updateInfo.selfApplies}
                  <button
                    class="px-3 py-2 rounded-lg text-xs font-medium font-tech uppercase tracking-wider transition-all glow-border-hover"
                    style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid var(--accent); {applyingUpdate ? 'opacity: 0.7;' : ''}"
                    on:click={applyUpdate}
                    disabled={applyingUpdate}
                  >{applyingUpdate ? 'Updating…' : 'Update Now'}</button>
                {:else}
                  <button
                    class="px-3 py-2 rounded-lg text-xs font-medium font-tech uppercase tracking-wider transition-colors"
                    style="background-color: var(--bg-secondary); border: 1px solid var(--border); color: var(--text-primary);"
                    on:click={openRelease}
                  >Open Download Page</button>
                {/if}
              </div>
            </div>
          {/if}

          {#if updateMsg}
            <span class="text-xs font-data" style="color: {(updateMsg.toLowerCase().includes('fail') || updateMsg.startsWith('Error')) ? 'var(--error)' : 'var(--text-secondary)'};">{updateMsg}</span>
          {/if}
        </div>
      </div>
    </div>
  </div>
</div>
