// Typed API client for the Go backend JSON endpoints.
// All requests include credentials (session cookie) automatically.

async function fetchJSON(path: string, opts?: RequestInit) {
	const res = await fetch('/api' + path, {
		credentials: 'include',
		headers: { 'Content-Type': 'application/json' },
		...opts
	});
	const data = await res.json();
	if (!res.ok) throw new Error(data.error || 'Request failed');
	return data;
}

export interface User {
	id: number;
	username: string;
	role: 'admin' | 'developer';
	totp_enabled: boolean;
	instance_id: string | null;
	created_at: string;
}

export interface AWSInstance {
	instance_id: string;
	state: string;
	public_ip: string;
	instance_type: string;
}

export interface DBInstance {
	instance_id: string;
	status: 'active' | 'idle' | 'unknown';
	last_heartbeat_at: string | null;
	last_active_at: string | null;
}

export interface DashboardData {
	aws_instance?: AWSInstance;
	db_instance?: DBInstance;
	aws_error?: string;
}

export interface AdminUser {
	id: number;
	username: string;
	role: 'admin' | 'developer';
	instance_id: string | null;
	created_at: string;
	totp_enabled: boolean;
}

export interface AdminInstance {
	instance_id: string;
	status: string;
	last_heartbeat_at: string | null;
	last_active_at: string | null;
}

export interface AuditLog {
	timestamp: string;
	username: string;
	action: string;
	instance_id: string | null;
	metadata: string | null;
}

export interface AdminData {
	users: AdminUser[];
	instances: AdminInstance[];
	logs: AuditLog[];
}

export const api = {
	// Auth
	me: (): Promise<User> => fetchJSON('/me'),
	login: (username: string, pin: string): Promise<{ role?: string; mfa_required?: boolean }> =>
		fetchJSON('/login', { method: 'POST', body: JSON.stringify({ username, pin }) }),
	logout: () => fetchJSON('/logout', { method: 'POST' }),

	// MFA
	mfaVerify: (code: string): Promise<{ role: string }> =>
		fetchJSON('/mfa/verify', { method: 'POST', body: JSON.stringify({ code }) }),
	mfaSetupGet: (): Promise<{ qr_code: string; secret: string }> => fetchJSON('/mfa/setup'),
	mfaSetupActivate: (code: string): Promise<{ message: string }> =>
		fetchJSON('/mfa/setup', { method: 'POST', body: JSON.stringify({ code }) }),
	mfaDisable: (code: string): Promise<{ message: string }> =>
		fetchJSON('/mfa/disable', { method: 'POST', body: JSON.stringify({ code }) }),

	// Developer
	dashboard: (): Promise<DashboardData> => fetchJSON('/dashboard'),
	startInstance: (): Promise<{ message: string }> =>
		fetchJSON('/start-instance', { method: 'POST' }),
	stopInstance: (): Promise<{ message: string }> =>
		fetchJSON('/stop-instance', { method: 'POST' }),

	// Admin
	admin: (): Promise<AdminData> => fetchJSON('/admin/'),
	appLogs: (): Promise<{ lines: string[] }> => fetchJSON('/admin/app-logs'),
	addUser: (username: string, pin: string, role: string): Promise<{ message: string }> =>
		fetchJSON('/admin/users', { method: 'POST', body: JSON.stringify({ username, pin, role }) }),
	assignInstance: (userId: number, instanceId: string): Promise<{ message: string }> =>
		fetchJSON(`/admin/users/${userId}/assign`, {
			method: 'POST',
			body: JSON.stringify({ instance_id: instanceId })
		}),
	resetPin: (userId: number, newPin: string): Promise<{ message: string }> =>
		fetchJSON(`/admin/users/${userId}/reset-pin`, {
			method: 'POST',
			body: JSON.stringify({ new_pin: newPin })
		}),
	resetMfa: (userId: number): Promise<{ message: string }> =>
		fetchJSON(`/admin/users/${userId}/reset-mfa`, { method: 'POST', body: '{}' }),
	deleteUser: (userId: number): Promise<{ message: string }> =>
		fetchJSON(`/admin/users/${userId}/delete`, { method: 'POST', body: '{}' }),
	provisionWorkspace: (userId: number): Promise<{ message: string }> =>
		fetchJSON(`/admin/users/${userId}/provision`, { method: 'POST', body: '{}' })
};
