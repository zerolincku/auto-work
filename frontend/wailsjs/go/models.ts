export namespace app {
	
	export class CreateProjectRequest {
	    name: string;
	    path: string;
	    default_provider: string;
	    model: string;
	    system_prompt: string;
	    failure_policy: string;
	    frontend_screenshot_report_enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateProjectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.default_provider = source["default_provider"];
	        this.model = source["model"];
	        this.system_prompt = source["system_prompt"];
	        this.failure_policy = source["failure_policy"];
	        this.frontend_screenshot_report_enabled = source["frontend_screenshot_report_enabled"];
	    }
	}
	export class CreateTaskRequest {
	    project_id: string;
	    title: string;
	    description: string;
	    priority: number;
	    provider: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateTaskRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_id = source["project_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.priority = source["priority"];
	        this.provider = source["provider"];
	    }
	}
	export class DispatchResponse {
	    claimed: boolean;
	    run_id?: string;
	    task_id?: string;
	    message?: string;
	
	    static createFrom(source: any = {}) {
	        return new DispatchResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.claimed = source["claimed"];
	        this.run_id = source["run_id"];
	        this.task_id = source["task_id"];
	        this.message = source["message"];
	    }
	}
	export class FinishRunRequest {
	    run_id: string;
	    status: string;
	    summary: string;
	    details: string;
	    exit_code?: number;
	    task_status: string;
	
	    static createFrom(source: any = {}) {
	        return new FinishRunRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.status = source["status"];
	        this.summary = source["summary"];
	        this.details = source["details"];
	        this.exit_code = source["exit_code"];
	        this.task_status = source["task_status"];
	    }
	}
	export class GlobalSettingsView {
	    telegram_enabled: boolean;
	    telegram_bot_token: string;
	    telegram_chat_ids: string;
	    telegram_poll_timeout: number;
	    telegram_proxy_url: string;
	    system_notification_mode: string;
	    system_prompt: string;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new GlobalSettingsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.telegram_enabled = source["telegram_enabled"];
	        this.telegram_bot_token = source["telegram_bot_token"];
	        this.telegram_chat_ids = source["telegram_chat_ids"];
	        this.telegram_poll_timeout = source["telegram_poll_timeout"];
	        this.telegram_proxy_url = source["telegram_proxy_url"];
	        this.system_notification_mode = source["system_notification_mode"];
	        this.system_prompt = source["system_prompt"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class MCPStatusView {
	    enabled: boolean;
	    state: string;
	    message: string;
	    run_id?: string;
	    // Go type: time
	    updated_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new MCPStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.state = source["state"];
	        this.message = source["message"];
	        this.run_id = source["run_id"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
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
	export class RunLogEventView {
	    id: string;
	    run_id: string;
	    // Go type: time
	    ts: any;
	    kind: string;
	    payload: string;
	
	    static createFrom(source: any = {}) {
	        return new RunLogEventView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.run_id = source["run_id"];
	        this.ts = this.convertValues(source["ts"], null);
	        this.kind = source["kind"];
	        this.payload = source["payload"];
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
	export class RunningRunView {
	    run_id: string;
	    task_id: string;
	    task_title: string;
	    project_id: string;
	    agent_id: string;
	    pid?: number;
	    status: string;
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    heartbeat_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new RunningRunView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.task_id = source["task_id"];
	        this.task_title = source["task_title"];
	        this.project_id = source["project_id"];
	        this.agent_id = source["agent_id"];
	        this.pid = source["pid"];
	        this.status = source["status"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.heartbeat_at = this.convertValues(source["heartbeat_at"], null);
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
	export class SystemLogView {
	    id: string;
	    run_id: string;
	    task_id: string;
	    task_title: string;
	    project_id: string;
	    // Go type: time
	    ts: any;
	    kind: string;
	    payload: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemLogView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.run_id = source["run_id"];
	        this.task_id = source["task_id"];
	        this.task_title = source["task_title"];
	        this.project_id = source["project_id"];
	        this.ts = this.convertValues(source["ts"], null);
	        this.kind = source["kind"];
	        this.payload = source["payload"];
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
	export class TaskRunHistoryView {
	    run_id: string;
	    status: string;
	    attempt: number;
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    finished_at?: any;
	    exit_code?: number;
	    result_summary?: string;
	    result_details?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskRunHistoryView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.status = source["status"];
	        this.attempt = source["attempt"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.finished_at = this.convertValues(source["finished_at"], null);
	        this.exit_code = source["exit_code"];
	        this.result_summary = source["result_summary"];
	        this.result_details = source["result_details"];
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
	export class TaskDetailView {
	    task?: domain.Task;
	    runs: TaskRunHistoryView[];
	
	    static createFrom(source: any = {}) {
	        return new TaskDetailView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task = this.convertValues(source["task"], domain.Task);
	        this.runs = this.convertValues(source["runs"], TaskRunHistoryView);
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
	export class TaskLatestRunView {
	    run_id: string;
	    status: string;
	    attempt: number;
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    finished_at?: any;
	    exit_code?: number;
	    result_summary?: string;
	    result_details?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskLatestRunView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.status = source["status"];
	        this.attempt = source["attempt"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.finished_at = this.convertValues(source["finished_at"], null);
	        this.exit_code = source["exit_code"];
	        this.result_summary = source["result_summary"];
	        this.result_details = source["result_details"];
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
	
	export class UpdateGlobalSettingsRequest {
	    telegram_enabled: boolean;
	    telegram_bot_token: string;
	    telegram_chat_ids: string;
	    telegram_poll_timeout: number;
	    telegram_proxy_url: string;
	    system_notification_mode: string;
	    system_prompt: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateGlobalSettingsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.telegram_enabled = source["telegram_enabled"];
	        this.telegram_bot_token = source["telegram_bot_token"];
	        this.telegram_chat_ids = source["telegram_chat_ids"];
	        this.telegram_poll_timeout = source["telegram_poll_timeout"];
	        this.telegram_proxy_url = source["telegram_proxy_url"];
	        this.system_notification_mode = source["system_notification_mode"];
	        this.system_prompt = source["system_prompt"];
	    }
	}
	export class UpdateProjectAIConfigRequest {
	    project_id: string;
	    default_provider: string;
	    model: string;
	    system_prompt: string;
	    failure_policy: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateProjectAIConfigRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_id = source["project_id"];
	        this.default_provider = source["default_provider"];
	        this.model = source["model"];
	        this.system_prompt = source["system_prompt"];
	        this.failure_policy = source["failure_policy"];
	    }
	}
	export class UpdateProjectRequest {
	    project_id: string;
	    name: string;
	    default_provider: string;
	    model: string;
	    system_prompt: string;
	    failure_policy: string;
	    frontend_screenshot_report_enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdateProjectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_id = source["project_id"];
	        this.name = source["name"];
	        this.default_provider = source["default_provider"];
	        this.model = source["model"];
	        this.system_prompt = source["system_prompt"];
	        this.failure_policy = source["failure_policy"];
	        this.frontend_screenshot_report_enabled = source["frontend_screenshot_report_enabled"];
	    }
	}
	export class UpdateTaskRequest {
	    task_id: string;
	    title: string;
	    description: string;
	    priority: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateTaskRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.priority = source["priority"];
	    }
	}

}

export namespace domain {
	
	export class Agent {
	    ID: string;
	    Name: string;
	    Provider: string;
	    Enabled: boolean;
	    Concurrency: number;
	    // Go type: time
	    LastSeenAt?: any;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Agent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Provider = source["Provider"];
	        this.Enabled = source["Enabled"];
	        this.Concurrency = source["Concurrency"];
	        this.LastSeenAt = this.convertValues(source["LastSeenAt"], null);
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
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
	export class Project {
	    ID: string;
	    Name: string;
	    Path: string;
	    DefaultProvider: string;
	    Model: string;
	    SystemPrompt: string;
	    FailurePolicy: string;
	    AutoDispatchEnabled: boolean;
	    FrontendScreenshotReportEnabled: boolean;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Path = source["Path"];
	        this.DefaultProvider = source["DefaultProvider"];
	        this.Model = source["Model"];
	        this.SystemPrompt = source["SystemPrompt"];
	        this.FailurePolicy = source["FailurePolicy"];
	        this.AutoDispatchEnabled = source["AutoDispatchEnabled"];
	        this.FrontendScreenshotReportEnabled = source["FrontendScreenshotReportEnabled"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
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
	export class Task {
	    ID: string;
	    ProjectID: string;
	    ProjectName: string;
	    ProjectPath: string;
	    Model: string;
	    SystemPrompt: string;
	    Title: string;
	    Description: string;
	    Priority: number;
	    Status: string;
	    Provider: string;
	    RetryCount: number;
	    MaxRetries: number;
	    // Go type: time
	    NextRetryAt?: any;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.ProjectID = source["ProjectID"];
	        this.ProjectName = source["ProjectName"];
	        this.ProjectPath = source["ProjectPath"];
	        this.Model = source["Model"];
	        this.SystemPrompt = source["SystemPrompt"];
	        this.Title = source["Title"];
	        this.Description = source["Description"];
	        this.Priority = source["Priority"];
	        this.Status = source["Status"];
	        this.Provider = source["Provider"];
	        this.RetryCount = source["RetryCount"];
	        this.MaxRetries = source["MaxRetries"];
	        this.NextRetryAt = this.convertValues(source["NextRetryAt"], null);
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
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

