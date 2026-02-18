<script lang="ts">
  import { onMount } from 'svelte';
  import Toggle from '../lib/components/common/Toggle.svelte';
  import Info from '../lib/components/common/Info.svelte';
  import ThemedSpinner from '../lib/components/common/ThemedSpinner.svelte';
  import { formatNumber, formatDifficulty, formatHashrate } from '../lib/utils/format';

  // Mode: 'solo' or 'proxy'
  let miningMode: 'solo' | 'proxy' = 'solo';

  // Solo mode fields
  let host = '127.0.0.1';
  let port = 8332;
  let username = 'bitcoin';
  let password = '';
  let useSSL = false;

  // Proxy mode fields
  let proxyUrl = '';
  let proxyWorker = '';
  let proxyPassword = 'x';

  let testing = false;
  let testResult: any = null;
  let testError = '';
  let saving = false;
  let savingStep = '';
  let nodeStatus: any = null;
  let loaded = false;
  let coinName = 'Bitcoin';
  let coinSymbol = 'BTC';
  let coinId = 'btc';
  let coinList: any[] = [];
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

  $: generatedConfig = [
    '# GoVault - RPC Configuration',
    'server=1',
    `rpcuser=${username || 'yourusername'}`,
    `rpcpassword=${password || 'yourpassword'}`,
    `rpcport=${port}`,
    'rpcallowip=127.0.0.1',
    ...(host !== '127.0.0.1' && host !== 'localhost' ? [`rpcallowip=${host}`] : []),
    '',
    '# Required for mining',
    'txindex=1',
  ].join('\n');

  async function copyConfig() {
    try {
      await navigator.clipboard.writeText(generatedConfig);
      copied = true;
      setTimeout(() => copied = false, 2000);
    } catch {}
  }

  async function detectNode() {
    detecting = true;
    detectResult = null;
    detectError = '';
    try {
      const { DetectNode } = await import('../../wailsjs/go/main/App');
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

  onMount(async () => {
    try {
      const { GetConfig, GetCoinList } = await import('../../wailsjs/go/main/App');
      const cfg = await GetConfig();
      if (cfg?.node) {
        host = cfg.node.host || host;
        port = cfg.node.port || port;
        username = cfg.node.username || username;
        password = cfg.node.password || '';
        useSSL = cfg.node.useSSL || false;
      }
      if (cfg?.proxy) {
        proxyUrl = cfg.proxy.url || '';
        proxyWorker = cfg.proxy.workerName || '';
        proxyPassword = cfg.proxy.password || 'x';
      }
      miningMode = cfg?.miningMode === 'proxy' ? 'proxy' : 'solo';

      // Get coin info for display
      coinList = await GetCoinList() || [];
      const selectedCoin = cfg?.mining?.coin || 'btc';
      coinId = selectedCoin;
      const coinDef = coinList.find((c: any) => c.id === selectedCoin);
      if (coinDef) {
        coinName = coinDef.name;
        coinSymbol = coinDef.symbol;
      }
    } catch {}

    if (miningMode === 'solo') {
      refreshStatus();
    } else {
      refreshUpstreamStatus();
    }
    loaded = true;
  });

  async function refreshStatus() {
    try {
      const { GetNodeStatus } = await import('../../wailsjs/go/main/App');
      nodeStatus = await GetNodeStatus();
    } catch {}
  }

  async function refreshUpstreamStatus() {
    try {
      const { GetUpstreamStatus } = await import('../../wailsjs/go/main/App');
      upstreamStatus = await GetUpstreamStatus();
    } catch {}
  }

  async function testConnection() {
    testing = true;
    testResult = null;
    testError = '';
    try {
      const { TestNodeConnection } = await import('../../wailsjs/go/main/App');
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
      const { TestUpstreamConnection } = await import('../../wailsjs/go/main/App');
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

  async function saveAndConnect() {
    saving = true;
    savingStep = 'saving';
    testResult = null;
    testError = '';
    try {
      const { GetConfig, UpdateConfig, ConnectNode } = await import('../../wailsjs/go/main/App');
      const cfg = await GetConfig();
      cfg.node = { host, port, username, password, useSSL };
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
    try {
      const { GetConfig, UpdateConfig, IsStratumRunning, StopStratum, StartStratum } = await import('../../wailsjs/go/main/App');
      const cfg = await GetConfig();
      cfg.proxy = { url: proxyUrl, workerName: proxyWorker, password: proxyPassword || 'x' };
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
    { name: 'CKPool Solo', url: 'solo.ckpool.org:3333', desc: 'Most popular solo pool, 2% fee' },
    { name: 'CKPool Solo (High Diff)', url: 'solo.ckpool.org:3334', desc: 'For high-hashrate setups' },
    { name: 'Public Pool', url: 'public-pool.io:21496', desc: 'Open source, no fee solo pool' },
    { name: 'Firepool', url: 'firepool.ca:4333', desc: 'Canadian solo pool' },
  ];

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
  <div>
    <h1 class="text-2xl font-bold font-tech uppercase tracking-wide" style="color: var(--text-primary);">Mining Source</h1>
    <p class="text-sm" style="color: var(--text-secondary);">Choose how GoVault gets work for your miners</p>
  </div>

  <!-- Mode Selector -->
  <div class="flex gap-3">
    <button
      class="flex-1 px-4 py-3 rounded-xl text-sm font-medium font-tech uppercase tracking-wider transition-all flex items-center justify-center gap-2"
      style="{miningMode === 'solo'
        ? 'background: rgba(var(--accent-rgb), 0.15); color: var(--accent); border: 2px solid var(--accent); box-shadow: 0 0 12px rgba(var(--accent-rgb), 0.2);'
        : 'background-color: var(--bg-card); color: var(--text-secondary); border: 2px solid var(--border);'}"
      on:click={() => setMode('solo')}
    >
      Solo (Local Node)
    </button>
    <button
      class="flex-1 px-4 py-3 rounded-xl text-sm font-medium font-tech uppercase tracking-wider transition-all flex items-center justify-center gap-2"
      style="{miningMode === 'proxy'
        ? 'background: rgba(var(--accent-rgb), 0.15); color: var(--accent); border: 2px solid var(--accent); box-shadow: 0 0 12px rgba(var(--accent-rgb), 0.2);'
        : 'background-color: var(--bg-card); color: var(--text-secondary); border: 2px solid var(--border);'}"
      on:click={() => setMode('proxy')}
    >
      Proxy (Upstream Pool)
    </button>
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
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="host">RPC Host <Info tip="{coinName} node IP or hostname (usually 127.0.0.1 for local)" size={12} /></label>
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
                  <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Network Difficulty <Info tip="Global {coinName} mining difficulty target" size={11} /></div>
                  <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatDifficulty(nodeStatus.networkDifficulty || 0)}</div>
                </div>
                <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                  <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Network Hashrate <Info tip="Estimated total {coinName} network hash rate" size={11} /></div>
                  <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatHashrate(nodeStatus.networkHashrate || 0)}</div>
                </div>
                {#if nodeStatus.connections !== undefined}
                  <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                    <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Peers <Info tip="{coinName} nodes connected for block propagation" size={11} /></div>
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
            <h3 class="text-sm font-medium font-tech uppercase tracking-wider inline-flex items-center gap-1" style="color: var(--text-secondary);">{coinName}.conf <Info tip="Sample {configFileName} to enable RPC. Copy to your node's data directory" size={12} /></h3>
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
            <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="proxy-coin">
              Blockchain <Info tip="Select which cryptocurrency the upstream pool is mining" size={12} />
            </label>
            <select
              id="proxy-coin"
              bind:value={coinId}
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
              Worker Name <Info tip="Your Bitcoin address or pool username. All miners share this identity upstream" size={12} />
            </label>
            <input
              id="proxy-worker"
              bind:value={proxyWorker}
              class="w-full rounded-lg px-3 py-2 text-sm input-themed"
              placeholder="your_bitcoin_address"
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
          <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-3" style="color: var(--text-secondary);">Pool Presets</h3>
          <div class="space-y-2">
            {#each poolPresets as preset}
              <button
                class="w-full text-left rounded-lg p-3 transition-all"
                style="background-color: var(--bg-secondary); border: 1px solid {proxyUrl === preset.url ? 'var(--accent)' : 'transparent'}; {proxyUrl === preset.url ? 'box-shadow: 0 0 8px rgba(var(--accent-rgb), 0.15);' : ''}"
                on:click={() => applyPreset(preset)}
              >
                <div class="flex items-center justify-between">
                  <span class="text-sm font-medium font-tech" style="color: {proxyUrl === preset.url ? 'var(--accent)' : 'var(--text-primary)'};">{preset.name}</span>
                  {#if proxyUrl === preset.url}
                    <span class="text-[10px] font-tech uppercase px-1.5 py-0.5 rounded" style="background: rgba(var(--accent-rgb), 0.15); color: var(--accent);">Selected</span>
                  {/if}
                </div>
                <div class="text-xs font-data mt-0.5" style="color: var(--text-secondary);">{preset.url}</div>
                <div class="text-xs mt-0.5" style="color: var(--text-secondary); opacity: 0.7;">{preset.desc}</div>
              </button>
            {/each}
          </div>
        </div>

        <!-- How it works -->
        <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
          <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-3" style="color: var(--text-secondary);">How Proxy Mode Works</h3>
          <div class="text-xs space-y-2" style="color: var(--text-secondary); line-height: 1.6;">
            <p>GoVault connects as a single miner to the upstream pool and relays work to all your local miners.</p>
            <p>Each miner gets its own difficulty via vardiff. Shares that meet the upstream difficulty are forwarded to the pool.</p>
            <p>Hashrate tracking, miner monitoring, and all dashboard stats work the same as solo mode.</p>
          </div>
        </div>
      </div>
    </div>
  {/if}
</div>
