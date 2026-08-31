// Device recognition for stratum miners. Parses the user-agent reported in
// mining.subscribe into a friendly model name, the representative ASIC chip,
// and a chip-count display (e.g. "4× BM1370"). Ported from the ASICpool
// (FirePool2) dashboard so GoVault shows the same hardware read-out.

// Known Bitaxe device family names keyed by their ASIC chip.
const ASIC_DEV: Record<string, string> = {
  BM1370: 'Gamma',
  BM1368: 'Supra',
  BM1366: 'Ultra',
  BM1397: 'Max',
  BM1387: 'S9',
  BM1362: 'S19',
};

// Boards that carry multiple ASICs but report only ONE chip token in their
// user-agent — map a firmware-name pattern to its chip count so we can show
// e.g. "4× BM1370". Boards that list every chip in the UA (e.g. "BM1372/BM1373")
// are counted by their tokens instead.
const CHIP_COUNT: Array<[RegExp, number]> = [
  [/nerdqaxe\+\+/, 4],
  [/nerdqaxe\+/, 4],
  [/nerdoctaxe/, 8],
];

export interface MinerDevice {
  model: string;    // friendly model, e.g. "Bitaxe Gamma"
  asic: string;     // representative chip, e.g. "BM1370"
  asicDisp: string; // chip display, e.g. "4× BM1370"
  count: number;    // chip count
  icon: string;     // themed glyph
  dim: boolean;     // true when nothing could be identified
}

// parseMiner turns a stratum user-agent into a friendly device read-out.
export function parseMiner(ua: string | undefined | null): MinerDevice {
  if (!ua) return { model: 'Unknown', asic: '', asicDisp: '', count: 0, icon: '?', dim: true };
  const s = ua.trim();
  const low = s.toLowerCase();

  // All chip tokens in the UA (global) — handles single- and multi-token boards.
  const all = (s.match(/\b(BM\d{3,4}|BF\d{2,4}|KS\d)\b/gi) || []).map((x) => x.toUpperCase());
  // Representative chip = highest-numbered token (so "BM1372/BM1373" reads as BM1373).
  const asic = all.length
    ? all.slice().sort((a, b) => (parseInt(a.replace(/\D/g, '')) || 0) - (parseInt(b.replace(/\D/g, '')) || 0)).pop()!
    : '';
  // Chip count = max(tokens seen, known multi-chip board count).
  let count = all.length;
  for (const [re, n] of CHIP_COUNT) {
    if (re.test(low) && n > count) count = n;
  }
  if (count < 1 && asic) count = 1;
  const asicDisp = asic ? (count > 1 ? `${count}× ${asic}` : asic) : '';

  let model = s.split('/')[0].trim() || 'Unknown';
  let icon = '◈';
  if (/clusteraxe/.test(low)) { model = 'ClusterAxe'; icon = '⬡'; }
  else if (/disrupt/.test(low)) { model = 'Disruptor'; icon = '⬡'; }
  else if (/nerdqaxe/.test(low)) { model = /\+\+/.test(low) ? 'NerdQAxe++' : /\+/.test(low) ? 'NerdQAxe+' : 'NerdQAxe'; icon = '⬢'; }
  else if (/esp-?miner|bitaxe/.test(low)) { model = 'Bitaxe'; icon = '⬡'; }
  else if (/luckyminer|lucky/.test(low)) { model = 'LuckyMiner'; icon = '⬡'; }
  else if (/nerd/.test(low)) { model = /axe/.test(low) ? 'NerdAxe' : 'NerdMiner'; icon = '⬢'; }
  else if (/bitdsk|dsk/.test(low)) { model = 'BitDSK'; icon = '⬡'; }
  else if (/avalon/.test(low)) { model = 'Avalon'; icon = '▦'; }
  else if (/whatsminer|btminer/.test(low)) { model = 'Whatsminer'; icon = '▦'; }
  else if (/bos(miner)?|braiins/.test(low)) { model = 'Braiins OS'; icon = '▦'; }
  else if (/antminer|bmminer/.test(low)) { model = 'Antminer'; icon = '▦'; }
  else if (/cgminer|bfgminer/.test(low)) { model = 'ASIC'; icon = '▦'; }
  else if (/cpuminer|xmrig/.test(low)) { model = 'CPU'; icon = '▢'; }
  else if (model.length > 18) { model = model.slice(0, 16) + '…'; }

  // Prefer the Bitaxe device family name when we know the chip.
  if (model === 'Bitaxe' && ASIC_DEV[asic]) model = 'Bitaxe ' + ASIC_DEV[asic];

  return { model, asic, asicDisp, count, icon, dim: false };
}
