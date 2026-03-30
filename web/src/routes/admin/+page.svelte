<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, type User, type AdminData, type AdminUser } from '$lib/api';
	import Nav from '$lib/components/Nav.svelte';

	let user = $state<User | null>(null);
	let data = $state<AdminData>({ users: [], instances: [], logs: [] });
	let pageLoading = $state(true);
	let error = $state('');
	let success = $state('');
	let activeTab = $state('users');

	// ── Modals ────────────────────────────────────────────────
	interface ModalState { open: boolean; userId: number; username: string }
	let assignModal = $state<ModalState>({ open: false, userId: 0, username: '' });
	let resetPinModal = $state<ModalState>({ open: false, userId: 0, username: '' });
	let deleteModal = $state<ModalState>({ open: false, userId: 0, username: '' });
	let provisionModal = $state<ModalState>({ open: false, userId: 0, username: '' });
	let resetMfaModal = $state<ModalState>({ open: false, userId: 0, username: '' });
	let disableMfaModal = $state(false);

	// ── Form values ───────────────────────────────────────────
	let assignInstanceId = $state('');
	let resetPinValue = $state('');
	let mfaCode = $state('');
	let newUsername = $state('');
	let newPin = $state('');
	let newRole = $state('developer');
	let provisionDevPassword = $state('');
	let provisionGuardPassword = $state('');

	// ── Loading states ────────────────────────────────────────
	let actionLoading = $state(false);
	let provisioning = $state(false);
	let appLogs = $state<string[]>([]);
	let appLogsLoading = $state(false);

	onMount(async () => {
		try {
			user = await api.me();
			if (user.role !== 'admin') {
				goto('/dashboard');
				return;
			}
			await loadData();
		} catch {
			goto('/login');
			return;
		}
		pageLoading = false;
	});

	async function loadData() {
		const d = await api.admin();
		data = d;
	}

	function notify(msg: string, isError = false) {
		if (isError) { error = msg; success = ''; }
		else { success = msg; error = ''; }
		setTimeout(() => { error = ''; success = ''; }, 6000);
	}

	// ── Actions ───────────────────────────────────────────────
	async function addUser(e: Event) {
		e.preventDefault();
		actionLoading = true;
		try {
			const res = await api.addUser(newUsername, newPin, newRole);
			notify(res.message);
			newUsername = ''; newPin = ''; newRole = 'developer';
			await loadData();
		} catch (err) {
			notify(err instanceof Error ? err.message : 'Failed to create user.', true);
		} finally {
			actionLoading = false;
		}
	}

	async function assignInstance(e: Event) {
		e.preventDefault();
		actionLoading = true;
		try {
			const res = await api.assignInstance(assignModal.userId, assignInstanceId);
			notify(res.message);
			assignModal.open = false;
			assignInstanceId = '';
			await loadData();
		} catch (err) {
			notify(err instanceof Error ? err.message : 'Failed to assign instance.', true);
		} finally {
			actionLoading = false;
		}
	}

	async function resetPin(e: Event) {
		e.preventDefault();
		actionLoading = true;
		try {
			const res = await api.resetPin(resetPinModal.userId, resetPinValue);
			notify(res.message);
			resetPinModal.open = false;
			resetPinValue = '';
			await loadData();
		} catch (err) {
			notify(err instanceof Error ? err.message : 'Failed to reset PIN.', true);
		} finally {
			actionLoading = false;
		}
	}

	async function deleteUser(e: Event) {
		e.preventDefault();
		actionLoading = true;
		try {
			const res = await api.deleteUser(deleteModal.userId);
			notify(res.message);
			deleteModal.open = false;
			await loadData();
		} catch (err) {
			notify(err instanceof Error ? err.message : 'Failed to delete user.', true);
		} finally {
			actionLoading = false;
		}
	}

	async function provisionWorkspace(e: Event) {
		e.preventDefault();
		if (!provisionDevPassword || !provisionGuardPassword) {
			notify('Both passwords are required before provisioning.', true);
			return;
		}
		provisioning = true;
		provisionModal.open = false;
		const devPwd = provisionDevPassword;
		const guardPwd = provisionGuardPassword;
		provisionDevPassword = '';
		provisionGuardPassword = '';
		try {
			const res = await api.provisionWorkspace(provisionModal.userId, devPwd, guardPwd);
			notify(res.message);
			await loadData();
		} catch (err) {
			notify(err instanceof Error ? err.message : 'Provisioning failed.', true);
		} finally {
			provisioning = false;
		}
	}

	async function resetMfa(e: Event) {
		e.preventDefault();
		actionLoading = true;
		try {
			const res = await api.resetMfa(resetMfaModal.userId);
			notify(res.message);
			resetMfaModal.open = false;
			await loadData();
		} catch (err) {
			notify(err instanceof Error ? err.message : 'Failed to reset MFA.', true);
		} finally {
			actionLoading = false;
		}
	}

	async function disableOwnMfa(e: Event) {
		e.preventDefault();
		actionLoading = true;
		try {
			await api.mfaDisable(mfaCode);
			disableMfaModal = false;
			mfaCode = '';
			notify('MFA disabled successfully.');
			user = await api.me();
		} catch (err) {
			notify(err instanceof Error ? err.message : 'Failed to disable MFA.', true);
		} finally {
			actionLoading = false;
		}
	}

	async function refreshAppLogs() {
		appLogsLoading = true;
		try {
			const res = await api.appLogs();
			appLogs = res.lines || [];
		} catch {
			appLogs = ['Error loading logs.'];
		} finally {
			appLogsLoading = false;
		}
	}

	function actionBadge(action: string) {
		const map: Record<string, string> = {
			START: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
			STOP: 'bg-forest-800 text-forest-300 border-forest-700',
			AUTO_STOP: 'bg-amber-500/15 text-amber-400 border-amber-500/30',
			LOGIN: 'bg-forest-700 text-forest-100 border-forest-600',
			LOGIN_FAIL: 'bg-red-500/15 text-red-400 border-red-500/30',
			LOGOUT: 'bg-forest-800 text-forest-300 border-forest-700',
			HEARTBEAT: 'bg-cyan-500/15 text-cyan-400 border-cyan-500/30',
			PROVISION: 'bg-blue-500/15 text-blue-400 border-blue-500/30'
		};
		return map[action] ?? 'bg-forest-800 text-forest-400 border-forest-700';
	}

	function renderLogLine(raw: string) {
		const levelColors: Record<string, string> = {
			DEBUG: 'text-forest-500',
			INFO: 'text-sky-400',
			WARN: 'text-amber-400',
			ERROR: 'text-red-400'
		};
		try {
			const entry = JSON.parse(raw);
			const level = (entry.level || 'INFO').toUpperCase();
			const color = levelColors[level] ?? 'text-forest-400';
			const time = (entry.time || '').replace('T', ' ').replace(/\.\d+Z$/, '');
			const msg = entry.msg || '';
			const extras = Object.keys(entry)
				.filter((k) => k !== 'time' && k !== 'level' && k !== 'msg')
				.map((k) => `${k}=${JSON.stringify(entry[k])}`)
				.join(' ');
			return { time, level, color, msg, extras };
		} catch {
			return { time: '', level: '', color: 'text-forest-400', msg: raw, extras: '' };
		}
	}
