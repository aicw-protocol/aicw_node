export namespace install {
	
	export class LocalSetup {
	    installDir: string;
	    nodeBinaryPresent: boolean;
	    networkConfigFound: boolean;
	    passwordFound: boolean;
	    identityFound: boolean;
	    nodeName?: string;
	    nodeId?: string;
	    publicKey?: string;
	    readyToStart: boolean;
	    missingItems: string[];
	
	    static createFrom(source: any = {}) {
	        return new LocalSetup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installDir = source["installDir"];
	        this.nodeBinaryPresent = source["nodeBinaryPresent"];
	        this.networkConfigFound = source["networkConfigFound"];
	        this.passwordFound = source["passwordFound"];
	        this.identityFound = source["identityFound"];
	        this.nodeName = source["nodeName"];
	        this.nodeId = source["nodeId"];
	        this.publicKey = source["publicKey"];
	        this.readyToStart = source["readyToStart"];
	        this.missingItems = source["missingItems"];
	    }
	}

}

export namespace main {
	
	export class BootstrapView {
	    installed: boolean;
	    installDir: string;
	    defaultInstallDir: string;
	    webBaseUrl: string;
	    version: string;
	    wallet?: string;
	    walletVerified: boolean;
	    nodeRunning: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BootstrapView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.installDir = source["installDir"];
	        this.defaultInstallDir = source["defaultInstallDir"];
	        this.webBaseUrl = source["webBaseUrl"];
	        this.version = source["version"];
	        this.wallet = source["wallet"];
	        this.walletVerified = source["walletVerified"];
	        this.nodeRunning = source["nodeRunning"];
	    }
	}
	export class BrowserSignInResult {
	    ok: boolean;
	    wallet?: string;
	    error?: string;
	    authUrl?: string;
	
	    static createFrom(source: any = {}) {
	        return new BrowserSignInResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.wallet = source["wallet"];
	        this.error = source["error"];
	        this.authUrl = source["authUrl"];
	    }
	}
	export class NodeRowView {
	    nodeId: string;
	    nodeName: string;
	    publicKey?: string;
	    webStatus: string;
	    localReady: boolean;
	    processRunning: boolean;
	    missingItems: string[];
	    canRemove: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NodeRowView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.nodeName = source["nodeName"];
	        this.publicKey = source["publicKey"];
	        this.webStatus = source["webStatus"];
	        this.localReady = source["localReady"];
	        this.processRunning = source["processRunning"];
	        this.missingItems = source["missingItems"];
	        this.canRemove = source["canRemove"];
	    }
	}
	export class OffboardStatusView {
	    pendingUnstake: boolean;
	    returnAvailableAt?: string;
	    hoursUntilReturn?: number;
	    registeredNodeCount: number;
	
	    static createFrom(source: any = {}) {
	        return new OffboardStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pendingUnstake = source["pendingUnstake"];
	        this.returnAvailableAt = source["returnAvailableAt"];
	        this.hoursUntilReturn = source["hoursUntilReturn"];
	        this.registeredNodeCount = source["registeredNodeCount"];
	    }
	}
	export class DashboardView {
	    ok: boolean;
	    error?: string;
	    wallet?: string;
	    walletVerified: boolean;
	    installDir: string;
	    installed: boolean;
	    runningNodeName?: string;
	    runningNodeNames: string[];
	    runningCount: number;
	    maxConcurrentNodes: number;
	    registerUrl: string;
	    dashboardUrl: string;
	    stakingUrl: string;
	    canRegister: boolean;
	    sharedMissing: string[];
	    offboard?: OffboardStatusView;
	    nodes: NodeRowView[];
	
	    static createFrom(source: any = {}) {
	        return new DashboardView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.wallet = source["wallet"];
	        this.walletVerified = source["walletVerified"];
	        this.installDir = source["installDir"];
	        this.installed = source["installed"];
	        this.runningNodeName = source["runningNodeName"];
	        this.runningNodeNames = source["runningNodeNames"];
	        this.runningCount = source["runningCount"];
	        this.maxConcurrentNodes = source["maxConcurrentNodes"];
	        this.registerUrl = source["registerUrl"];
	        this.dashboardUrl = source["dashboardUrl"];
	        this.stakingUrl = source["stakingUrl"];
	        this.canRegister = source["canRegister"];
	        this.sharedMissing = source["sharedMissing"];
	        this.offboard = this.convertValues(source["offboard"], OffboardStatusView);
	        this.nodes = this.convertValues(source["nodes"], NodeRowView);
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
	export class InstallResult {
	    ok: boolean;
	    installDir?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new InstallResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.installDir = source["installDir"];
	        this.error = source["error"];
	    }
	}
	export class NodeActionResult {
	    ok: boolean;
	    cancelled?: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.cancelled = source["cancelled"];
	        this.error = source["error"];
	    }
	}
	
	
	export class RegisterNodeResult {
	    ok: boolean;
	    pending?: boolean;
	    error?: string;
	    nodeId?: string;
	    nodeName?: string;
	    publicKey?: string;
	    authUrl?: string;
	
	    static createFrom(source: any = {}) {
	        return new RegisterNodeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.pending = source["pending"];
	        this.error = source["error"];
	        this.nodeId = source["nodeId"];
	        this.nodeName = source["nodeName"];
	        this.publicKey = source["publicKey"];
	        this.authUrl = source["authUrl"];
	    }
	}
	export class RegisterStatusView {
	    active: boolean;
	    phase: string;
	    nodeName?: string;
	    result?: RegisterNodeResult;
	
	    static createFrom(source: any = {}) {
	        return new RegisterStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.phase = source["phase"];
	        this.nodeName = source["nodeName"];
	        this.result = this.convertValues(source["result"], RegisterNodeResult);
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
	export class UnstakeNodeResult {
	    ok: boolean;
	    error?: string;
	    phase?: string;
	    message?: string;
	    returnAvailableAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new UnstakeNodeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.phase = source["phase"];
	        this.message = source["message"];
	        this.returnAvailableAt = source["returnAvailableAt"];
	    }
	}
	export class WalletStatusView {
	    ok: boolean;
	    error?: string;
	    status?: nodeweb.WalletStatus;
	    local?: install.LocalSetup;
	
	    static createFrom(source: any = {}) {
	        return new WalletStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.status = this.convertValues(source["status"], nodeweb.WalletStatus);
	        this.local = this.convertValues(source["local"], install.LocalSetup);
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

export namespace nodeweb {
	
	export class Eligibility {
	    registeredNodeCount: number;
	    requiredStakeSol: number;
	    canRegister: boolean;
	    blockReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new Eligibility(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.registeredNodeCount = source["registeredNodeCount"];
	        this.requiredStakeSol = source["requiredStakeSol"];
	        this.canRegister = source["canRegister"];
	        this.blockReason = source["blockReason"];
	    }
	}
	export class GuiURLs {
	    recommendedAction: string;
	    canLaunchNode: boolean;
	    stakingUrl: string;
	    dashboardUrl: string;
	    registerUrl: string;
	    onboardingUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new GuiURLs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recommendedAction = source["recommendedAction"];
	        this.canLaunchNode = source["canLaunchNode"];
	        this.stakingUrl = source["stakingUrl"];
	        this.dashboardUrl = source["dashboardUrl"];
	        this.registerUrl = source["registerUrl"];
	        this.onboardingUrl = source["onboardingUrl"];
	    }
	}
	export class NodeRecord {
	    nodeId: string;
	    nodeName?: string;
	    publicKey?: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.nodeName = source["nodeName"];
	        this.publicKey = source["publicKey"];
	        this.status = source["status"];
	    }
	}
	export class WalletStatus {
	    wallet: string;
	    eligibility: Eligibility;
	    nodes: NodeRecord[];
	    gui: GuiURLs;
	
	    static createFrom(source: any = {}) {
	        return new WalletStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.wallet = source["wallet"];
	        this.eligibility = this.convertValues(source["eligibility"], Eligibility);
	        this.nodes = this.convertValues(source["nodes"], NodeRecord);
	        this.gui = this.convertValues(source["gui"], GuiURLs);
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

