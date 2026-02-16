export namespace config {
	
	export class AppConfig {
	    theme: string;
	    logLevel: string;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.logLevel = source["logLevel"];
	    }
	}
	export class ProxyConfig {
	    url: string;
	    workerName: string;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxyConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.workerName = source["workerName"];
	        this.password = source["password"];
	    }
	}
	export class VardiffConfig {
	    minDiff: number;
	    startDiff: number;
	    maxDiff: number;
	    targetTimeSec: number;
	    retargetTimeSec: number;
	    variancePct: number;
	
	    static createFrom(source: any = {}) {
	        return new VardiffConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.minDiff = source["minDiff"];
	        this.startDiff = source["startDiff"];
	        this.maxDiff = source["maxDiff"];
	        this.targetTimeSec = source["targetTimeSec"];
	        this.retargetTimeSec = source["retargetTimeSec"];
	        this.variancePct = source["variancePct"];
	    }
	}
	export class MiningConfig {
	    coin: string;
	    payoutAddress: string;
	    coinbaseTag: string;
	
	    static createFrom(source: any = {}) {
	        return new MiningConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.coin = source["coin"];
	        this.payoutAddress = source["payoutAddress"];
	        this.coinbaseTag = source["coinbaseTag"];
	    }
	}
	export class StratumConfig {
	    port: number;
	    maxConn: number;
	    autoStart: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StratumConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.maxConn = source["maxConn"];
	        this.autoStart = source["autoStart"];
	    }
	}
	export class NodeConfig {
	    host: string;
	    port: number;
	    username: string;
	    password: string;
	    useSSL: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NodeConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.useSSL = source["useSSL"];
	    }
	}
	export class Config {
	    node: NodeConfig;
	    stratum: StratumConfig;
	    mining: MiningConfig;
	    vardiff: VardiffConfig;
	    app: AppConfig;
	    proxy: ProxyConfig;
	    miningMode: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.node = this.convertValues(source["node"], NodeConfig);
	        this.stratum = this.convertValues(source["stratum"], StratumConfig);
	        this.mining = this.convertValues(source["mining"], MiningConfig);
	        this.vardiff = this.convertValues(source["vardiff"], VardiffConfig);
	        this.app = this.convertValues(source["app"], AppConfig);
	        this.proxy = this.convertValues(source["proxy"], ProxyConfig);
	        this.miningMode = source["miningMode"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	

}

export namespace logger {
	
	export class LogEntry {
	    timestamp: string;
	    level: string;
	    component: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.level = source["level"];
	        this.component = source["component"];
	        this.message = source["message"];
	    }
	}

}

export namespace miner {
	
	export class DashboardStats {
	    totalHashrate: number;
	    activeMiners: number;
	    sharesAccepted: number;
	    sharesRejected: number;
	    poolShares: number;
	    bestDifficulty: number;
	    blocksFound: number;
	    networkDifficulty: number;
	    networkHashrate: number;
	    estTimeToBlock: number;
	    blockChance: number;
	    stratumRunning: boolean;
	    blockHeight: number;
	
	    static createFrom(source: any = {}) {
	        return new DashboardStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalHashrate = source["totalHashrate"];
	        this.activeMiners = source["activeMiners"];
	        this.sharesAccepted = source["sharesAccepted"];
	        this.sharesRejected = source["sharesRejected"];
	        this.poolShares = source["poolShares"];
	        this.bestDifficulty = source["bestDifficulty"];
	        this.blocksFound = source["blocksFound"];
	        this.networkDifficulty = source["networkDifficulty"];
	        this.networkHashrate = source["networkHashrate"];
	        this.estTimeToBlock = source["estTimeToBlock"];
	        this.blockChance = source["blockChance"];
	        this.stratumRunning = source["stratumRunning"];
	        this.blockHeight = source["blockHeight"];
	    }
	}
	export class DiscoveredMiner {
	    ip: string;
	    hostname: string;
	    model: string;
	    hashrate: number;
	    temperature: number;
	    currentPool: string;
	    firmware: string;
	
	    static createFrom(source: any = {}) {
	        return new DiscoveredMiner(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.hostname = source["hostname"];
	        this.model = source["model"];
	        this.hashrate = source["hashrate"];
	        this.temperature = source["temperature"];
	        this.currentPool = source["currentPool"];
	        this.firmware = source["firmware"];
	    }
	}
	export class HashratePoint {
	    t: number;
	    h: number;
	
	    static createFrom(source: any = {}) {
	        return new HashratePoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.h = source["h"];
	    }
	}
	export class MinerInfo {
	    id: string;
	    workerName: string;
	    userAgent: string;
	    ipAddress: string;
	    // Go type: time
	    connectedAt: any;
	    currentDiff: number;
	    hashrate: number;
	    sharesAccepted: number;
	    sharesRejected: number;
	    // Go type: time
	    lastShareTime: any;
	    bestDifficulty: number;
	
	    static createFrom(source: any = {}) {
	        return new MinerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workerName = source["workerName"];
	        this.userAgent = source["userAgent"];
	        this.ipAddress = source["ipAddress"];
	        this.connectedAt = this.convertValues(source["connectedAt"], null);
	        this.currentDiff = source["currentDiff"];
	        this.hashrate = source["hashrate"];
	        this.sharesAccepted = source["sharesAccepted"];
	        this.sharesRejected = source["sharesRejected"];
	        this.lastShareTime = this.convertValues(source["lastShareTime"], null);
	        this.bestDifficulty = source["bestDifficulty"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

