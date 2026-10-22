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
	export class EditWorkInput {
	    url: string;
	    title: string;
	    siteName: string;
	    notes: string;

	    static createFrom(source: any = {}) {
	        return new EditWorkInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.title = source["title"];
	        this.siteName = source["siteName"];
	        this.notes = source["notes"];
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

}
