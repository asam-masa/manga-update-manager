export namespace main {

	export class CoverProgressDTO {
	    operationID: string;
	    workID: number;
	    kind: string;
	    stage: string;
	    timed: boolean;
	    elapsedMilliseconds: number;

	    static createFrom(source: any = {}) {
	        return new CoverProgressDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.operationID = source["operationID"];
	        this.workID = source["workID"];
	        this.kind = source["kind"];
	        this.stage = source["stage"];
	        this.timed = source["timed"];
	        this.elapsedMilliseconds = source["elapsedMilliseconds"];
	    }
	}
	export class CoverWarningDTO {
	    code: string;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new CoverWarningDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}
	export class CreateWorkInput {
	    url: string;
	    title: string;
	    siteName: string;
	    thumbnailPath: string;
	    notes: string;

	    static createFrom(source: any = {}) {
	        return new CreateWorkInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.title = source["title"];
	        this.siteName = source["siteName"];
	        this.thumbnailPath = source["thumbnailPath"];
	        this.notes = source["notes"];
	    }
	}
	export class TagDTO {
	    id: string;
	    name: string;

	    static createFrom(source: any = {}) {
	        return new TagDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class DeleteWorkInput {
	    url: string;
	    title: string;
	    siteName: string;
	    notes: string;
	    thumbnailPath: string;
	    scheduledDate: string;
	    tags?: TagDTO[];

	    static createFrom(source: any = {}) {
	        return new DeleteWorkInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.title = source["title"];
	        this.siteName = source["siteName"];
	        this.notes = source["notes"];
	        this.thumbnailPath = source["thumbnailPath"];
	        this.scheduledDate = source["scheduledDate"];
	        this.tags = this.convertValues(source["tags"], TagDTO);
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
	export class WorkWarningDTO {
	    code: string;
	    message: string;

	    static createFrom(source: any = {}) {
	        return new WorkWarningDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}
	export class DeleteWorkResult {
	    workID: number;
	    warnings: WorkWarningDTO[];

	    static createFrom(source: any = {}) {
	        return new DeleteWorkResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workID = source["workID"];
	        this.warnings = this.convertValues(source["warnings"], WorkWarningDTO);
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
	export class DisplaySettingsDTO {
	    registrationFormVisible: boolean;

	    static createFrom(source: any = {}) {
	        return new DisplaySettingsDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.registrationFormVisible = source["registrationFormVisible"];
	    }
	}
	export class TagEditInput {
	    existing: TagDTO[];
	    newNames: string[];

	    static createFrom(source: any = {}) {
	        return new TagEditInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.existing = this.convertValues(source["existing"], TagDTO);
	        this.newNames = source["newNames"];
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
	export class EditWorkInput {
	    url: string;
	    title: string;
	    siteName: string;
	    notes: string;
	    scheduledDate?: string;
	    tags?: TagEditInput;

	    static createFrom(source: any = {}) {
	        return new EditWorkInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.title = source["title"];
	        this.siteName = source["siteName"];
	        this.notes = source["notes"];
	        this.scheduledDate = source["scheduledDate"];
	        this.tags = this.convertValues(source["tags"], TagEditInput);
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
	export class WorkDTO {
	    id: number;
	    url: string;
	    title: string;
	    siteName: string;
	    thumbnailPath: string;
	    notes: string;
	    createdAt: string;
	    updatedAt: string;
	    lastAccessedAt?: string;
	    scheduledDate?: string;

	    static createFrom(source: any = {}) {
	        return new WorkDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.title = source["title"];
	        this.siteName = source["siteName"];
	        this.thumbnailPath = source["thumbnailPath"];
	        this.notes = source["notes"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.lastAccessedAt = source["lastAccessedAt"];
	        this.scheduledDate = source["scheduledDate"];
	    }
	}
	export class EditWorkResult {
	    work: WorkDTO;
	    warnings: WorkWarningDTO[];

	    static createFrom(source: any = {}) {
	        return new EditWorkResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.work = this.convertValues(source["work"], WorkDTO);
	        this.warnings = this.convertValues(source["warnings"], WorkWarningDTO);
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
	export class RemoveWorkCoverResult {
	    work: WorkDTO;
	    warnings: CoverWarningDTO[];

	    static createFrom(source: any = {}) {
	        return new RemoveWorkCoverResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.work = this.convertValues(source["work"], WorkDTO);
	        this.warnings = this.convertValues(source["warnings"], CoverWarningDTO);
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
	export class SelectWorkCoverResult {
	    cancelled: boolean;
	    work?: WorkDTO;
	    warnings: CoverWarningDTO[];

	    static createFrom(source: any = {}) {
	        return new SelectWorkCoverResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cancelled = source["cancelled"];
	        this.work = this.convertValues(source["work"], WorkDTO);
	        this.warnings = this.convertValues(source["warnings"], CoverWarningDTO);
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

	export class TagWorkDTO {
	    id: string;
	    createdAt: string;
	    title: string;

	    static createFrom(source: any = {}) {
	        return new TagWorkDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.title = source["title"];
	    }
	}
	export class TagDeletePreviewDTO {
	    tag: TagDTO;
	    works: TagWorkDTO[];

	    static createFrom(source: any = {}) {
	        return new TagDeletePreviewDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tag = this.convertValues(source["tag"], TagDTO);
	        this.works = this.convertValues(source["works"], TagWorkDTO);
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

	export class TagUsageDTO {
	    tag: TagDTO;
	    workCount: number;

	    static createFrom(source: any = {}) {
	        return new TagUsageDTO(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tag = this.convertValues(source["tag"], TagDTO);
	        this.workCount = source["workCount"];
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
