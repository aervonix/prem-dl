export namespace apperr {
	
	export class AppError {
	    code: string;
	    title: string;
	    message: string;
	    hint: string;
	    action: string;
	    detail: string;
	    retryable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppError(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.title = source["title"];
	        this.message = source["message"];
	        this.hint = source["hint"];
	        this.action = source["action"];
	        this.detail = source["detail"];
	        this.retryable = source["retryable"];
	    }
	}

}

export namespace main {
	
	export class ToolStatus {
	    checking: boolean;
	    ytdlp?: ytdlp.Binary;
	    ytdlpError: string;
	    ffmpeg?: ytdlp.FFmpeg;
	    jsRuntimes: ytdlp.JSRuntime[];
	    managedPath: string;
	    ffmpegManaged: boolean;
	    canInstallFfmpeg: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ToolStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.checking = source["checking"];
	        this.ytdlp = this.convertValues(source["ytdlp"], ytdlp.Binary);
	        this.ytdlpError = source["ytdlpError"];
	        this.ffmpeg = this.convertValues(source["ffmpeg"], ytdlp.FFmpeg);
	        this.jsRuntimes = this.convertValues(source["jsRuntimes"], ytdlp.JSRuntime);
	        this.managedPath = source["managedPath"];
	        this.ffmpegManaged = source["ffmpegManaged"];
	        this.canInstallFfmpeg = source["canInstallFfmpeg"];
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
	export class AppState {
	    version: string;
	    settings: settings.Settings;
	    jobs: queue.Job[];
	    tools: ToolStatus;
	    dataDir: string;
	
