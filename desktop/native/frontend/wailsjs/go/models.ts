export namespace browserbridge {

	export class Element {
	    selector: string;
	    role: string;
	    label: string;

	    static createFrom(source: any = {}) {
	        return new Element(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.selector = source["selector"];
	        this.role = source["role"];
	        this.label = source["label"];
	    }
	}
	export class State {
	    url: string;
	    title: string;
	    image: string;
	    text: string;
	    console: string[];
	    width: number;
	    height: number;
	    owner: string;
	    targetId: string;
	    elements: Element[];
	    revision: number;

	    static createFrom(source: any = {}) {
	        return new State(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.title = source["title"];
	        this.image = source["image"];
	        this.text = source["text"];
	        this.console = source["console"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.owner = source["owner"];
	        this.targetId = source["targetId"];
	        this.elements = this.convertValues(source["elements"], Element);
	        this.revision = source["revision"];
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

export namespace config {

	export class RedactedModelConfig {
	    configured: boolean;
	    provider: string;
	    model: string;
	    baseUrl: string;
	    source: string;
	    wireApi: string;
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
	        this.wireApi = source["wireApi"];
	        this.hasKey = source["hasKey"];
	        this.keyLast4 = source["keyLast4"];
	        this.maxContextTokens = source["maxContextTokens"];
	        this.maxOutputTokens = source["maxOutputTokens"];
	        this.maxTotalTokens = source["maxTotalTokens"];
	        this.error = source["error"];
	    }
	}

}

export namespace main {

	export class BundledRuntime {
	    available: boolean;
	    release: string;
	    files: number;
	    bytes: number;
	    sha256: string;

	    static createFrom(source: any = {}) {
	        return new BundledRuntime(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.release = source["release"];
	        this.files = source["files"];
	        this.bytes = source["bytes"];
	        this.sha256 = source["sha256"];
	    }
	}
	export class CommandState {
	    id: string;
	    program: string;
	    args: string[];
	    output: string;
	    running: boolean;
	    exitCode: number;
	    truncated: boolean;

	    static createFrom(source: any = {}) {
	        return new CommandState(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.program = source["program"];
	        this.args = source["args"];
	        this.output = source["output"];
	        this.running = source["running"];
	        this.exitCode = source["exitCode"];
	        this.truncated = source["truncated"];
	    }
	}
	export class EditorFile {
	    path: string;
	    content: string;
	    hash: string;

	    static createFrom(source: any = {}) {
	        return new EditorFile(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.content = source["content"];
	        this.hash = source["hash"];
	    }
	}
	export class GitCommit {
	    hash: string;
	    author: string;
	    date: string;
	    subject: string;

	    static createFrom(source: any = {}) {
	        return new GitCommit(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.author = source["author"];
	        this.date = source["date"];
	        this.subject = source["subject"];
	    }
	}
	export class GitFile {
	    path: string;
	    previousPath?: string;
	    index: string;
	    working: string;
	    operable: boolean;

	    static createFrom(source: any = {}) {
	        return new GitFile(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.previousPath = source["previousPath"];
	        this.index = source["index"];
	        this.working = source["working"];
	        this.operable = source["operable"];
	    }
	}
	export class GitState {
	    initialized: boolean;
	    branch: string;
	    revision: string;
	    files: GitFile[];
	    branches: string[];
	    commits: GitCommit[];
	    diff: string;
	    stagedDiff: string;
	    origin: string;

	    static createFrom(source: any = {}) {
	        return new GitState(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.initialized = source["initialized"];
	        this.branch = source["branch"];
	        this.revision = source["revision"];
	        this.files = this.convertValues(source["files"], GitFile);
	        this.branches = source["branches"];
	        this.commits = this.convertValues(source["commits"], GitCommit);
	        this.diff = source["diff"];
	        this.stagedDiff = source["stagedDiff"];
	        this.origin = source["origin"];
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
	export class McpServiceScaffold {
	    created: boolean;
	    name: string;
	    directory: string;
	    transport: string;
	    url: string;
	    bearer: string;
	    commands: string;
	    docker: boolean;

	    static createFrom(source: any = {}) {
	        return new McpServiceScaffold(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created = source["created"];
	        this.name = source["name"];
	        this.directory = source["directory"];
	        this.transport = source["transport"];
	        this.url = source["url"];
	        this.bearer = source["bearer"];
	        this.commands = source["commands"];
	        this.docker = source["docker"];
	    }
	}
	export class MergeFile {
	    path: string;
	    base: string;
	    ours: string;
	    theirs: string;
	    result: string;
	    resolved: boolean;
	    editable: boolean;
	    method: string;

	    static createFrom(source: any = {}) {
	        return new MergeFile(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.base = source["base"];
	        this.ours = source["ours"];
	        this.theirs = source["theirs"];
	        this.result = source["result"];
	        this.resolved = source["resolved"];
	        this.editable = source["editable"];
	        this.method = source["method"];
	    }
	}
	export class MergePreview {
	    id: string;
	    revision: string;
	    target: string;
	    ours: string;
	    theirs: string;
	    base: string;
	    files: MergeFile[];
	    patch: string;
	    ready: boolean;
	    engine: string;
	    path: string;

	    static createFrom(source: any = {}) {
	        return new MergePreview(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.revision = source["revision"];
	        this.target = source["target"];
	        this.ours = source["ours"];
	        this.theirs = source["theirs"];
	        this.base = source["base"];
	        this.files = this.convertValues(source["files"], MergeFile);
	        this.patch = source["patch"];
	        this.ready = source["ready"];
	        this.engine = source["engine"];
	        this.path = source["path"];
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
	export class ProjectFile {
	    path: string;
	    size: number;
	    hash: string;

	    static createFrom(source: any = {}) {
	        return new ProjectFile(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.size = source["size"];
	        this.hash = source["hash"];
	    }
	}
	export class ProtocolEvent {
	    sequence: number;
	    message: number[];

	    static createFrom(source: any = {}) {
	        return new ProtocolEvent(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sequence = source["sequence"];
	        this.message = source["message"];
	    }
	}
	export class ProtocolState {
	    id: string;
	    kind: string;
	    rootUri: string;
	    running: boolean;
	    sequence: number;
	    events: ProtocolEvent[];
	    dropped: boolean;
	    error: string;

	    static createFrom(source: any = {}) {
	        return new ProtocolState(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.rootUri = source["rootUri"];
	        this.running = source["running"];
	        this.sequence = source["sequence"];
	        this.events = this.convertValues(source["events"], ProtocolEvent);
	        this.dropped = source["dropped"];
	        this.error = source["error"];
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
	export class RuntimeConnection {
	    baseUrl: string;
	    source: string;

	    static createFrom(source: any = {}) {
	        return new RuntimeConnection(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.source = source["source"];
	    }
	}
	export class RuntimeService {
	    name: string;
	    state: string;
	    health: string;

	    static createFrom(source: any = {}) {
	        return new RuntimeService(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.state = source["state"];
	        this.health = source["health"];
	    }
	}
	export class RuntimeManagement {
	    directory: string;
	    configured: boolean;
	    running: boolean;
	    observed: boolean;
	    busy: boolean;
	    phase: string;
	    message: string;
	    checkedAt: string;
	    services: RuntimeService[];

	    static createFrom(source: any = {}) {
	        return new RuntimeManagement(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.directory = source["directory"];
	        this.configured = source["configured"];
	        this.running = source["running"];
	        this.observed = source["observed"];
	        this.busy = source["busy"];
	        this.phase = source["phase"];
	        this.message = source["message"];
	        this.checkedAt = source["checkedAt"];
	        this.services = this.convertValues(source["services"], RuntimeService);
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

export namespace runtimecheck {

	export class Check {
	    id: string;
	    name: string;
	    state: string;
	    detail: string;

	    static createFrom(source: any = {}) {
	        return new Check(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.state = source["state"];
	        this.detail = source["detail"];
	    }
	}
	export class Report {
	    // Go type: time
	    checkedAt: any;
	    checks: Check[];

	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.checkedAt = this.convertValues(source["checkedAt"], null);
	        this.checks = this.convertValues(source["checks"], Check);
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