</script>

{#if !pageLoading && user}
	<Nav {user} />
{/if}

<main class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
	{#if pageLoading}
		<div class="flex justify-center py-20">
			<svg class="animate-spin h-6 w-6 text-forest-400" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
				<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
				<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
			</svg>
		</div>
	{:else}
		<!-- Page header -->
		<div class="mb-6">
			<h1 class="text-xl font-bold text-forest-50 tracking-tight">Admin Dashboard</h1>
			<p class="text-sm text-forest-300 mt-0.5">Manage users, instances, and audit logs</p>
		</div>

		{#if error}
			<div class="mb-5 flex items-start gap-2.5 p-3.5 bg-red-500/10 border border-red-500/30 rounded-xl">
				<i class="bi bi-exclamation-circle-fill text-red-400 mt-0.5 shrink-0"></i>
				<p class="text-red-400 text-sm">{error}</p>
			</div>
		{/if}

		{#if success}
			<div class="mb-5 flex items-start gap-2.5 p-3.5 bg-emerald-500/10 border border-emerald-500/30 rounded-xl">
				<i class="bi bi-check-circle-fill text-emerald-400 mt-0.5 shrink-0"></i>
				<p class="text-emerald-400 text-sm">{success}</p>
			</div>
		{/if}

		{#if provisioning}
			<div class="mb-5 flex items-start gap-2.5 p-3.5 bg-blue-500/10 border border-blue-500/30 rounded-xl">
				<svg class="animate-spin h-4 w-4 text-blue-400 mt-0.5 shrink-0" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
					<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
					<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
				</svg>
				<p class="text-blue-400 text-sm">Provisioning workspace… This may take up to 5 minutes. Please wait.</p>
			</div>
		{/if}

		<!-- Admin MFA Card -->
		{#if user}
			<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30 px-5 py-4 flex items-center justify-between mb-6">
				<div class="flex items-center gap-3">
					<div class="w-9 h-9 bg-forest-700 rounded-xl flex items-center justify-center shrink-0 ring-1 ring-forest-500/30">
						<i class="bi bi-shield-lock text-forest-300"></i>
					</div>
					<div>
						<p class="text-sm font-semibold text-forest-100">Your Two-Factor Authentication</p>
						{#if user.totp_enabled}
							<p class="text-xs text-emerald-400 font-medium mt-0.5"><i class="bi bi-check-circle-fill mr-1"></i>Enabled</p>
						{:else}
							<p class="text-xs text-forest-400 mt-0.5">Not enabled — your admin account has no MFA protection</p>
						{/if}
					</div>
				</div>
				{#if user.totp_enabled}
					<button onclick={() => { disableMfaModal = true; mfaCode = ''; }}
						class="px-3 py-1.5 text-xs font-medium text-red-400 hover:bg-red-500/10 rounded-lg transition-colors cursor-pointer border border-red-500/30">
						<i class="bi bi-shield-x mr-1"></i>Disable
					</button>
				{:else}
					<a href="/mfa/setup"
						class="px-3 py-1.5 text-xs font-medium text-white bg-forest-400 hover:bg-forest-500 rounded-lg transition-colors shadow-sm">
						<i class="bi bi-shield-plus mr-1"></i>Set up MFA
					</a>
				{/if}
			</div>
		{/if}

		<!-- Tab navigation -->
		<div class="flex gap-1 bg-forest-800 p-1 rounded-xl mb-6 w-fit border border-forest-700">
			{#each [
				{ id: 'users', icon: 'bi-people', label: 'Users', count: data.users.length },
				{ id: 'instances', icon: 'bi-hdd-network', label: 'Instances', count: null },
				{ id: 'logs', icon: 'bi-journal-text', label: 'Audit Logs', count: null },
				{ id: 'applogs', icon: 'bi-terminal', label: 'App Logs', count: null },
				{ id: 'add-user', icon: 'bi-person-plus', label: 'Add User', count: null }
			] as tab}
				<button
					onclick={() => { activeTab = tab.id; if (tab.id === 'applogs' && appLogs.length === 0) refreshAppLogs(); }}
					class="px-4 py-2 text-sm font-medium rounded-lg transition-all {activeTab === tab.id ? 'bg-forest-600 shadow-sm text-forest-50' : 'text-forest-400 hover:text-forest-100 hover:bg-forest-700'}"
				>
					<i class="bi {tab.icon} mr-1.5"></i>{tab.label}
					{#if tab.count !== null}
						<span class="ml-1.5 px-1.5 py-0.5 bg-forest-400/20 text-forest-300 text-xs rounded-md font-semibold">{tab.count}</span>
					{/if}
				</button>
			{/each}
		</div>

		<!-- ── Users Tab ──────────────────────────────────────── -->
		{#if activeTab === 'users'}
			<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30 overflow-hidden">
				<div class="px-5 py-4 border-b border-forest-700 flex items-center justify-between">
					<h2 class="text-sm font-semibold text-forest-100">All Users</h2>
					<span class="text-xs text-forest-400">{data.users.length} total</span>
				</div>
				<div class="overflow-x-auto">
					<table class="w-full text-sm">
						<thead>
							<tr class="border-b border-forest-700 bg-forest-800/70">
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">User</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Role</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Instance ID</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Workspace Credentials</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Created</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">MFA</th>
								<th class="text-right px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Actions</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-forest-800">
							{#if data.users.length === 0}
								<tr><td colspan="7" class="px-5 py-12 text-center text-sm text-forest-400">No users found.</td></tr>
							{/if}
							{#each data.users as u (u.id)}
								<tr class="hover:bg-forest-800/50 transition-colors">
									<td class="px-5 py-3.5">
										<div class="flex items-center gap-2.5">
											<div class="w-7 h-7 bg-forest-700 rounded-full flex items-center justify-center shrink-0 ring-1 ring-forest-500/30">
												<i class="bi bi-person text-forest-300" style="font-size:0.7rem"></i>
											</div>
											<span class="font-medium text-forest-100">{u.username}</span>
										</div>
									</td>
									<td class="px-5 py-3.5">
										{#if u.role === 'admin'}
											<span class="px-2 py-0.5 bg-amber-500/15 text-amber-400 text-xs rounded-md font-medium border border-amber-500/30">Admin</span>
										{:else}
											<span class="px-2 py-0.5 bg-forest-700 text-forest-300 text-xs rounded-md font-medium border border-forest-600">Developer</span>
										{/if}
									</td>
									<td class="px-5 py-3.5 font-mono text-xs text-forest-300">
										{#if u.instance_id}
											<span class="px-2 py-1 bg-forest-800 rounded-md border border-forest-700">{u.instance_id}</span>
										{:else}
											<span class="text-forest-600">Not assigned</span>
										{/if}
									</td>
									<td class="px-5 py-3.5 text-xs">
										{#if u.workspace_password || u.workspace_guard_password}
											<div class="space-y-1.5">
												{#if u.workspace_password}
													<div class="flex items-center gap-1.5">
														<span class="text-forest-500 shrink-0">Dev:</span>
														<code class="px-1.5 py-0.5 bg-forest-800 border border-forest-700 rounded text-forest-200 font-mono select-all">{u.workspace_password}</code>
													</div>
												{/if}
												{#if u.workspace_guard_password}
													<div class="flex items-center gap-1.5">
														<span class="text-forest-500 shrink-0">Guard:</span>
														<code class="px-1.5 py-0.5 bg-amber-500/10 border border-amber-500/30 rounded text-amber-300 font-mono select-all">{u.workspace_guard_password}</code>
													</div>
												{/if}
											</div>
										{:else}
											<span class="text-forest-600">—</span>
										{/if}
									</td>
									<td class="px-5 py-3.5 text-xs text-forest-400">{u.created_at}</td>
									<td class="px-5 py-3.5">
										{#if u.totp_enabled}
											<span class="inline-flex items-center gap-1 px-2 py-0.5 bg-emerald-500/15 text-emerald-400 text-xs rounded-md font-medium border border-emerald-500/30">
												<i class="bi bi-shield-check"></i>On
											</span>
										{:else}
											<span class="px-2 py-0.5 bg-forest-800 text-forest-500 text-xs rounded-md font-medium border border-forest-700">Off</span>
										{/if}
									</td>
									<td class="px-5 py-3.5">
										<div class="flex items-center justify-end gap-1">
											<button
												onclick={() => { assignModal = { open: true, userId: u.id, username: u.username }; assignInstanceId = ''; }}
												class="px-2.5 py-1.5 text-xs font-medium text-forest-300 hover:text-forest-50 hover:bg-forest-700 rounded-lg transition-colors cursor-pointer">
												<i class="bi bi-link-45deg mr-1"></i>Assign
											</button>
											{#if u.role === 'developer' && !u.instance_id}
												<button
													onclick={() => { provisionModal = { open: true, userId: u.id, username: u.username }; provisionDevPassword = ''; provisionGuardPassword = ''; }}
													class="px-2.5 py-1.5 text-xs font-medium text-emerald-400 hover:text-emerald-300 hover:bg-emerald-500/10 rounded-lg transition-colors cursor-pointer">
													<i class="bi bi-plus-circle mr-1"></i>Provision
												</button>
											{/if}
											<button
												onclick={() => { resetPinModal = { open: true, userId: u.id, username: u.username }; resetPinValue = ''; }}
												class="px-2.5 py-1.5 text-xs font-medium text-amber-400 hover:text-amber-300 hover:bg-amber-500/10 rounded-lg transition-colors cursor-pointer">
												<i class="bi bi-key mr-1"></i>Reset PIN
											</button>
											{#if u.totp_enabled}
												<button
													onclick={() => resetMfaModal = { open: true, userId: u.id, username: u.username }}
													class="px-2.5 py-1.5 text-xs font-medium text-orange-400 hover:text-orange-300 hover:bg-orange-500/10 rounded-lg transition-colors cursor-pointer">
													<i class="bi bi-shield-x mr-1"></i>Reset MFA
												</button>
											{/if}
											{#if u.role !== 'admin'}
												<button
													onclick={() => deleteModal = { open: true, userId: u.id, username: u.username }}
													class="px-2.5 py-1.5 text-xs font-medium text-red-400 hover:text-red-300 hover:bg-red-500/10 rounded-lg transition-colors cursor-pointer">
													<i class="bi bi-trash mr-1"></i>Delete
												</button>
											{/if}
										</div>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}

		<!-- ── Instances Tab ──────────────────────────────────── -->
		{#if activeTab === 'instances'}
			<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30 overflow-hidden">
				<div class="px-5 py-4 border-b border-forest-700">
					<h2 class="text-sm font-semibold text-forest-100">Instance Heartbeat Status</h2>
					<p class="text-xs text-forest-400 mt-0.5">Updated every 10 minutes from running desktop instances</p>
				</div>
				<div class="overflow-x-auto">
					<table class="w-full text-sm">
						<thead>
							<tr class="border-b border-forest-700 bg-forest-800/70">
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Instance ID</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Status</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Last Heartbeat</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Last Active</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-forest-800">
							{#if data.instances.length === 0}
								<tr><td colspan="4" class="px-5 py-12 text-center text-sm text-forest-400">No heartbeat data yet.</td></tr>
							{/if}
							{#each data.instances as inst (inst.instance_id)}
								<tr class="hover:bg-forest-800/50 transition-colors">
									<td class="px-5 py-3.5 font-mono text-xs text-forest-300">{inst.instance_id}</td>
									<td class="px-5 py-3.5">
										{#if inst.status === 'active'}
											<span class="inline-flex items-center gap-1.5 px-2.5 py-1 bg-emerald-500/15 text-emerald-400 text-xs font-semibold rounded-full border border-emerald-500/30">
												<span class="w-1.5 h-1.5 bg-emerald-500 rounded-full animate-pulse"></span>Active
											</span>
										{:else if inst.status === 'idle'}
											<span class="inline-flex items-center gap-1.5 px-2.5 py-1 bg-amber-500/15 text-amber-400 text-xs font-semibold rounded-full border border-amber-500/30">
												<i class="bi bi-moon text-xs"></i>Idle
											</span>
										{:else}
											<span class="inline-flex items-center gap-1.5 px-2.5 py-1 bg-forest-800 text-forest-400 text-xs font-semibold rounded-full border border-forest-700">Unknown</span>
										{/if}
									</td>
									<td class="px-5 py-3.5 text-xs text-forest-400">{inst.last_heartbeat_at ?? '—'}</td>
									<td class="px-5 py-3.5 text-xs text-forest-400">{inst.last_active_at ?? '—'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}

		<!-- ── Audit Logs Tab ─────────────────────────────────── -->
		{#if activeTab === 'logs'}
			<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30 overflow-hidden">
				<div class="px-5 py-4 border-b border-forest-700 flex items-center justify-between">
					<h2 class="text-sm font-semibold text-forest-100">Audit Logs</h2>
					<span class="text-xs text-forest-400">Last 100 entries</span>
				</div>
				<div class="overflow-x-auto">
					<table class="w-full text-sm">
						<thead>
							<tr class="border-b border-forest-700 bg-forest-800/70">
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Time</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">User</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Action</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Instance</th>
								<th class="text-left px-5 py-3 text-xs font-semibold text-forest-400 uppercase tracking-wider">Metadata</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-forest-800">
							{#if data.logs.length === 0}
								<tr><td colspan="5" class="px-5 py-12 text-center text-sm text-forest-400">No log entries yet.</td></tr>
							{/if}
							{#each data.logs as log, i (i)}
								<tr class="hover:bg-forest-800/50 transition-colors">
									<td class="px-5 py-3 text-xs text-forest-400 font-mono whitespace-nowrap">{log.timestamp}</td>
									<td class="px-5 py-3 text-xs font-medium text-forest-100">{log.username}</td>
									<td class="px-5 py-3">
										<span class="px-2 py-0.5 text-xs rounded-md font-semibold border {actionBadge(log.action)}">{log.action}</span>
									</td>
									<td class="px-5 py-3 font-mono text-xs text-forest-400">{log.instance_id ?? '—'}</td>
									<td class="px-5 py-3 text-xs text-forest-500 max-w-xs truncate">{log.metadata ?? '—'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</div>
		{/if}

		<!-- ── App Logs Tab ───────────────────────────────────── -->
		{#if activeTab === 'applogs'}
			<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30 overflow-hidden">
				<div class="px-5 py-4 border-b border-forest-700 flex items-center justify-between">
					<div>
						<h2 class="text-sm font-semibold text-forest-100">Application Logs</h2>
						<p class="text-xs text-forest-400 mt-0.5">Last 200 lines from app.log</p>
					</div>
					<button onclick={refreshAppLogs} disabled={appLogsLoading}
						class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-white bg-forest-400 hover:bg-forest-500 rounded-lg transition-colors shadow-sm cursor-pointer disabled:opacity-60">
						<i class="bi bi-arrow-clockwise {appLogsLoading ? 'animate-spin' : ''}"></i>Refresh
					</button>
				</div>
				<div class="p-4 bg-forest-950 rounded-b-2xl min-h-48 max-h-[600px] overflow-y-auto">
					{#if appLogs.length === 0}
						<p class="text-forest-600 text-xs font-mono">Click Refresh to load application logs.</p>
					{:else}
						{#each appLogs as raw}
							{@const line = renderLogLine(raw)}
							<div class="font-mono text-xs leading-5 flex gap-2">
								<span class="text-forest-600 shrink-0">{line.time}</span>
								{#if line.level}
									<span class="font-semibold shrink-0 w-12 {line.color}">{line.level}</span>
								{/if}
								<span class="text-forest-100">{line.msg}</span>
								{#if line.extras}
									<span class="text-forest-500">{line.extras}</span>
								{/if}
							</div>
						{/each}
					{/if}
				</div>
			</div>
		{/if}

		<!-- ── Add User Tab ───────────────────────────────────── -->
		{#if activeTab === 'add-user'}
			<div class="max-w-sm">
				<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30 overflow-hidden">
					<div class="px-5 py-4 border-b border-forest-700">
						<h2 class="text-sm font-semibold text-forest-100">Create New User</h2>
						<p class="text-xs text-forest-400 mt-0.5">The user can log in immediately after creation</p>
					</div>
					<form onsubmit={addUser} autocomplete="off" novalidate class="p-5 space-y-4">
						<div>
							<label for="new-username" class="block text-sm font-medium text-forest-100 mb-1.5">Username</label>
							<input type="text" id="new-username" bind:value={newUsername}
								class="w-full px-3 py-2.5 text-sm border border-forest-600 rounded-xl bg-forest-800 text-forest-50 focus:outline-none focus:ring-2 focus:ring-forest-400 focus:border-transparent transition"
								placeholder="jane.doe" required maxlength="64" />
						</div>
						<div>
							<label for="new-pin" class="block text-sm font-medium text-forest-100 mb-1.5">PIN / Password</label>
							<input type="password" id="new-pin" bind:value={newPin}
								class="w-full px-3 py-2.5 text-sm border border-forest-600 rounded-xl bg-forest-800 text-forest-50 focus:outline-none focus:ring-2 focus:ring-forest-400 focus:border-transparent transition"
								placeholder="Minimum 4 characters" required minlength="4" maxlength="128" />
						</div>
						<div>
							<label for="new-role" class="block text-sm font-medium text-forest-100 mb-1.5">Role</label>
							<select id="new-role" bind:value={newRole}
								class="w-full px-3 py-2.5 text-sm border border-forest-600 rounded-xl bg-forest-800 text-forest-50 focus:outline-none focus:ring-2 focus:ring-forest-400 focus:border-transparent transition">
								<option value="developer">Developer</option>
								<option value="admin">Admin</option>
							</select>
						</div>
						<button type="submit" disabled={actionLoading}
							class="w-full flex items-center justify-center gap-2 bg-forest-400 hover:bg-forest-500 disabled:opacity-60 text-white font-medium py-2.5 px-4 rounded-xl text-sm transition-colors shadow-sm cursor-pointer">
							{#if actionLoading}
								<svg class="animate-spin h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
								</svg>
							{:else}
								<i class="bi bi-person-check"></i>
							{/if}
							Create User
						</button>
					</form>
				</div>
			</div>
		{/if}

	{/if}
</main>

<footer class="border-t border-forest-800 mt-12">
	<div class="max-w-7xl mx-auto px-4 py-4 text-center text-xs text-forest-500"></div>
</footer>

<!-- ═══════════ Modals ═══════════ -->

<!-- Assign Instance Modal -->
{#if assignModal.open}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="absolute inset-0 bg-black/60 backdrop-blur-sm" onclick={() => (assignModal.open = false)}></div>
		<div class="relative bg-forest-900 rounded-2xl shadow-2xl shadow-forest-950/80 w-full max-w-md border border-forest-600">
			<div class="flex items-center justify-between px-6 py-4 border-b border-forest-700">
				<div>
					<h3 class="text-base font-semibold text-forest-50">Assign EC2 Instance</h3>
					<p class="text-xs text-forest-400 mt-0.5">Assigning to: <span class="font-medium text-forest-200">{assignModal.username}</span></p>
				</div>
				<button class="text-forest-400 hover:text-forest-100 cursor-pointer" onclick={() => (assignModal.open = false)}>
					<i class="bi bi-x-lg text-sm"></i>
				</button>
			</div>
			<form onsubmit={assignInstance}>
				<div class="px-6 py-5">
					<label for="instance_id" class="block text-sm font-medium text-forest-100 mb-1.5">EC2 Instance ID</label>
					<input type="text" id="instance_id" bind:value={assignInstanceId}
						class="w-full px-3 py-2.5 text-sm font-mono border border-forest-600 rounded-xl bg-forest-800 text-forest-50 focus:outline-none focus:ring-2 focus:ring-forest-400 focus:border-transparent transition"
						placeholder="i-0123456789abcdef0" required maxlength="64" />
					<p class="text-xs text-forest-500 mt-1.5">Format: i-xxxxxxxxxxxxxxxxx</p>
				</div>
				<div class="flex gap-2.5 px-6 pb-5">
					<button type="button" onclick={() => (assignModal.open = false)}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-forest-100 bg-forest-800 hover:bg-forest-700 rounded-xl transition-colors cursor-pointer">Cancel</button>
					<button type="submit" disabled={actionLoading}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-white bg-forest-400 hover:bg-forest-500 disabled:opacity-60 rounded-xl transition-colors shadow-sm cursor-pointer">
						<i class="bi bi-link-45deg mr-1"></i>Assign
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}

<!-- Reset PIN Modal -->
{#if resetPinModal.open}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="absolute inset-0 bg-black/60 backdrop-blur-sm" onclick={() => (resetPinModal.open = false)}></div>
		<div class="relative bg-forest-900 rounded-2xl shadow-2xl shadow-forest-950/80 w-full max-w-md border border-forest-600">
			<div class="flex items-center justify-between px-6 py-4 border-b border-forest-700">
				<div>
					<h3 class="text-base font-semibold text-forest-50">Reset PIN</h3>
					<p class="text-xs text-forest-400 mt-0.5">Resetting for: <span class="font-medium text-forest-200">{resetPinModal.username}</span></p>
				</div>
				<button class="text-forest-400 hover:text-forest-100 cursor-pointer" onclick={() => (resetPinModal.open = false)}>
					<i class="bi bi-x-lg text-sm"></i>
				</button>
			</div>
			<form onsubmit={resetPin}>
				<div class="px-6 py-5 space-y-4">
					<div class="flex items-start gap-2.5 p-3 bg-amber-500/10 border border-amber-500/30 rounded-xl">
						<i class="bi bi-exclamation-triangle text-amber-400 shrink-0 mt-0.5"></i>
						<p class="text-xs text-amber-400">The user's active sessions will be invalidated and they will need to log in again.</p>
					</div>
					<div>
						<label for="new_pin" class="block text-sm font-medium text-forest-100 mb-1.5">New PIN</label>
						<input type="password" id="new_pin" bind:value={resetPinValue}
							class="w-full px-3 py-2.5 text-sm border border-forest-600 rounded-xl bg-forest-800 text-forest-50 focus:outline-none focus:ring-2 focus:ring-amber-500 focus:border-transparent transition"
							placeholder="Minimum 4 characters" required minlength="4" maxlength="128" />
					</div>
				</div>
				<div class="flex gap-2.5 px-6 pb-5">
					<button type="button" onclick={() => (resetPinModal.open = false)}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-forest-100 bg-forest-800 hover:bg-forest-700 rounded-xl transition-colors cursor-pointer">Cancel</button>
					<button type="submit" disabled={actionLoading}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-white bg-amber-500 hover:bg-amber-600 disabled:opacity-60 rounded-xl transition-colors shadow-sm cursor-pointer">
						<i class="bi bi-key mr-1"></i>Reset PIN
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}

<!-- Delete User Modal -->
{#if deleteModal.open}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="absolute inset-0 bg-black/60 backdrop-blur-sm" onclick={() => (deleteModal.open = false)}></div>
		<div class="relative bg-forest-900 rounded-2xl shadow-2xl shadow-forest-950/80 w-full max-w-md border border-forest-600">
			<div class="flex items-center justify-between px-6 py-4 border-b border-forest-700">
				<div class="flex items-center gap-3">
					<div class="w-9 h-9 bg-red-500/15 rounded-xl flex items-center justify-center ring-1 ring-red-500/30">
						<i class="bi bi-trash text-red-400"></i>
					</div>
					<h3 class="text-base font-semibold text-forest-50">Delete User</h3>
				</div>
				<button class="text-forest-400 hover:text-forest-100 cursor-pointer" onclick={() => (deleteModal.open = false)}>
					<i class="bi bi-x-lg text-sm"></i>
				</button>
			</div>
			<form onsubmit={deleteUser}>
				<div class="px-6 py-5">
					<p class="text-sm text-forest-300">Are you sure you want to delete <strong class="text-forest-50">{deleteModal.username}</strong>?</p>
					<p class="text-xs text-forest-500 mt-2">This will permanently remove the account and all their sessions. This action cannot be undone.</p>
				</div>
				<div class="flex gap-2.5 px-6 pb-5">
					<button type="button" onclick={() => (deleteModal.open = false)}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-forest-100 bg-forest-800 hover:bg-forest-700 rounded-xl transition-colors cursor-pointer">Cancel</button>
					<button type="submit" disabled={actionLoading}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-white bg-red-600 hover:bg-red-700 disabled:opacity-60 rounded-xl transition-colors shadow-sm cursor-pointer">
						<i class="bi bi-trash mr-1"></i>Delete
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}

<!-- Provision Workspace Modal -->
{#if provisionModal.open}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="absolute inset-0 bg-black/60 backdrop-blur-sm" onclick={() => (provisionModal.open = false)}></div>
		<div class="relative bg-forest-900 rounded-2xl shadow-2xl shadow-forest-950/80 w-full max-w-md border border-forest-600">
			<div class="flex items-center justify-between px-6 py-4 border-b border-forest-700">
				<div class="flex items-center gap-3">
					<div class="w-9 h-9 bg-emerald-500/15 rounded-xl flex items-center justify-center ring-1 ring-emerald-500/30">
						<i class="bi bi-plus-circle text-emerald-400"></i>
					</div>
					<div>
						<h3 class="text-base font-semibold text-forest-50">Provision Workspace</h3>
						<p class="text-xs text-forest-400 mt-0.5">For: <span class="font-medium text-forest-200">{provisionModal.username}</span></p>
					</div>
				</div>
				<button class="text-forest-400 hover:text-forest-100 cursor-pointer" onclick={() => (provisionModal.open = false)}>
					<i class="bi bi-x-lg text-sm"></i>
				</button>
			</div>
			<form onsubmit={provisionWorkspace}>
				<div class="px-6 py-5 space-y-3">
					<div class="flex items-start gap-2.5 p-3 bg-emerald-500/10 border border-emerald-500/30 rounded-xl">
						<i class="bi bi-info-circle text-emerald-400 shrink-0 mt-0.5"></i>
						<p class="text-xs text-emerald-400">A new EC2 instance will be launched from the configured AMI, an Elastic IP will be allocated and associated, and the instance will be assigned to this developer automatically.</p>
					</div>
					<div>
						<label class="block text-xs font-medium text-forest-300 mb-1.5">
							Developer Password
							<span class="text-forest-500 font-normal ml-1">— set on the Linux user account</span>
						</label>
						<input
							type="text"
							bind:value={provisionDevPassword}
							placeholder="Enter developer password"
							required
							class="w-full px-3 py-2 bg-forest-800 border border-forest-600 rounded-xl text-sm text-forest-100 placeholder-forest-600 focus:outline-none focus:ring-2 focus:ring-emerald-500/50 focus:border-emerald-500/50 font-mono"
						/>
					</div>
					<div>
						<label class="block text-xs font-medium text-forest-300 mb-1.5">
							Guard Password
							<span class="text-forest-500 font-normal ml-1">— special password for dnsmasq access, separate from above</span>
						</label>
						<input
							type="text"
							bind:value={provisionGuardPassword}
							placeholder="Enter guard password"
							required
							class="w-full px-3 py-2 bg-forest-800 border border-forest-600 rounded-xl text-sm text-forest-100 placeholder-forest-600 focus:outline-none focus:ring-2 focus:ring-amber-500/50 focus:border-amber-500/50 font-mono"
						/>
					</div>
					<div class="flex items-start gap-2.5 p-3 bg-amber-500/10 border border-amber-500/30 rounded-xl">
						<i class="bi bi-exclamation-triangle text-amber-400 shrink-0 mt-0.5"></i>
						<p class="text-xs text-amber-400">This will incur AWS costs. The operation may take up to 5 minutes to complete. Both passwords will be stored in plain text and visible on this page.</p>
					</div>
				</div>
				<div class="flex gap-2.5 px-6 pb-5">
					<button type="button" onclick={() => (provisionModal.open = false)}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-forest-100 bg-forest-800 hover:bg-forest-700 rounded-xl transition-colors cursor-pointer">Cancel</button>
					<button type="submit" disabled={!provisionDevPassword || !provisionGuardPassword}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-white bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 rounded-xl transition-colors shadow-sm cursor-pointer">
						<i class="bi bi-plus-circle mr-1"></i>Provision
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}

<!-- Reset MFA Modal -->
{#if resetMfaModal.open}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="absolute inset-0 bg-black/60 backdrop-blur-sm" onclick={() => (resetMfaModal.open = false)}></div>
		<div class="relative bg-forest-900 rounded-2xl shadow-2xl shadow-forest-950/80 w-full max-w-md border border-forest-600">
			<div class="flex items-center justify-between px-6 py-4 border-b border-forest-700">
				<div class="flex items-center gap-3">
					<div class="w-9 h-9 bg-orange-500/15 rounded-xl flex items-center justify-center ring-1 ring-orange-500/30">
						<i class="bi bi-shield-x text-orange-400"></i>
					</div>
					<div>
						<h3 class="text-base font-semibold text-forest-50">Reset MFA</h3>
						<p class="text-xs text-forest-400 mt-0.5">For: <span class="font-medium text-forest-200">{resetMfaModal.username}</span></p>
					</div>
				</div>
				<button class="text-forest-400 hover:text-forest-100 cursor-pointer" onclick={() => (resetMfaModal.open = false)}>
					<i class="bi bi-x-lg text-sm"></i>
				</button>
			</div>
			<form onsubmit={resetMfa}>
				<div class="px-6 py-5 space-y-3">
					<p class="text-sm text-forest-300">This will immediately disable MFA and invalidate all active sessions for this user.</p>
					<div class="flex items-start gap-2.5 p-3 bg-amber-500/10 border border-amber-500/30 rounded-xl">
						<i class="bi bi-exclamation-triangle text-amber-400 shrink-0 mt-0.5"></i>
						<p class="text-xs text-amber-400">Only use this for account recovery when the user has lost access to their authenticator device.</p>
					</div>
				</div>
				<div class="flex gap-2.5 px-6 pb-5">
					<button type="button" onclick={() => (resetMfaModal.open = false)}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-forest-100 bg-forest-800 hover:bg-forest-700 rounded-xl transition-colors cursor-pointer">Cancel</button>
					<button type="submit" disabled={actionLoading}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-white bg-orange-600 hover:bg-orange-700 disabled:opacity-60 rounded-xl transition-colors cursor-pointer">
						<i class="bi bi-shield-x mr-1"></i>Reset MFA
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}

<!-- Disable Own MFA Modal -->
{#if disableMfaModal}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="absolute inset-0 bg-black/60 backdrop-blur-sm" onclick={() => (disableMfaModal = false)}></div>
		<div class="relative bg-forest-900 rounded-2xl shadow-2xl shadow-forest-950/80 w-full max-w-sm border border-forest-600 p-6 space-y-4">
			<div class="flex items-center gap-3">
				<div class="w-9 h-9 bg-red-500/15 rounded-xl flex items-center justify-center shrink-0 ring-1 ring-red-500/30">
					<i class="bi bi-shield-x text-red-400"></i>
				</div>
				<h3 class="text-base font-semibold text-forest-50">Disable Your MFA</h3>
			</div>
			<p class="text-sm text-forest-300">Enter your current authenticator code to confirm.</p>
			<form onsubmit={disableOwnMfa} autocomplete="off">
				<input
					type="text"
					bind:value={mfaCode}
					inputmode="numeric"
					pattern="[0-9]{6}"
					maxlength="6"
					required
					placeholder="000000"
					class="w-full text-center text-2xl font-mono tracking-[0.4em] px-4 py-3 border border-forest-600 rounded-xl bg-forest-800 text-forest-50 focus:outline-none focus:ring-2 focus:ring-red-500 focus:border-transparent transition mb-4"
				/>
				<div class="flex gap-2.5">
					<button type="button" onclick={() => (disableMfaModal = false)}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-forest-100 bg-forest-800 hover:bg-forest-700 rounded-xl transition-colors cursor-pointer">Cancel</button>
					<button type="submit" disabled={actionLoading}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-white bg-red-600 hover:bg-red-700 disabled:opacity-60 rounded-xl transition-colors cursor-pointer">
						{actionLoading ? 'Disabling…' : 'Disable MFA'}
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}