	    static createFrom(source: any = {}) {
	        return new AppState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.settings = this.convertValues(source["settings"], settings.Settings);
	        this.jobs = this.convertValues(source["jobs"], queue.Job);
	        this.tools = this.convertValues(source["tools"], ToolStatus);
	        this.dataDir = source["dataDir"];
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
	export class LinkFailure {
	    url: string;
	    error?: apperr.AppError;
	
	    static createFrom(source: any = {}) {
	        return new LinkFailure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.error = this.convertValues(source["error"], apperr.AppError);
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
	export class BulkResponse {
	    added: number;
	    skipped: number;
	    failed: LinkFailure[];
	    error: string;
	    cancelled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BulkResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.skipped = source["skipped"];
	        this.failed = this.convertValues(source["failed"], LinkFailure);
	        this.error = source["error"];
	        this.cancelled = source["cancelled"];
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
	export class ChromeHelperInfo {
	    path: string;
	    extensionId: string;
	    chromeFound: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ChromeHelperInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.extensionId = source["extensionId"];
	        this.chromeFound = source["chromeFound"];
	        this.error = source["error"];
	    }
	}
	export class CookieResponse {
	    info: ytdlp.CookieInfo;
	    settings: settings.Settings;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new CookieResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.info = this.convertValues(source["info"], ytdlp.CookieInfo);
	        this.settings = this.convertValues(source["settings"], settings.Settings);
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
	export class EnqueueResponse {
	    added: number;
	    skipped: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new EnqueueResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.skipped = source["skipped"];
	        this.error = source["error"];
	    }
	}
	
	export class ProbeResponse {
	    result?: ytdlp.ProbeResult;
	    error?: apperr.AppError;
	    cancelled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProbeResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.result = this.convertValues(source["result"], ytdlp.ProbeResult);
	        this.error = this.convertValues(source["error"], apperr.AppError);
	        this.cancelled = source["cancelled"];
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
	export class SettingsResponse {
	    settings: settings.Settings;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SettingsResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], settings.Settings);
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
	
	export class UpdateInfo {
	    current: string;
	    latest: string;
	    available: boolean;
	    channel: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	        this.channel = source["channel"];
	        this.error = source["error"];
	    }
	}
	export class UpdateResult {
	    ok: boolean;
	    previous: string;
	    version: string;
	    path: string;
	    message: string;
	    error?: apperr.AppError;
	
	    static createFrom(source: any = {}) {
	        return new UpdateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.previous = source["previous"];
	        this.version = source["version"];
	        this.path = source["path"];
	        this.message = source["message"];
	        this.error = this.convertValues(source["error"], apperr.AppError);
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

export namespace queue {
	
	export class Job {
	    id: string;
	    seq: number;
	    url: string;
	    title: string;
	    uploader: string;
	    thumbnail: string;
	    duration: number;
	    status: string;
	    stage: string;
	    percent: number;
	    downloaded: number;
	    total: number;
	    speed: number;
	    eta: number;
	    playlistTitle: string;
	    playlistIndex: number;
	    playlistCount: number;
	    options: settings.DownloadOptions;
	    outputFile: string;
	    note: string;
	    error?: apperr.AppError;
	    warnings: string[];
	    attempts: number;
	    retryAt: string;
	    autoRetries: number;
	    createdAt: string;
	    startedAt: string;
	    finishedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Job(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.seq = source["seq"];
	        this.url = source["url"];
	        this.title = source["title"];
	        this.uploader = source["uploader"];
	        this.thumbnail = source["thumbnail"];
	        this.duration = source["duration"];
	        this.status = source["status"];
	        this.stage = source["stage"];
	        this.percent = source["percent"];
	        this.downloaded = source["downloaded"];
	        this.total = source["total"];
	        this.speed = source["speed"];
	        this.eta = source["eta"];
	        this.playlistTitle = source["playlistTitle"];
	        this.playlistIndex = source["playlistIndex"];
	        this.playlistCount = source["playlistCount"];
	        this.options = this.convertValues(source["options"], settings.DownloadOptions);
	        this.outputFile = source["outputFile"];
	        this.note = source["note"];
	        this.error = this.convertValues(source["error"], apperr.AppError);
	        this.warnings = source["warnings"];
	        this.attempts = source["attempts"];
	        this.retryAt = source["retryAt"];
	        this.autoRetries = source["autoRetries"];
	        this.createdAt = source["createdAt"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
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
	export class NewJob {
	    url: string;
	    title: string;
	    uploader: string;
	    thumbnail: string;
	    duration: number;
	    playlistTitle: string;
	    playlistIndex: number;
	    playlistCount: number;
	
	    static createFrom(source: any = {}) {
	        return new NewJob(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.title = source["title"];
	        this.uploader = source["uploader"];
	        this.thumbnail = source["thumbnail"];
	        this.duration = source["duration"];
	        this.playlistTitle = source["playlistTitle"];
	        this.playlistIndex = source["playlistIndex"];
	        this.playlistCount = source["playlistCount"];
	    }
	}

}

export namespace settings {
	
	export class Cookies {
	    mode: string;
	    browser: string;
	    profile: string;
	    file: string;
	
	    static createFrom(source: any = {}) {
	        return new Cookies(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.browser = source["browser"];
	        this.profile = source["profile"];
	        this.file = source["file"];
	    }
	}
	export class SubtitleOptions {
	    mode: string;
	    langs: string[];
	    auto: boolean;
	    format: string;
	
	    static createFrom(source: any = {}) {
	        return new SubtitleOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.langs = source["langs"];
	        this.auto = source["auto"];
	        this.format = source["format"];
	    }
	}
	export class DownloadOptions {
	    preset: string;
	    formatId: string;
	    container: string;
	    audioFormat: string;
	    audioQuality: string;
	    subtitles: SubtitleOptions;
	    embedMetadata: boolean;
	    embedThumbnail: boolean;
	    embedChapters: boolean;
	    writeDescription: boolean;
	    writeInfoJson: boolean;
	    writeThumbnail: boolean;
	    sponsorBlock: string[];
	    sectionStart: string;
	    sectionEnd: string;
	    outputDir: string;
	    playlistFolder: boolean;
	    playlistNumbering: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DownloadOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preset = source["preset"];
	        this.formatId = source["formatId"];
	        this.container = source["container"];
	        this.audioFormat = source["audioFormat"];
	        this.audioQuality = source["audioQuality"];
	        this.subtitles = this.convertValues(source["subtitles"], SubtitleOptions);
	        this.embedMetadata = source["embedMetadata"];
	        this.embedThumbnail = source["embedThumbnail"];
	        this.embedChapters = source["embedChapters"];
	        this.writeDescription = source["writeDescription"];
	        this.writeInfoJson = source["writeInfoJson"];
	        this.writeThumbnail = source["writeThumbnail"];
	        this.sponsorBlock = source["sponsorBlock"];
	        this.sectionStart = source["sectionStart"];
	        this.sectionEnd = source["sectionEnd"];
	        this.outputDir = source["outputDir"];
	        this.playlistFolder = source["playlistFolder"];
	        this.playlistNumbering = source["playlistNumbering"];
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
	export class Settings {
	    downloadDir: string;
	    filenameTemplate: string;
	    maxConcurrent: number;
	    concurrentFragments: number;
	    rateLimit: string;
	    proxy: string;
	    retries: number;
	    autoRetry: boolean;
	    useArchive: boolean;
	    cookies: Cookies;
	    autoUpdate: boolean;
	    updateChannel: string;
	    lastUpdateCheck: string;
	    ytdlpPath: string;
	    ffmpegPath: string;
	    theme: string;
	    notifySound: boolean;
	    notifyPopup: boolean;
	    extraArgs: string;
	    defaults: DownloadOptions;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloadDir = source["downloadDir"];
	        this.filenameTemplate = source["filenameTemplate"];
	        this.maxConcurrent = source["maxConcurrent"];
	        this.concurrentFragments = source["concurrentFragments"];
	        this.rateLimit = source["rateLimit"];
	        this.proxy = source["proxy"];
	        this.retries = source["retries"];
	        this.autoRetry = source["autoRetry"];
	        this.useArchive = source["useArchive"];
	        this.cookies = this.convertValues(source["cookies"], Cookies);
	        this.autoUpdate = source["autoUpdate"];
	        this.updateChannel = source["updateChannel"];
	        this.lastUpdateCheck = source["lastUpdateCheck"];
	        this.ytdlpPath = source["ytdlpPath"];
	        this.ffmpegPath = source["ffmpegPath"];
	        this.theme = source["theme"];
	        this.notifySound = source["notifySound"];
	        this.notifyPopup = source["notifyPopup"];
	        this.extraArgs = source["extraArgs"];
	        this.defaults = this.convertValues(source["defaults"], DownloadOptions);
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

export namespace ytdlp {
	
	export class Binary {
	    path: string;
	    source: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new Binary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.source = source["source"];
	        this.version = source["version"];
	    }
	}
	export class CookieInfo {
	    path: string;
	    valid: boolean;
	    problem: string;
	    count: number;
	    expired: number;
	    session: number;
	    domains: string[];
	    domainCount: number;
	
	    static createFrom(source: any = {}) {
	        return new CookieInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.valid = source["valid"];
	        this.problem = source["problem"];
	        this.count = source["count"];
	        this.expired = source["expired"];
	        this.session = source["session"];
	        this.domains = source["domains"];
	        this.domainCount = source["domainCount"];
	    }
	}
	export class CookieTest {
	    ok: boolean;
	    count: number;
	    browser: string;
	    error?: apperr.AppError;
	
	    static createFrom(source: any = {}) {
	        return new CookieTest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.count = source["count"];
	        this.browser = source["browser"];
	        this.error = this.convertValues(source["error"], apperr.AppError);
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
	export class FFmpeg {
	    path: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new FFmpeg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.version = source["version"];
	    }
	}
	export class FormatInfo {
	    id: string;
	    ext: string;
	    resolution: string;
	    width: number;
	    height: number;
	    fps: number;
	    vcodec: string;
	    acodec: string;
	    note: string;
	    filesize: number;
	    tbr: number;
	    hasVideo: boolean;
	    hasAudio: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FormatInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.ext = source["ext"];
	        this.resolution = source["resolution"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.fps = source["fps"];
	        this.vcodec = source["vcodec"];
	        this.acodec = source["acodec"];
	        this.note = source["note"];
	        this.filesize = source["filesize"];
	        this.tbr = source["tbr"];
	        this.hasVideo = source["hasVideo"];
	        this.hasAudio = source["hasAudio"];
	    }
	}
	export class JSRuntime {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new JSRuntime(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class PlaylistEntry {
	    index: number;
	    id: string;
	    title: string;
	    url: string;
	    uploader: string;
	    thumbnail: string;
	    duration: number;
	    group: string;
	    unavailable: boolean;
	    isPlaylist: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PlaylistEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.id = source["id"];
	        this.title = source["title"];
	        this.url = source["url"];
	        this.uploader = source["uploader"];
	        this.thumbnail = source["thumbnail"];
	        this.duration = source["duration"];
	        this.group = source["group"];
	        this.unavailable = source["unavailable"];
	        this.isPlaylist = source["isPlaylist"];
	    }
	}
	export class PlaylistInfo {
	    id: string;
	    title: string;
	    url: string;
	    uploader: string;
	    entries: PlaylistEntry[];
	
	    static createFrom(source: any = {}) {
	        return new PlaylistInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.url = source["url"];
	        this.uploader = source["uploader"];
	        this.entries = this.convertValues(source["entries"], PlaylistEntry);
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
	export class SubtitleTrack {
	    lang: string;
	    name: string;
	    auto: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SubtitleTrack(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lang = source["lang"];
	        this.name = source["name"];
	        this.auto = source["auto"];
	    }
	}
	export class VideoInfo {
	    id: string;
	    title: string;
	    url: string;
	    uploader: string;
	    thumbnail: string;
	    thumbnailSmall: string;
	    duration: number;
	    uploadDate: string;
	    viewCount: number;
	    isLive: boolean;
	    extractor: string;
	    formats: FormatInfo[];
	    heights: number[];
	    subtitles: SubtitleTrack[];
	    autoCaptions: SubtitleTrack[];
	    bestPremium: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VideoInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.url = source["url"];
	        this.uploader = source["uploader"];
	        this.thumbnail = source["thumbnail"];
	        this.thumbnailSmall = source["thumbnailSmall"];
	        this.duration = source["duration"];
	        this.uploadDate = source["uploadDate"];
	        this.viewCount = source["viewCount"];
	        this.isLive = source["isLive"];
	        this.extractor = source["extractor"];
	        this.formats = this.convertValues(source["formats"], FormatInfo);
	        this.heights = source["heights"];
	        this.subtitles = this.convertValues(source["subtitles"], SubtitleTrack);
	        this.autoCaptions = this.convertValues(source["autoCaptions"], SubtitleTrack);
	        this.bestPremium = source["bestPremium"];
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
	export class ProbeResult {
	    kind: string;
	    video?: VideoInfo;
	    playlist?: PlaylistInfo;
	
	    static createFrom(source: any = {}) {
	        return new ProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.video = this.convertValues(source["video"], VideoInfo);
	        this.playlist = this.convertValues(source["playlist"], PlaylistInfo);
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

