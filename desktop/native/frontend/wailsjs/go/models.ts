export namespace main {

	export class PatchApplication {
	    applied: boolean;
	    fileCount: number;
	    path: string;

	    static createFrom(source: any = {}) {
	        return new PatchApplication(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applied = source["applied"];
	        this.fileCount = source["fileCount"];
	        this.path = source["path"];
	    }
	}
	export class ProxyResponse {
	    status: number;
	    body: string;

	    static createFrom(source: any = {}) {
	        return new ProxyResponse(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.body = source["body"];
	    }
	}
	export class RedactedModelConfig {
	    configured: boolean;
	    provider: string;
	    model: string;
	    baseUrl: string;
	    source: string;
	    hasKey: boolean;
	    keyLast4: string;
	    maxContextTokens: number;
	    maxOutputTokens: number;
	    maxTotalTokens: number;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new RedactedModelConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.baseUrl = source["baseUrl"];
	        this.source = source["source"];
	        this.hasKey = source["hasKey"];
	        this.keyLast4 = source["keyLast4"];
	        this.maxContextTokens = source["maxContextTokens"];
	        this.maxOutputTokens = source["maxOutputTokens"];
	        this.maxTotalTokens = source["maxTotalTokens"];
	        this.error = source["error"];
	    }
	}
	export class ScratchProject {
	    name: string;
	    localHandle: string;
	    bootstrapId: string;
	    path: string;

	    static createFrom(source: any = {}) {
	        return new ScratchProject(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.localHandle = source["localHandle"];
	        this.bootstrapId = source["bootstrapId"];
	        this.path = source["path"];
	    }
	}
	export class sourceFile {
	    path: string;
	    sha256: string;
	    content: string;
	    executable: boolean;

	    static createFrom(source: any = {}) {
	        return new sourceFile(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.sha256 = source["sha256"];
	        this.content = source["content"];
	        this.executable = source["executable"];
	    }
	}
	export class SourceArchive {
	    manifestHash: string;
	    files: sourceFile[];

	    static createFrom(source: any = {}) {
	        return new SourceArchive(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.manifestHash = source["manifestHash"];
	        this.files = this.convertValues(source["files"], sourceFile);
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
