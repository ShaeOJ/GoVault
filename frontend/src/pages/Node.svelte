<script lang="ts">
  import { onMount } from 'svelte';
  import Toggle from '../lib/components/common/Toggle.svelte';
  import Info from '../lib/components/common/Info.svelte';
  import ThemedSpinner from '../lib/components/common/ThemedSpinner.svelte';
  import { formatNumber, formatDifficulty, formatHashrate } from '../lib/utils/format';

  let host = '127.0.0.1';
  let port = 8332;
  let username = 'bitcoin';
  let password = '';
  let useSSL = false;

  let testing = false;
  let testResult: any = null;
  let testError = '';
  let saving = false;
  let savingStep = ''; // 'saving' | 'connecting'
  let nodeStatus: any = null;
  let loaded = false;
  let coinName = 'Bitcoin';
  let coinSymbol = 'BTC';
  let coinId = 'btc';
  let copied = false;

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
      // Get coin info for display
      const coinList = await GetCoinList() || [];
      const selectedCoin = cfg?.mining?.coin || 'btc';
      coinId = selectedCoin;
      const coinDef = coinList.find((c: any) => c.id === selectedCoin);
      if (coinDef) {
        coinName = coinDef.name;
        coinSymbol = coinDef.symbol;
      }
    } catch {}

    refreshStatus();
    loaded = true;
  });

  async function refreshStatus() {
    try {
      const { GetNodeStatus } = await import('../../wailsjs/go/main/App');
      nodeStatus = await GetNodeStatus();
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

  async function saveAndConnect() {
    saving = true;
    savingStep = 'saving';
    testResult = null;
    testError = '';
    try {
      const { GetConfig, UpdateConfig, ConnectNode } = await import('../../wailsjs/go/main/App');
      const cfg = await GetConfig();
      cfg.node = { host, port, username, password, useSSL };
      await UpdateConfig(cfg);

      savingStep = 'connecting';
      const status = await ConnectNode();
      nodeStatus = status;
    } catch (e: any) {
      testError = e?.message || String(e);
    }
    saving = false;
    savingStep = '';
  }
</script>

<div class="space-y-6">
  <div>
    <h1 class="text-2xl font-bold font-tech uppercase tracking-wide" style="color: var(--text-primary);">{coinName} Node</h1>
    <p class="text-sm" style="color: var(--text-secondary);">Configure your local {coinName} node connection</p>
  </div>

  <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
    <!-- Connection Settings -->
    <div class="rounded-xl p-6 card-glow" style="background-color: var(--bg-card);">
      <h3 class="text-sm font-medium font-tech uppercase tracking-wider mb-4" style="color: var(--text-secondary);">Connection Settings</h3>

      <div class="space-y-4">
        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="host">RPC Host <Info tip="Bitcoin node IP or hostname (usually 127.0.0.1 for local)" size={12} /></label>
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
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="user">RPC Username <Info tip="From bitcoin.conf rpcuser setting" size={12} /></label>
          <input
            id="user"
            bind:value={username}
            class="w-full rounded-lg px-3 py-2 text-sm input-themed"
            placeholder="bitcoin"
          />
        </div>

        <div>
          <label class="block text-xs mb-1.5 inline-flex items-center gap-1" style="color: var(--text-secondary);" for="pass">RPC Password <Info tip="From bitcoin.conf rpcpassword setting" size={12} /></label>
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

        <!-- Test Result -->
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

    <!-- Node Status -->
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
                <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Network Difficulty <Info tip="Global Bitcoin mining difficulty target" size={11} /></div>
                <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatDifficulty(nodeStatus.networkDifficulty || 0)}</div>
              </div>
              <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Network Hashrate <Info tip="Estimated total global hash rate" size={11} /></div>
                <div class="text-sm font-medium font-data" style="color: var(--text-primary);">{formatHashrate(nodeStatus.networkHashrate || 0)}</div>
              </div>
              {#if nodeStatus.connections !== undefined}
                <div class="rounded-lg p-3" style="background-color: var(--bg-secondary);">
                  <div class="text-xs inline-flex items-center gap-1" style="color: var(--text-secondary);">Peers <Info tip="Bitcoin nodes connected for block propagation" size={11} /></div>
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
          <h3 class="text-sm font-medium font-tech uppercase tracking-wider inline-flex items-center gap-1" style="color: var(--text-secondary);">{coinName}.conf <Info tip="Sample bitcoin.conf to enable RPC. Copy to your node's data directory" size={12} /></h3>
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
</div>
