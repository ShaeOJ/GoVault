<script lang="ts">
  import { onMount } from 'svelte';
  import Toggle from '../lib/components/common/Toggle.svelte';
  import Info from '../lib/components/common/Info.svelte';
  import ThemedSpinner from '../lib/components/common/ThemedSpinner.svelte';
  import { theme } from '../lib/stores/theme';
  import type { ThemeName } from '../lib/stores/theme';
  import { formatNumber, formatDifficulty, formatHashrate } from '../lib/utils/format';

  // Mode: 'solo' or 'proxy'. Proxy is the default for new users.
  let miningMode: 'solo' | 'proxy' = 'proxy';

  // Shared coin + payout identity (used by both modes; payout address/coinbase
  // tag only matter in solo mode — proxy mode pays out via the pool worker
  // identity entered below instead).
  let coinId = 'btc';
  let coinList: any[] = [];
  let payoutAddress = '';
  let coinbaseTag = '/TheVault/';
  let addressValid: boolean | null = null;
  let addressType = '';

  const addressPlaceholders: Record<string, string> = {
    btc: 'bc1q...',
    bch: 'bitcoincash:q...',
    dgb: 'D...',
    bc2: 'bc1q...',
    xec: 'ecash:q...',
  };

  $: currentCoinName = coinList.find((c: any) => c.id === coinId)?.name || 'Bitcoin';
  $: currentCoinSymbol = coinList.find((c: any) => c.id === coinId)?.symbol || 'BTC';

  function onCoinChange() {
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
      const result = await ValidateAddress(payoutAddress, coinId);
      addressValid = result?.valid || false;
      addressType = result?.type || '';
    } catch {
      addressValid = false;
    }
  }

  // Step: where the user points their actual mining hardware (this PC).
  let stratumURL = '';
  let copiedURL = false;

  // Solo mode fields
  let host = '127.0.0.1';
  let port = 8332;
  let username = 'bitcoin';
  let password = '';
  let useSSL = false;
  // ZMQ hashblock endpoint — blank = poll-only. Set to enable instant block
  // notifications (requires the node to run -zmqpubhashblock=<endpoint>).
  let zmqBlock = '';
  // How often (seconds) the RPC poll runs as a fallback when ZMQ is enabled.
  // ZMQ is the instant path, so this only needs to be a slow safety net. 0 = 30s default.
  let fallbackPollSec = 30;

  // Proxy mode fields
  let proxyUrl = '';
  let proxyWorker = '';
  let proxyPassword = 'x';
  let proxyPassThrough = false;

  // Hashrate-split fields (shared proxy mode only — mutually exclusive with
  // proxyPassThrough). Pool A = the proxyUrl/proxyWorker/proxyPassword fields
  // above; Pool B is separate. splitPercentA is Pool A's share, Pool B gets
  // the rest — kept in sync so they always sum to 100.
  let splitEnabled = false;
  let splitUrl = '';
  let splitWorker = '';
  let splitPassword = 'x';
  let splitPercentA = 80;
  $: splitPercentB = 100 - splitPercentA;
  function setSplitPercentA(v: number) {
    splitPercentA = Math.max(1, Math.min(99, Math.round(v) || 0));
  }
  function setSplitPercentB(v: number) {
    splitPercentA = 100 - Math.max(1, Math.min(99, Math.round(v) || 0));
  }
  function onSplitPercentAInput(e: Event) {
    setSplitPercentA(Number((e.currentTarget as HTMLInputElement).value));
  }
  function onSplitPercentBInput(e: Event) {
    setSplitPercentB(Number((e.currentTarget as HTMLInputElement).value));
  }

  let testing = false;
  let testResult: any = null;
  let testError = '';
  let saving = false;
  let savingStep = '';
  let nodeStatus: any = null;
  let loaded = false;
  let copied = false;

  let detecting = false;
  let detectResult: any = null;
  let detectError = '';

  // Proxy test state
  let proxyTesting = false;
  let proxyTestResult: any = null;
  let proxyTestError = '';
  let proxySaving = false;
  let upstreamStatus: any = null;

  // Config file name and path per coin
  const configFiles: Record<string, { file: string; path: string }> = {
    btc: { file: 'bitcoin.conf', path: '%APPDATA%\\Bitcoin\\bitcoin.conf' },
    bch: { file: 'bitcoin.conf', path: '%APPDATA%\\Bitcoin Cash\\bitcoin.conf' },
    dgb: { file: 'digibyte.conf', path: '%APPDATA%\\DigiByte\\digibyte.conf' },
    bc2: { file: 'bitcoin.conf', path: '%APPDATA%\\Bitcoin\\bitcoin.conf' },
    xec: { file: 'bitcoin.conf', path: '%APPDATA%\\Bitcoin ABC\\bitcoin.conf' },
  };

  $: configFileName = configFiles[coinId]?.file || 'bitcoin.conf';
  $: configPath = configFiles[coinId]?.path || '%APPDATA%\\Bitcoin\\bitcoin.conf';

  // Default ZMQ hashblock port per coin (matches the node's -zmqpubhashblock).
  const zmqPortDefaults: Record<string, number> = { btc: 28332, bch: 28332, bc2: 28332, xec: 28332, dgb: 28336 };
  function suggestZmq() {
    const p = zmqPortDefaults[coinId] || 28332;
    zmqBlock = `tcp://${host || '127.0.0.1'}:${p}`;
  }
  // Port extracted from the endpoint, for the generated node-side config line.
  $: zmqPort = (zmqBlock.match(/:(\d+)\s*$/) || [])[1] || '';

  $: generatedConfig = [
    '# TheVault - RPC Configuration',
    'server=1',
    `rpcuser=${username || 'yourusername'}`,
    `rpcpassword=${password || 'yourpassword'}`,
    `rpcport=${port}`,
    'rpcallowip=127.0.0.1',
    ...(host !== '127.0.0.1' && host !== 'localhost' ? [`rpcallowip=${host}`] : []),
    '',
    '# Required for mining',
    'txindex=1',
    ...(zmqBlock ? ['', '# Instant block notifications (ZMQ)', `zmqpubhashblock=tcp://0.0.0.0:${zmqPort || '28332'}`] : []),
  ].join('\n');

  async function copyConfig() {
    try {
      await navigator.clipboard.writeText(generatedConfig);
      copied = true;
      setTimeout(() => copied = false, 2000);
    } catch {}
  }

  async function loadStratumURL() {
    try {
      const { GetStratumURL } = await import('../../wailsjs/go/appcore/App');
      stratumURL = await GetStratumURL();
    } catch {}
  }

  async function copyStratumURL() {
    try {
      await navigator.clipboard.writeText(stratumURL);
      copiedURL = true;
      setTimeout(() => copiedURL = false, 2000);
    } catch {}
  }

  // Plain-language guidance for what username each miner should use, per mode.
  $: minerUsername = miningMode === 'proxy'
    ? '<your-pool-address>.<worker>'
    : '<worker-name>';
  $: minerUsernameHint = miningMode === 'proxy'
    ? 'Use the wallet address / username your upstream pool expects, plus a worker label after the dot (e.g. bc1q… .bitaxe1).'
    : 'Any label to name the device (e.g. bitaxe1). Blocks pay to the payout address set above.';

  async function detectNode() {
    detecting = true;
    detectResult = null;
    detectError = '';
    try {
      const { DetectNode } = await import('../../wailsjs/go/appcore/App');
      const result = await DetectNode(coinId);
      if (result?.found) {
        detectResult = result;
        host = result.host;
        port = result.port;
        username = result.username;
        password = result.password;
      } else {
        const tried = result?.tried as string[] || [];
        detectError = 'No local node detected.\n' + tried.join('\n');
      }
    } catch (e: any) {
      detectError = e?.message || String(e);
    }
    detecting = false;
  }

  // --- Settings-page state (server, vardiff, app, theme, updates) ---
  let stratumPort = 10333;
  let maxConn = 100;
  let autoStart = false;
  let minDiff = 0.001;
  let maxDiff = 0;
  let targetTimeSec = 15;
  let retargetTimeSec = 90;
  let variancePct = 30;
  let maxMsgPerSec = 50;
  let maxMsgBurst = 150;
  let logLevel = 'info';
  let electricityCost = 0.10;

  let settingsSaving = false;
  let settingsSaveMsg = '';
  let dbPath = '';
  let dbSize = 0;
  let dbShareRows = 0;
  let dbMaxSizeMB = 250;
  let dbBusy = '';
  let dbMsg = '';
  let confirmClear = false;

  const themes: { id: ThemeName; label: string; accent: string; desc: string }[] = [
    { id: 'vault-tec', label: 'VAULT-TEC', accent: '#f5a623', desc: 'Retro amber' },
    { id: 'nuclear', label: 'NUCLEAR', accent: '#39ff14', desc: 'Radioactive green' },
    { id: 'tron', label: 'TRON', accent: '#00d4ff', desc: 'Neon cyan' },
    { id: 'synthwave', label: 'SYNTHWAVE', accent: '#ff3ea5', desc: 'Retrowave pink' },
    { id: 'mono', label: 'MONO', accent: '#dadada', desc: 'CRT terminal' },
    { id: 'light', label: 'LIGHT', accent: '#a85e00', desc: 'Bright daylight' },
  ];

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

  async function saveSettings() {
    settingsSaving = true;
    settingsSaveMsg = '';
    try {
      const { GetConfig, UpdateConfig } = await import('../../wailsjs/go/appcore/App');
      const cfg = await GetConfig();
      // Spread the existing stratum/vardiff config rather than replacing it
      // wholesale — StratumConfig has several security fields (banDurationMinutes,
      // invalidShareThreshold, maxConnPerIP, authTimeoutSec, etc.) and VardiffConfig
      // has startDiff that aren't bound to inputs here; overwriting the whole
      // object would silently zero them out on every save.
      cfg.stratum = { ...cfg.stratum, port: stratumPort, maxConn, maxMsgPerSec, maxMsgBurst, autoStart };
      cfg.mining = { ...cfg.mining, coin: coinId, payoutAddress, coinbaseTag };
      cfg.vardiff = { ...cfg.vardiff, minDiff, maxDiff, targetTimeSec, retargetTimeSec, variancePct };
      cfg.app = { ...cfg.app, logLevel, electricityCost, dbMaxSizeMB: Math.max(0, Math.round(dbMaxSizeMB) || 0) };
      await UpdateConfig(cfg);
      settingsSaveMsg = 'Settings saved!';
      setTimeout(() => settingsSaveMsg = '', 3000);
    } catch (e: any) {
      settingsSaveMsg = `Error: ${e?.message || e}`;
    }
    settingsSaving = false;
  }

  // --- Updates ---
  let appVersion = '';
  let checkingUpdate = false;
  let updateInfo: any = null;
  let applyingUpdate = false;
  let updateMsg = '';

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
    applyingUpdate = true; updateMsg = 'Downloading & applying — TheVault will restart when done…';
    try {
      const { ApplyUpdate } = await import('../../wailsjs/go/appcore/App');
      await ApplyUpdate();
    } catch (e: any) { updateMsg = 'Update failed: ' + (e?.message || e); applyingUpdate = false; }
  }

  async function openRelease() {
    try { const { BrowserOpenURL } = await import('../../wailsjs/runtime/runtime'); if (updateInfo?.releaseUrl) BrowserOpenURL(updateInfo.releaseUrl); } catch {}
  }

  onMount(async () => {
    try {
      const { GetConfig, GetCoinList, GetStratumURL, GetDatabaseInfo, GetVersion } = await import('../../wailsjs/go/appcore/App');
      const cfg = await GetConfig();
      coinList = await GetCoinList() || [];

      coinId = cfg?.mining?.coin || 'btc';
      payoutAddress = cfg?.mining?.payoutAddress || '';
      coinbaseTag = cfg?.mining?.coinbaseTag || '/TheVault/';

      if (cfg?.node) {
        host = cfg.node.host || host;
        port = cfg.node.port || port;
        username = cfg.node.username || username;
        password = cfg.node.password || '';
        useSSL = cfg.node.useSSL || false;
        zmqBlock = cfg.node.zmqBlock || '';
        fallbackPollSec = cfg.node.fallbackPollSec || 30;
      }
      if (cfg?.proxy) {
        proxyUrl = cfg.proxy.url || '';
        proxyWorker = cfg.proxy.workerName || '';
        proxyPassword = cfg.proxy.password || 'x';
        proxyPassThrough = cfg.proxy.passThrough || false;
        if (cfg.proxy.split) {
          splitEnabled = cfg.proxy.split.enabled || false;
          splitUrl = cfg.proxy.split.poolB?.url || '';
          splitWorker = cfg.proxy.split.poolB?.workerName || '';
          splitPassword = cfg.proxy.split.poolB?.password || 'x';
          splitPercentA = cfg.proxy.split.splitPercentA || 80;
        }
      }
      miningMode = cfg?.miningMode === 'proxy' ? 'proxy' : 'solo';

      stratumPort = cfg?.stratum?.port || 10333;
      maxConn = cfg?.stratum?.maxConn || 100;
      autoStart = cfg?.stratum?.autoStart || false;
      minDiff = cfg?.vardiff?.minDiff || 0.001;
      maxDiff = cfg?.vardiff?.maxDiff || 0;
      targetTimeSec = cfg?.vardiff?.targetTimeSec || 15;
      retargetTimeSec = cfg?.vardiff?.retargetTimeSec || 90;
      variancePct = cfg?.vardiff?.variancePct || 30;
      maxMsgPerSec = cfg?.stratum?.maxMsgPerSec || 50;
      maxMsgBurst = cfg?.stratum?.maxMsgBurst || 150;
      logLevel = cfg?.app?.logLevel || 'info';
      electricityCost = cfg?.app?.electricityCost ?? 0.10;

      stratumURL = await GetStratumURL();

      const dbInfo = await GetDatabaseInfo();
      if (dbInfo) {
        dbPath = dbInfo.path || '';
        dbSize = dbInfo.size || 0;
        dbShareRows = dbInfo.shareRows || 0;
        dbMaxSizeMB = dbInfo.maxSizeMB ?? 250;
      }

      appVersion = await GetVersion();
    } catch {}

    if (payoutAddress) validateAddress();

    if (miningMode === 'solo') {
      refreshStatus();
    } else {
      refreshUpstreamStatus();
    }
    loaded = true;

    // Keep the active-pool indicator live while hashrate splitting is on.
    const splitPoll = setInterval(() => {
      if (miningMode === 'proxy' && splitEnabled) refreshUpstreamStatus();
    }, 8000);
    return () => clearInterval(splitPoll);
  });

  async function refreshStatus() {
    try {
      const { GetNodeStatus } = await import('../../wailsjs/go/appcore/App');
      nodeStatus = await GetNodeStatus();
    } catch {}
  }

  async function refreshUpstreamStatus() {
    try {
      const { GetUpstreamStatus } = await import('../../wailsjs/go/appcore/App');
      upstreamStatus = await GetUpstreamStatus();
    } catch {}
  }

  async function testConnection() {
    testing = true;
    testResult = null;
    testError = '';
    try {
      const { TestNodeConnection } = await import('../../wailsjs/go/appcore/App');
      testResult = await TestNodeConnection(host, port, username, password, useSSL);
    } catch (e: any) {
      testError = e?.message || String(e);
    }
    testing = false;
  }

  async function testProxyConnection() {
    proxyTesting = true;
    proxyTestResult = null;
    proxyTestError = '';
    try {
      const { TestUpstreamConnection } = await import('../../wailsjs/go/appcore/App');
      const result = await TestUpstreamConnection(proxyUrl, proxyWorker, proxyPassword || 'x');
      if (result?.connected) {
        proxyTestResult = result;
      } else {
        proxyTestError = result?.error || 'Connection failed';
      }
    } catch (e: any) {
      proxyTestError = e?.message || String(e);
    }
    proxyTesting = false;
  }

  // Pool B connection test state (hashrate splitting)
  let splitTesting = false;
  let splitTestResult: any = null;
  let splitTestError = '';

  async function testSplitConnection() {
    splitTesting = true;
    splitTestResult = null;
    splitTestError = '';
    try {
      const { TestUpstreamConnection } = await import('../../wailsjs/go/appcore/App');
      const result = await TestUpstreamConnection(splitUrl, splitWorker, splitPassword || 'x');
      if (result?.connected) {
        splitTestResult = result;
      } else {
        splitTestError = result?.error || 'Connection failed';
      }
    } catch (e: any) {
      splitTestError = e?.message || String(e);
    }
    splitTesting = false;
  }

  let switchingPool = false;
  async function switchActivePool(label: 'A' | 'B') {
    switchingPool = true;
    try {
      const { SwitchActivePool } = await import('../../wailsjs/go/appcore/App');
      await SwitchActivePool(label);
      await refreshUpstreamStatus();
    } catch {}
    switchingPool = false;
  }

  async function saveAndConnect() {
    saving = true;
    savingStep = 'saving';
    testResult = null;
    testError = '';
    try {
      const { GetConfig, UpdateConfig, ConnectNode } = await import('../../wailsjs/go/appcore/App');
      const cfg = await GetConfig();
      cfg.node = { ...cfg.node, host, port, username, password, useSSL, zmqBlock: zmqBlock.trim(), fallbackPollSec: Math.max(0, Math.floor(Number(fallbackPollSec) || 0)) };
      cfg.mining = { ...cfg.mining, coin: coinId, payoutAddress, coinbaseTag };
      cfg.miningMode = 'solo';
      await UpdateConfig(cfg);
      miningMode = 'solo';

      savingStep = 'connecting';
      const status = await ConnectNode();
      nodeStatus = status;
    } catch (e: any) {
      testError = e?.message || String(e);
    }
    saving = false;
    savingStep = '';
  }

  let proxySaveSuccess = false;

  async function saveProxy() {
    proxySaving = true;
    proxyTestError = '';
    proxySaveSuccess = false;

    if (splitEnabled) {
      if (proxyPassThrough) {
        proxyTestError = 'Hashrate splitting cannot be combined with per-miner pass-through.';
        proxySaving = false;
        return;
      }
      if (!splitUrl.trim() || !splitWorker.trim()) {
        proxyTestError = 'Hashrate splitting requires a Pool B URL and worker name.';
        proxySaving = false;
        return;
      }
      if (splitPercentA < 1 || splitPercentA > 99) {
        proxyTestError = 'Split percentage must be between 1 and 99.';
        proxySaving = false;
        return;
      }
    }

    try {
      const { GetConfig, UpdateConfig, IsStratumRunning, StopStratum, StartStratum } = await import('../../wailsjs/go/appcore/App');
      const cfg = await GetConfig();
      cfg.proxy = {
        url: proxyUrl,
        workerName: proxyWorker,
        password: proxyPassword || 'x',
        passThrough: proxyPassThrough,
        split: splitEnabled ? {
          enabled: true,
          poolB: { url: splitUrl, workerName: splitWorker, password: splitPassword || 'x' },
          splitPercentA,
        } : undefined,
      };
      cfg.mining = { ...cfg.mining, coin: coinId };
      cfg.miningMode = 'proxy';
      await UpdateConfig(cfg);
      miningMode = 'proxy';

      // If stratum is running, restart it in proxy mode
      const running = await IsStratumRunning();
      if (running) {
        await StopStratum();
        await StartStratum();
      }

      proxySaveSuccess = true;
      setTimeout(() => proxySaveSuccess = false, 3000);
      refreshUpstreamStatus();
    } catch (e: any) {
      proxyTestError = e?.message || String(e);
    }
    proxySaving = false;
  }

  const poolPresets = [
    { name: 'BTC', label: 'Low Diff', url: 'asicpool.space:4334', coin: 'BTC', logo: 'btc.png', color: '#f7931a' },
    { name: 'BTC', url: 'asicpool.space:4333', coin: 'BTC', logo: 'btc.png', color: '#f7931a' },
    { name: 'BCH', url: 'asicpool.space:5333', coin: 'BCH', logo: 'bch.png', color: '#65c062' },
    { name: 'BTCS', url: 'asicpool.space:7333', coin: 'BTCS', logo: 'btcs-top.png', color: '#a8b3bd' },
    { name: 'DGB', url: 'asicpool.space:6333', coin: 'DGB', logo: 'dgb.png', color: '#0066cc' },
    { name: 'BSV', url: 'asicpool.space:5433', coin: 'BSV', logo: 'bsv.png', color: '#eab300' },
  ];
  // asicpool.space hosts its own coin logos — pull them directly rather than
  // bundling/duplicating brand art. Falls back to a colored ticker badge if
  // the asset can't load (offline, DNS, etc).
  const COIN_LOGO_BASE = 'https://asicpool.space/static/coins/';
  let logoFailed: Record<string, boolean> = {};

  function applyPreset(preset: typeof poolPresets[0]) {
    proxyUrl = preset.url;
    proxyTestResult = null;
    proxyTestError = '';
    proxySaveSuccess = false;
  }

  function setMode(mode: 'solo' | 'proxy') {
    miningMode = mode;
    // Clear results when switching
    testResult = null;
    testError = '';
    proxyTestResult = null;
    proxyTestError = '';
  }
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-bold font-tech uppercase tracking-wide" style="color: var(--text-primary);">Mining</h1>
      <p class="text-sm" style="color: var(--text-secondary);">Choose how you mine, connect it, then tune the server below.</p>
    </div>
    <div class="flex items-center gap-3">
      {#if settingsSaveMsg}
        <span class="text-sm font-data" style="color: {settingsSaveMsg.startsWith('Error') ? 'var(--error)' : 'var(--success)'};">{settingsSaveMsg}</span>
      {/if}
      <button
        class="px-4 py-2 rounded-lg text-sm font-medium font-tech uppercase tracking-wider transition-all glow-border-hover {settingsSaving ? 'opacity-50' : ''}"
        style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid var(--accent);"
        on:click={saveSettings}
        disabled={settingsSaving}
      >
        {settingsSaving ? 'Saving...' : 'Save Settings'}
      </button>
    </div>
  </div>

  <!-- ==================== MODE ==================== -->
  <div>
    <h2 class="text-sm font-medium font-tech uppercase tracking-wider mb-3" style="color: var(--text-primary);">Mining Mode</h2>
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
      <!-- Proxy card (default, shown first) -->
      <button
        class="text-left rounded-xl p-4 transition-all"
        style="{miningMode === 'proxy'
          ? 'background: rgba(var(--accent-rgb), 0.12); border: 2px solid var(--accent); box-shadow: 0 0 12px rgba(var(--accent-rgb), 0.2);'
          : 'background-color: var(--bg-card); border: 2px solid var(--border);'}"
        on:click={() => setMode('proxy')}
      >
        <div class="flex items-center justify-between mb-1.5">
          <span class="text-sm font-bold font-tech uppercase tracking-wider" style="color: {miningMode === 'proxy' ? 'var(--accent)' : 'var(--text-primary)'};">Proxy to a pool</span>
          <span class="text-[10px] font-tech uppercase px-1.5 py-0.5 rounded" style="background: rgba(var(--accent-rgb), 0.15); color: var(--accent);">Easiest</span>
        </div>
        <p class="text-xs" style="color: var(--text-secondary); line-height: 1.5;">
          TheVault forwards your miners' work to an existing pool (like CKPool or Public Pool). No full node needed — just a pool URL and your address.
        </p>
      </button>

      <!-- Solo card -->
      <button
        class="text-left rounded-xl p-4 transition-all"
        style="{miningMode === 'solo'
          ? 'background: rgba(var(--accent-rgb), 0.12); border: 2px solid var(--accent); box-shadow: 0 0 12px rgba(var(--accent-rgb), 0.2);'
          : 'background-color: var(--bg-card); border: 2px solid var(--border);'}"
        on:click={() => setMode('solo')}
      >
        <div class="flex items-center justify-between mb-1.5">
          <span class="text-sm font-bold font-tech uppercase tracking-wider" style="color: {miningMode === 'solo' ? 'var(--accent)' : 'var(--text-primary)'};">Solo to your node</span>
          <span class="text-[10px] font-tech uppercase px-1.5 py-0.5 rounded" style="background: var(--bg-secondary); color: var(--text-secondary);">Advanced</span>
        </div>
        <p class="text-xs" style="color: var(--text-secondary); line-height: 1.5;">
          TheVault builds blocks against your own full node — no pool, no fees, you keep the whole reward. Requires a fully-synced node with RPC enabled.
        </p>
      </button>
    </div>
  </div>

  <!-- ==================== COIN + PAYOUT (shared, single source) ==================== -->
  <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
    <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Coin</h3>
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <div>
        <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="coin">Blockchain <Info tip="Select which cryptocurrency to mine" size={12} /></label>
        <select
          id="coin"
          bind:value={coinId}
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
      </div>

      {#if miningMode === 'solo'}
        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="payout">{currentCoinSymbol} Payout Address <Info tip="Wallet address for block rewards. Must match selected blockchain" size={12} /></label>
          <input
            id="payout"
            bind:value={payoutAddress}
            on:blur={validateAddress}
            class="w-full rounded-lg px-3 py-2 text-sm input-themed"
            style={addressValid === true ? 'border-color: var(--success);' : addressValid === false ? 'border-color: var(--error);' : ''}
            placeholder={addressPlaceholders[coinId] || 'bc1q...'}
          />
          {#if addressValid === true}
            <div class="text-xs mt-1" style="color: var(--success);">{addressType}</div>
          {:else if addressValid === false}
            <div class="text-xs mt-1" style="color: var(--error);">Invalid {currentCoinName} address format</div>
          {/if}
        </div>
      {:else}
        <div class="rounded-lg p-3 flex items-center" style="background-color: var(--bg-secondary); border: 1px solid var(--border);">
          <p class="text-xs" style="color: var(--text-secondary); line-height: 1.5;">
            In proxy mode, payouts come from the <strong style="color: var(--text-primary);">upstream pool</strong> using the wallet address entered below in Upstream Pool — not this card.
          </p>
        </div>
      {/if}
    </div>

    {#if miningMode === 'solo'}
      <div class="mt-4">
        <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="tag">Coinbase Tag <Info tip="Custom text embedded in mined blocks, visible on-chain" size={12} /></label>
        <input
          id="tag"
          bind:value={coinbaseTag}
          class="w-full sm:w-80 rounded-lg px-3 py-2 text-sm input-themed"
          placeholder="/TheVault/"
        />
        <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">Embedded in blocks you mine</div>
      </div>
    {/if}
  </div>

  <!-- ==================== CONNECTION ==================== -->
  <div class="flex items-center gap-2">
    <h2 class="text-sm font-medium font-tech uppercase tracking-wider" style="color: var(--text-primary);">
      {miningMode === 'proxy' ? 'Connect to your pool' : 'Connect to your node'}
    </h2>
  </div>

  {#if miningMode === 'solo'}
    <!-- ==================== SOLO MODE ==================== -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Connection Settings -->
      <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
        <div class="flex items-center justify-between mb-4">
          <h3 class="text-sm font-medium font-tech uppercase tracking-wider" style="color: var(--text-secondary);">Node Connection</h3>
          <button
            class="px-3 py-1.5 text-xs rounded-lg font-tech uppercase tracking-wider transition-all glow-border-hover flex items-center gap-1.5"
            style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid rgba(var(--accent-rgb), 0.3); {detecting ? 'opacity: 0.7;' : ''}"
            on:click={detectNode}
            disabled={detecting || saving}
          >
            {#if detecting}
              <ThemedSpinner size={12} />
              Scanning...
            {:else}
              Detect Node
            {/if}
          </button>
        </div>

        {#if detectResult}
          <div class="rounded-lg p-3 mb-4" style="background: rgba(var(--accent-rgb), 0.05); border: 1px solid rgba(var(--accent-rgb), 0.2);">
            <div class="text-sm font-medium mb-1" style="color: var(--success);">Node Detected</div>
            <div class="text-xs font-data space-y-0.5" style="color: var(--text-secondary);">
              <div>{detectResult.nodeVersion} on {detectResult.host}:{detectResult.port}</div>
              <div>Auth: {detectResult.authMethod} &middot; Chain: {detectResult.chain} &middot; Height: {formatNumber(detectResult.blockHeight)}</div>
              {#if detectResult.syncPercent < 99.99}
                <div>Sync: {detectResult.syncPercent?.toFixed(2)}%</div>
              {/if}
            </div>
            <div class="text-xs mt-1.5" style="color: var(--text-secondary); opacity: 0.7;">Fields auto-filled. Click Save & Connect to apply.</div>
          </div>
        {/if}

        {#if detectError}
          <div class="rounded-lg p-3 mb-4" style="background: rgba(255,50,50,0.05); border: 1px solid rgba(255,50,50,0.2);">
            <div class="text-sm font-medium mb-1" style="color: var(--error);">Detection Failed</div>
            <div class="text-xs font-data" style="color: var(--text-secondary); white-space: pre-line;">{detectError}</div>
          </div>
        {/if}

        <div class="space-y-4">
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="host">RPC Host <Info tip="{currentCoinName} node IP or hostname (usually 127.0.0.1 for local)" size={12} /></label>
            <input
              id="host"
              bind:value={host}
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder="127.0.0.1"
            />
          </div>

          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="port">RPC Port <Info tip="Node RPC port (default: 8332 mainnet, 18332 testnet)" size={12} /></label>
            <input
              id="port"
              bind:value={port}
              type="number"
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder="8332"
            />
          </div>

          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="user">RPC Username <Info tip="From {configFileName} rpcuser setting" size={12} /></label>
            <input
              id="user"
              bind:value={username}
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder="bitcoin"
            />
          </div>

          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="pass">RPC Password <Info tip="From {configFileName} rpcpassword setting" size={12} /></label>
            <input
              id="pass"
              bind:value={password}
              type="password"
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder="Your RPC password"
            />
          </div>

          <div class="inline-flex items-center gap-1">
            <Toggle bind:checked={useSSL} label="Use SSL/TLS" />
            <Info tip="Encrypted RPC connection. Rarely needed locally" size={12} />
          </div>

          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="zmq">ZMQ Block Endpoint <span style="color: var(--text-secondary); opacity: 0.7;">(optional)</span> <Info tip="Instant new-block notifications. Blank = poll-only. Requires the node to run zmqpubhashblock at this endpoint (added to the generated config below). e.g. tcp://127.0.0.1:28332" size={12} /></label>
            <div class="flex gap-2">
              <input
                id="zmq"
                bind:value={zmqBlock}
                class="flex-1 rounded-lg px-3 py-2 text-sm input-themed"
                placeholder="tcp://127.0.0.1:{zmqPortDefaults[coinId] || 28332} — blank for poll-only"
              />
              <button
                type="button"
                class="px-3 py-2 rounded-lg text-xs font-tech uppercase tracking-wider transition-colors"
                style="background-color: var(--bg-secondary); border: 1px solid var(--border); color: var(--text-primary);"
                on:click={suggestZmq}
                title="Fill from host + default ZMQ port for this coin"
              >
                Auto
              </button>
            </div>
          </div>

          {#if zmqBlock.trim()}
            <div>
              <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="fallbackPoll">Fallback Poll Interval <span style="color: var(--text-secondary); opacity: 0.7;">(seconds)</span> <Info tip="With ZMQ enabled, blocks are detected instantly — the RPC poll is only a safety net in case a ZMQ message is ever missed. A slower interval (e.g. 60s) is fine and lighter on your node. Default 30s." size={12} /></label>
              <input
                id="fallbackPoll"
                type="number"
                min="5"
                step="5"
                bind:value={fallbackPollSec}
                class="w-40 rounded-lg px-3 py-2 text-sm input-themed"
                placeholder="30"
              />
            </div>
          {/if}

          <div class="flex gap-3 pt-2">
            <button
              class="flex-1 px-4 py-2 rounded-lg text-sm font-medium font-tech uppercase tracking-wider transition-colors flex items-center justify-center gap-2"
              style="background-color: var(--bg-secondary); border: 1px solid var(--border); color: var(--text-primary); {testing ? 'opacity: 0.7;' : ''}"
              on:click={testConnection}
              disabled={testing || saving}
            >
              {#if testing}
                <ThemedSpinner size={16} />
                Testing...
              {:else}
                Test Connection
              {/if}
            </button>
            <button
              class="flex-1 px-4 py-2 rounded-lg text-sm font-medium font-tech uppercase tracking-wider transition-all glow-border-hover flex items-center justify-center gap-2"
              style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid var(--accent); {saving ? 'opacity: 0.7;' : ''}"
              on:click={saveAndConnect}
              disabled={saving || testing}
            >
              {#if saving}
                <ThemedSpinner size={16} />
                {savingStep === 'connecting' ? 'Connecting...' : 'Saving...'}
              {:else}
                Save & Connect
              {/if}
            </button>
          </div>

          {#if testResult}
            <div class="rounded-lg p-3" style="background: rgba(var(--accent-rgb), 0.05); border: 1px solid rgba(var(--accent-rgb), 0.2);">
              <div class="text-sm font-medium mb-1" style="color: var(--success);">Connection Successful</div>
              <div class="text-xs font-data space-y-0.5" style="color: var(--text-secondary);">
                <div>Chain: {testResult.chain}</div>
                <div>Block Height: {formatNumber(testResult.blocks)}</div>
                <div>Sync: {testResult.syncPercent?.toFixed(2)}%</div>
              </div>
            </div>
          {/if}

          {#if testError}
            <div class="rounded-lg p-3" style="background: rgba(255,50,50,0.05); border: 1px solid rgba(255,50,50,0.2);">
              <div class="text-sm font-medium mb-1" style="color: var(--error);">Connection Failed</div>
              <div class="text-xs font-data" style="color: var(--text-secondary);">{testError}</div>
            </div>
          {/if}
        </div>
      </div>

      <!-- Node Status + Config Generator -->
      <div class="space-y-4">
        <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
          <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Node Status</h3>

          {#if saving && savingStep === 'connecting'}
            <div class="text-center py-8">
              <div class="mb-3" style="color: var(--accent);">
                <ThemedSpinner size={32} mode="block" />
              </div>
              <div class="text-sm font-medium" style="color: var(--accent);">Connecting to node...</div>
              <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">{host}:{port}</div>
            </div>
          {:else if nodeStatus?.connected}
            <div class="space-y-3">
              <div class="flex items-center gap-2 mb-4">
                <div
                  class="w-3 h-3 rounded-full status-pulse"
                  style="background-color: var(--success); box-shadow: 0 0 6px var(--success);"
                ></div>
                <span class="text-sm font-medium glow-text" style="color: var(--success);">Connected</span>
                {#if nodeStatus.nodeVersion}
                  <span class="text-xs font-data" style="color: var(--text-secondary);">{nodeStatus.nodeVersion}</span>
                {/if}
              </div>

              <div class="grid grid-cols-2 gap-3">
                <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                  <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Block Height <Info tip="Current blockchain height synced by your node" size={11} /></div>
                  <div class="text-sm font-medium data-readout">{formatNumber(nodeStatus.blocks || nodeStatus.blockHeight || 0)}</div>
                </div>
                <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                  <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Chain <Info tip="Network: main (mainnet), test (testnet), or regtest" size={11} /></div>
                  <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{nodeStatus.chain || 'main'}</div>
                </div>
                <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                  <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Network Difficulty <Info tip="Global {currentCoinName} mining difficulty target" size={11} /></div>
                  <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatDifficulty(nodeStatus.networkDifficulty || 0)}</div>
                </div>
                <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                  <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Network Hashrate <Info tip="Estimated total {currentCoinName} network hash rate" size={11} /></div>
                  <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatHashrate(nodeStatus.networkHashrate || 0)}</div>
                </div>
                {#if nodeStatus.connections !== undefined}
                  <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                    <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Peers <Info tip="{currentCoinName} nodes connected for block propagation" size={11} /></div>
                    <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{nodeStatus.connections}</div>
                  </div>
                {/if}
                {#if nodeStatus.syncPercent !== undefined}
                  <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                    <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Sync <Info tip="Sync progress. Must reach 100% before mining works" size={11} /></div>
                    <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{nodeStatus.syncPercent?.toFixed(2)}%</div>
                  </div>
                {/if}
              </div>

              {#if nodeStatus.syncing}
                <div class="rounded-lg p-3" style="background: rgba(var(--accent-rgb), 0.05); border: 1px solid rgba(var(--accent-rgb), 0.2);">
                  <div class="text-sm font-medium font-tech" style="color: var(--warning);">Syncing...</div>
                  <div class="w-full rounded-full h-2 mt-2" style="background-color: var(--bg-secondary);">
                    <div class="h-2 rounded-full transition-all" style="width: {nodeStatus.syncPercent || 0}%; background-color: var(--warning);"></div>
                  </div>
                </div>
              {/if}
            </div>
          {:else}
            <div class="text-center py-8">
              <div class="w-3 h-3 rounded-full mx-auto mb-3" style="background-color: var(--error);"></div>
              <div class="text-sm" style="color: var(--text-secondary);">Not Connected</div>
              <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">Configure and test your node connection</div>
            </div>
          {/if}
        </div>

        <!-- Config Generator -->
        <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
          <div class="flex items-center justify-between mb-3">
            <h3 class="text-sm font-medium font-tech uppercase tracking-wider inline-flex items-center gap-1" style="color: var(--text-secondary);">{currentCoinName}.conf <Info tip="Sample {configFileName} to enable RPC. Copy to your node's data directory" size={12} /></h3>
            <button
              class="px-3 py-1 text-xs rounded-lg font-tech uppercase tracking-wider transition-all glow-border-hover"
              style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid rgba(var(--accent-rgb), 0.3);"
              on:click={copyConfig}
            >
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>
          <p class="text-xs mb-3" style="color: var(--text-secondary); opacity: 0.7;">
            Paste this into your <code class="font-data px-1 rounded" style="color: var(--accent); background-color: var(--bg-secondary);">{configFileName}</code> file and restart your node.
          </p>
          <pre
            class="rounded-lg p-4 text-xs font-data leading-relaxed overflow-x-auto whitespace-pre"
            style="background-color: var(--bg-primary); color: var(--accent); border: 1px solid var(--border);"
          >{generatedConfig}</pre>
          <p class="text-xs mt-3" style="color: var(--text-secondary); opacity: 0.5;">
            Config path: <code class="font-data px-1 rounded" style="background-color: var(--bg-secondary);">{configPath}</code>
          </p>
        </div>
      </div>
    </div>
  {:else}
    <!-- ==================== PROXY MODE ==================== -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Proxy Settings -->
      <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
        <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Upstream Pool</h3>

        <div class="space-y-4">
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="proxy-url">
              Pool URL <Info tip="Stratum URL of the upstream pool (e.g. solo.ckpool.org:3333)" size={12} />
            </label>
            <input
              id="proxy-url"
              bind:value={proxyUrl}
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder="stratum+tcp://solo.ckpool.org:3333"
            />
          </div>

          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="proxy-worker">
              {proxyPassThrough ? 'Pool Wallet / Prefix' : 'Worker Name'}
              <Info tip={proxyPassThrough
                ? 'Optional. If set, each miner authorizes upstream as <this>.<miner-worker>, so the pool credits this wallet while tracking each device. Leave blank to pass miner usernames through unchanged.'
                : 'Your Bitcoin address or pool username. All miners share this identity upstream.'} size={12} />
            </label>
            <input
              id="proxy-worker"
              bind:value={proxyWorker}
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder={proxyPassThrough ? 'your_wallet (optional)' : 'your_bitcoin_address'}
            />
          </div>

          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="proxy-pass">
              Password <Info tip="Pool password (usually 'x' or empty)" size={12} />
            </label>
            <input
              id="proxy-pass"
              bind:value={proxyPassword}
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder="x"
            />
          </div>

          <div class="rounded-lg p-3" style="background-color: var(--bg-secondary); border: 1px solid var(--border);">
            <div class="inline-flex items-center gap-1">
              <Toggle bind:checked={proxyPassThrough} label="Per-miner pass-through" />
              <Info tip="OFF (shared): all your miners appear on the pool as one worker. ON (pass-through): each miner gets its own upstream connection and appears on the pool as its own worker — correct per-device stats and vardiff." size={12} />
            </div>
            <p class="text-xs mt-2" style="color: var(--text-secondary); opacity: 0.75; line-height: 1.5;">
              {proxyPassThrough
                ? 'Each miner connects to the pool independently under its own worker name — best for correct per-device attribution.'
                : 'All miners are aggregated under one pool identity (the worker name above).'}
            </p>
          </div>

          <!-- Hashrate splitting (shared mode only) -->
          <div class="rounded-lg p-3" style="background-color: var(--bg-secondary); border: 1px solid var(--border); {proxyPassThrough ? 'opacity: 0.5;' : ''}">
            <div class="inline-flex items-center gap-1">
              <Toggle bind:checked={splitEnabled} label="Split hashrate between two pools" disabled={proxyPassThrough} />
              <Info tip="Automatically splits your hashrate between the pool above (Pool A) and a second pool (Pool B) by percentage, switching which pool's job is active job-by-job so it converges on the target split over time. Pool switches never reconnect your miners. Not available with per-miner pass-through." size={12} />
            </div>

            {#if proxyPassThrough}
              <p class="text-xs mt-2" style="color: var(--text-secondary); opacity: 0.7;">Turn off per-miner pass-through to enable hashrate splitting.</p>
            {:else if splitEnabled}
              <div class="space-y-3 mt-3">
                <div>
                  <label class="block text-xs mb-1.5" style="color: var(--text-secondary);" for="split-url">Pool B URL</label>
                  <input
                    id="split-url"
                    bind:value={splitUrl}
                    class="w-full rounded-lg px-3 py-2 text-sm input-themed"
                    placeholder="stratum+tcp://second-pool.example:3333"
                  />
                </div>
                <div>
                  <label class="block text-xs mb-1.5" style="color: var(--text-secondary);" for="split-worker">Pool B Worker Name</label>
                  <input
                    id="split-worker"
                    bind:value={splitWorker}
                    class="w-full rounded-lg px-3 py-2 text-sm input-themed"
                    placeholder="your_address_on_pool_b"
                  />
                </div>
                <div>
                  <label class="block text-xs mb-1.5" style="color: var(--text-secondary);" for="split-pass">Pool B Password</label>
                  <input
                    id="split-pass"
                    bind:value={splitPassword}
                    class="w-full rounded-lg px-3 py-2 text-sm input-themed"
                    placeholder="x"
                  />
                </div>

                <div>
                  <div class="text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">
                    Split Ratio <Info tip="Must sum to 100. Editing one field updates the other automatically." size={12} />
                  </div>
                  <div class="flex items-center gap-2">
                    <div class="flex-1">
                      <label class="block text-[10px] mb-1 opacity-70" style="color: var(--text-secondary);" for="split-pct-a">Pool A %</label>
                      <input
                        id="split-pct-a"
                        type="number"
                        min="1"
                        max="99"
                        value={splitPercentA}
                        on:input={onSplitPercentAInput}
                        class="w-full rounded-lg px-3 py-2 text-sm input-themed"
                      />
                    </div>
                    <span class="text-sm pt-4" style="color: var(--text-secondary);">/</span>
                    <div class="flex-1">
                      <label class="block text-[10px] mb-1 opacity-70" style="color: var(--text-secondary);" for="split-pct-b">Pool B %</label>
                      <input
                        id="split-pct-b"
                        type="number"
                        min="1"
                        max="99"
                        value={splitPercentB}
                        on:input={onSplitPercentBInput}
                        class="w-full rounded-lg px-3 py-2 text-sm input-themed"
                      />
                    </div>
                  </div>
                </div>

                <button
                  class="w-full px-4 py-2 rounded-lg text-sm font-medium font-tech uppercase tracking-wider transition-colors flex items-center justify-center gap-2"
                  style="background-color: var(--bg-primary); border: 1px solid var(--border); color: var(--text-primary); {splitTesting ? 'opacity: 0.7;' : ''}"
                  on:click={testSplitConnection}
                  disabled={splitTesting}
                >
                  {#if splitTesting}
                    <ThemedSpinner size={16} />
                    Testing Pool B...
                  {:else}
                    Test Pool B Connection
                  {/if}
                </button>

                {#if splitTestResult}
                  <div class="rounded-lg p-3" style="background: rgba(var(--accent-rgb), 0.05); border: 1px solid rgba(var(--accent-rgb), 0.2);">
                    <div class="text-sm font-medium mb-1" style="color: var(--success);">Pool B Connection Successful</div>
                    <div class="text-xs font-data" style="color: var(--text-secondary);">EN2 Size: {splitTestResult.extranonce2Size} bytes</div>
                  </div>
                {/if}
                {#if splitTestError}
                  <div class="rounded-lg p-3" style="background: rgba(255,50,50,0.05); border: 1px solid rgba(255,50,50,0.2);">
                    <div class="text-sm font-medium mb-1" style="color: var(--error);">Pool B Connection Failed</div>
                    <div class="text-xs font-data" style="color: var(--text-secondary);">{splitTestError}</div>
                  </div>
                {/if}
              </div>
            {/if}
          </div>

          <div class="flex gap-3 pt-2">
            <button
              class="flex-1 px-4 py-2 rounded-lg text-sm font-medium font-tech uppercase tracking-wider transition-colors flex items-center justify-center gap-2"
              style="background-color: var(--bg-secondary); border: 1px solid var(--border); color: var(--text-primary); {proxyTesting ? 'opacity: 0.7;' : ''}"
              on:click={testProxyConnection}
              disabled={proxyTesting || proxySaving}
            >
              {#if proxyTesting}
                <ThemedSpinner size={16} />
                Testing...
              {:else}
                Test Connection
              {/if}
            </button>
            <button
              class="flex-1 px-4 py-2 rounded-lg text-sm font-medium font-tech uppercase tracking-wider transition-all glow-border-hover flex items-center justify-center gap-2"
              style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid var(--accent); {proxySaving ? 'opacity: 0.7;' : ''}"
              on:click={saveProxy}
              disabled={proxySaving || proxyTesting}
            >
              {#if proxySaving}
                <ThemedSpinner size={16} />
                Saving...
              {:else}
                Save & Connect
              {/if}
            </button>
          </div>

          {#if proxySaveSuccess}
            <div class="rounded-lg p-3" style="background: rgba(var(--accent-rgb), 0.05); border: 1px solid rgba(var(--accent-rgb), 0.2);">
              <div class="text-sm font-medium" style="color: var(--success);">Settings saved. Proxy mode active.</div>
            </div>
          {/if}

          {#if proxyTestResult}
            <div class="rounded-lg p-3" style="background: rgba(var(--accent-rgb), 0.05); border: 1px solid rgba(var(--accent-rgb), 0.2);">
              <div class="text-sm font-medium mb-1" style="color: var(--success);">Connection Successful</div>
              <div class="text-xs font-data space-y-0.5" style="color: var(--text-secondary);">
                <div>Extranonce1: <span class="font-mono" style="color: var(--accent);">{proxyTestResult.extranonce1}</span></div>
                <div>EN2 Size: {proxyTestResult.extranonce2Size} bytes (local: {proxyTestResult.localEN2Size})</div>
                {#if proxyTestResult.upstreamDiff}
                  <div>Upstream Difficulty: {proxyTestResult.upstreamDiff}</div>
                {/if}
                {#if proxyTestResult.networkDiff}
                  <div>Network Difficulty: {formatDifficulty(proxyTestResult.networkDiff)}</div>
                {/if}
              </div>
            </div>
          {/if}

          {#if proxyTestError}
            <div class="rounded-lg p-3" style="background: rgba(255,50,50,0.05); border: 1px solid rgba(255,50,50,0.2);">
              <div class="text-sm font-medium mb-1" style="color: var(--error);">Connection Failed</div>
              <div class="text-xs font-data" style="color: var(--text-secondary);">{proxyTestError}</div>
            </div>
          {/if}
        </div>
      </div>

      <!-- Proxy Status + Info -->
      <div class="space-y-4">
        <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
          <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Upstream Status</h3>

          {#if upstreamStatus?.connected}
            <div class="space-y-3">
              <div class="flex items-center gap-2 mb-4">
                <div
                  class="w-3 h-3 rounded-full status-pulse"
                  style="background-color: var(--success); box-shadow: 0 0 6px var(--success);"
                ></div>
                <span class="text-sm font-medium glow-text" style="color: var(--success);">Connected</span>
              </div>

              {#if upstreamStatus.split}
                <div class="space-y-3">
                  <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                    <div class="text-xs" style="color: var(--text-secondary);">Active Pool</div>
                    <div class="text-sm font-medium font-data" style="color: var(--accent);">
                      Pool {upstreamStatus.activePool} ({upstreamStatus.activePool === 'A' ? upstreamStatus.splitPercentA : 100 - upstreamStatus.splitPercentA}% target)
                    </div>
                  </div>

                  {#each ['A', 'B'] as label (label)}
                    {@const pool = label === 'A' ? upstreamStatus.poolA : upstreamStatus.poolB}
                    <div class="rounded-lg p-3 flex items-center justify-between" style="background-color: var(--bg-secondary); {!pool.healthy ? 'border: 1px solid var(--error);' : ''}">
                      <div>
                        <div class="text-xs inline-flex items-center gap-1.5" style="color: var(--text-secondary);">
                          Pool {label}
                          {#if upstreamStatus.activePool === label}
                            <span class="text-[10px] font-tech uppercase px-1.5 py-0.5 rounded" style="background: rgba(var(--accent-rgb), 0.15); color: var(--accent);">Active</span>
                          {/if}
                          {#if !pool.healthy}
                            <span class="text-[10px] font-tech uppercase px-1.5 py-0.5 rounded" style="background: rgba(255,50,50,0.15); color: var(--error);">Down</span>
                          {/if}
                        </div>
                        <div class="text-sm font-medium font-data" style="color: {pool.connected ? 'var(--text-primary)' : 'var(--error)'};">
                          {pool.connected ? `Diff ${pool.upstreamDiff || '—'}` : 'Disconnected'}
                        </div>
                      </div>
                      {#if upstreamStatus.activePool !== label && pool.healthy}
                        <button
                          class="px-2.5 py-1 text-[11px] rounded-lg font-tech uppercase tracking-wider transition-colors"
                          style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid rgba(var(--accent-rgb), 0.3); {switchingPool ? 'opacity: 0.6;' : ''}"
                          on:click={() => switchActivePool(label)}
                          disabled={switchingPool}
                        >
                          Switch
                        </button>
                      {/if}
                    </div>
                  {/each}

                  {#if !upstreamStatus.poolA.healthy || !upstreamStatus.poolB.healthy}
                    <div class="rounded-lg p-3 text-xs" style="background: rgba(255,50,50,0.05); border: 1px solid rgba(255,50,50,0.2); color: var(--text-secondary);">
                      Failover active — 100% of hashrate is temporarily routed to the surviving pool until the other reconnects.
                    </div>
                  {/if}
                </div>
              {:else if upstreamStatus.passThrough}
                <div class="grid grid-cols-2 gap-3">
                  <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                    <div class="text-xs" style="color: var(--text-secondary);">Mode</div>
                    <div class="text-sm font-medium font-data" style="color: var(--accent);">Per-miner</div>
                  </div>
                  <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                    <div class="text-xs" style="color: var(--text-secondary);">Miners → Pool</div>
                    <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{upstreamStatus.minerCount ?? 0}</div>
                  </div>
                </div>
                <div class="text-xs" style="color: var(--text-secondary); opacity: 0.7;">Each miner holds its own upstream connection to the pool.</div>
              {:else}
                <div class="grid grid-cols-2 gap-3">
                  <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                    <div class="text-xs" style="color: var(--text-secondary);">Extranonce1</div>
                    <div class="text-sm font-medium font-data" style="color: var(--accent);">{upstreamStatus.extranonce1 || '—'}</div>
                  </div>
                  <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                    <div class="text-xs" style="color: var(--text-secondary);">Upstream Difficulty</div>
                    <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{upstreamStatus.upstreamDiff || '—'}</div>
                  </div>
                </div>
              {/if}
            </div>
          {:else}
            <div class="text-center py-8">
              <div class="w-3 h-3 rounded-full mx-auto mb-3" style="background-color: var(--text-secondary); opacity: 0.5;"></div>
              <div class="text-sm" style="color: var(--text-secondary);">Not Connected</div>
              <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">Save settings and start the stratum server to connect</div>
            </div>
          {/if}
        </div>

        <!-- Pool Presets -->
        <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
          <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-3" style="color: var(--text-secondary);">ASICpool</h3>
          <div class="grid grid-cols-2 gap-1.5">
            {#each poolPresets as preset}
              <button
                class="flex items-center gap-2 rounded-lg px-2.5 py-2 transition-all"
                style="background-color: var(--bg-secondary); border: 1px solid {proxyUrl === preset.url ? 'var(--accent)' : 'transparent'}; {proxyUrl === preset.url ? 'box-shadow: 0 0 8px rgba(var(--accent-rgb), 0.15);' : ''}"
                on:click={() => applyPreset(preset)}
                title={preset.url}
              >
                {#if !logoFailed[preset.logo]}
                  <img
                    src="{COIN_LOGO_BASE}{preset.logo}?v=3"
                    alt={preset.coin}
                    class="flex-shrink-0 rounded-full"
                    style="width: 22px; height: 22px; object-fit: contain; background: rgba(255,255,255,0.06);"
                    on:error={() => (logoFailed[preset.logo] = true)}
                  />
                {:else}
                  <span
                    class="flex-shrink-0 flex items-center justify-center rounded-full font-tech font-bold"
                    style="width: 22px; height: 22px; font-size: 8px; background: {preset.color}; color: #0a0a0a;"
                  >{preset.coin.slice(0, 3)}</span>
                {/if}
                <span class="text-xs font-medium font-tech truncate" style="color: {proxyUrl === preset.url ? 'var(--accent)' : 'var(--text-primary)'};">
                  {preset.name}{#if preset.label} <span class="opacity-60">{preset.label}</span>{/if}
                </span>
                {#if proxyUrl === preset.url}
                  <svg class="flex-shrink-0 ml-auto" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6L9 17l-5-5"/></svg>
                {/if}
              </button>
            {/each}
          </div>
        </div>

        <!-- How it works -->
        <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
          <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-3" style="color: var(--text-secondary);">How Proxy Mode Works</h3>
          <div class="text-xs space-y-2" style="color: var(--text-secondary); line-height: 1.6;">
            <p>TheVault connects as a single miner to the upstream pool and relays work to all your local miners.</p>
            <p>Each miner gets its own difficulty via vardiff. Shares that meet the upstream difficulty are forwarded to the pool.</p>
            <p>Hashrate tracking, miner monitoring, and all dashboard stats work the same as solo mode.</p>
          </div>
        </div>
      </div>
    </div>
  {/if}

  <!-- ==================== POINT YOUR MINERS HERE ==================== -->
  <div class="flex items-center gap-2">
    <h2 class="text-sm font-medium font-tech uppercase tracking-wider" style="color: var(--text-primary);">Point your miners here</h2>
  </div>

  <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
    <p class="text-xs mb-4" style="color: var(--text-secondary); line-height: 1.6;">
      Enter these details into each mining device (Bitaxe, NerdAxe, etc.) on your network. Every miner connects to TheVault at this address — TheVault handles the {miningMode === 'proxy' ? 'pool' : 'node'} for them.
    </p>

    <div class="space-y-3">
      <!-- Stratum URL -->
      <div>
        <div class="text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">
          Stratum URL <Info tip="This computer's LAN address and TheVault's stratum port. Point every miner here." size={12} />
        </div>
        <div class="flex gap-2">
          <code
            class="flex-1 rounded-lg px-3 py-2 text-sm font-data overflow-x-auto whitespace-nowrap"
            style="background-color: var(--bg-primary); color: var(--accent); border: 1px solid var(--border);"
          >{stratumURL || 'stratum+tcp://…'}</code>
          <button
            class="px-3 py-2 text-xs rounded-lg font-tech uppercase tracking-wider transition-all glow-border-hover flex-shrink-0"
            style="background: rgba(var(--accent-rgb), 0.1); color: var(--accent); border: 1px solid rgba(var(--accent-rgb), 0.3);"
            on:click={copyStratumURL}
          >
            {copiedURL ? 'Copied!' : 'Copy'}
          </button>
        </div>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <!-- Username -->
        <div>
          <div class="text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">
            Username / Worker <Info tip={minerUsernameHint} size={12} />
          </div>
          <code
            class="block rounded-lg px-3 py-2 text-sm font-data overflow-x-auto whitespace-nowrap"
            style="background-color: var(--bg-primary); color: var(--text-primary); border: 1px solid var(--border);"
          >{minerUsername}</code>
          <p class="text-xs mt-1.5" style="color: var(--text-secondary); opacity: 0.7; line-height: 1.5;">{minerUsernameHint}</p>
        </div>

        <!-- Password -->
        <div>
          <div class="text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);">
            Password <Info tip="Most miners require a value here. TheVault ignores it — 'x' is fine." size={12} />
          </div>
          <code
            class="block rounded-lg px-3 py-2 text-sm font-data"
            style="background-color: var(--bg-primary); color: var(--text-primary); border: 1px solid var(--border);"
          >x</code>
          <p class="text-xs mt-1.5" style="color: var(--text-secondary); opacity: 0.7; line-height: 1.5;">Anything works — TheVault does not check it.</p>
        </div>
      </div>
    </div>
  </div>

  <!-- ==================== ADVANCED / SERVER SETTINGS ==================== -->
  <div class="flex items-center gap-2">
    <h2 class="text-sm font-medium font-tech uppercase tracking-wider" style="color: var(--text-primary);">Server & App Settings</h2>
  </div>

  <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
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
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="mmps">Max Shares/sec per Miner <Info tip="Per-connection submit rate cap (anti-spam). Raise this if a high-hashrate device gets disconnected with 'message rate limit exceeded' — a single 200+ TH/s ASIC can submit 50+ shares/sec at typical difficulty" size={12} /></label>
            <input
              id="mmps"
              bind:value={maxMsgPerSec}
              type="number"
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
            />
            <div class="text-xs mt-1" style="color: var(--text-secondary); opacity: 0.7;">0 = disabled</div>
          </div>
          <div>
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="mmb">Burst Allowance <Info tip="How many shares can arrive instantly (e.g. right as a powerful miner connects) before the per-second cap above applies" size={12} /></label>
            <input
              id="mmb"
              bind:value={maxMsgBurst}
              type="number"
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
            />
          </div>
        </div>
        <div class="inline-flex items-center gap-1">
          <Toggle bind:checked={autoStart} label="Auto-start on launch" />
          <Info tip="Start stratum server automatically when TheVault launches" size={12} />
        </div>
      </div>
    </div>

    <!-- Vardiff -->
    <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
      <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4 inline-flex items-center gap-1" style="color: var(--text-secondary);">Variable Difficulty <Info tip="Applies in both solo and proxy mining mode. In proxy mode each miner's own diff can still rise above the pool's shared value (never below it) so a stronger device sharing the connection doesn't get rate-limited" size={12} /></h3>
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
