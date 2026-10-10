export namespace main {

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

}
