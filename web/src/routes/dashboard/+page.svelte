<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, type User, type DashboardData } from '$lib/api';
	import Nav from '$lib/components/Nav.svelte';

	let user = $state<User | null>(null);
	let data = $state<DashboardData>({});
	let pageLoading = $state(true);
	let error = $state('');
	let success = $state('');
	let actionLoading = $state(false);

	// MFA disable modal
	let showDisableMfaModal = $state(false);
	let mfaCode = $state('');
	let mfaError = $state('');
	let mfaLoading = $state(false);

	let refreshInterval: ReturnType<typeof setInterval> | null = null;

	onMount(async () => {
		try {
			user = await api.me();
			if (user.role === 'admin') {
				goto('/admin');
				return;
			}
			await loadDashboard();
		} catch {
			goto('/login');
			return;
		}
		pageLoading = false;
	});

	onDestroy(() => {
		if (refreshInterval) clearInterval(refreshInterval);
	});

	async function loadDashboard() {
		try {
			data = await api.dashboard();
			// Refresh automatically while instance is transitioning
			const state = data.aws_instance?.state;
			if (state === 'pending' || state === 'stopping') {
				if (!refreshInterval) {
					refreshInterval = setInterval(async () => {
						data = await api.dashboard();
						const newState = data.aws_instance?.state;
						if (newState !== 'pending' && newState !== 'stopping') {
							clearInterval(refreshInterval!);
							refreshInterval = null;
						}
					}, 10000);
				}
			} else {
				if (refreshInterval) {
					clearInterval(refreshInterval);
					refreshInterval = null;
				}
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load dashboard.';
		}
	}

	async function startInstance() {
		error = '';
		success = '';
		actionLoading = true;
		try {
			const res = await api.startInstance();
			success = res.message;
			await loadDashboard();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to start instance.';
		} finally {
			actionLoading = false;
		}
	}

	async function stopInstance() {
		if (!confirm('Stop the instance? Any unsaved work may be lost.')) return;
		error = '';
		success = '';
		actionLoading = true;
		try {
			const res = await api.stopInstance();
			success = res.message;
			await loadDashboard();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to stop instance.';
		} finally {
			actionLoading = false;
		}
	}

	async function disableMfa() {
		mfaError = '';
		mfaLoading = true;
		try {
			await api.mfaDisable(mfaCode);
			showDisableMfaModal = false;
			mfaCode = '';
			success = 'MFA disabled successfully.';
			user = await api.me();
		} catch (err) {
			mfaError = err instanceof Error ? err.message : 'Failed to disable MFA.';
		} finally {
			mfaLoading = false;
		}
	}

	function stateBadgeClass(state: string) {
		if (state === 'running') return 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30';
		if (state === 'stopped') return 'bg-forest-800 text-forest-300 border-forest-600';
		return 'bg-amber-500/15 text-amber-400 border-amber-500/30';
	}

	function stateDotClass(state: string) {
		if (state === 'running') return 'bg-emerald-500 animate-pulse';
		if (state === 'stopped') return 'bg-forest-400';
		return 'bg-amber-400 animate-pulse';
	}

	function stateLabel(state: string) {
		if (state === 'pending') return 'Starting…';
		if (state === 'stopping') return 'Stopping…';
		return state.charAt(0).toUpperCase() + state.slice(1);
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
			<h1 class="text-xl font-bold text-forest-50 tracking-tight">Developer Dashboard</h1>
			<p class="text-sm text-forest-300 mt-0.5">Manage your assigned EC2 desktop instance</p>
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

		{#if !data.aws_instance}
			<!-- No instance assigned -->
			<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30">
				<div class="flex flex-col items-center justify-center py-20 text-center px-6">
					<div class="w-14 h-14 bg-forest-800 rounded-2xl flex items-center justify-center mb-4 ring-1 ring-forest-600">
						<i class="bi bi-hdd-network text-forest-400 text-2xl"></i>
					</div>
					<h3 class="text-base font-semibold text-forest-100">No Instance Assigned</h3>
					<p class="text-sm text-forest-400 mt-1 max-w-xs">Contact an administrator to have an EC2 instance assigned to your account.</p>
				</div>
			</div>
		{:else}
			{@const inst = data.aws_instance}
			{@const state = inst.state}

			<div class="max-w-2xl space-y-4">

				<!-- Instance card -->
				<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30 overflow-hidden">

					<!-- Card header -->
					<div class="flex items-center justify-between px-5 py-4 border-b border-forest-700 bg-forest-800/50">
						<div class="flex items-center gap-2.5">
							<div class="w-8 h-8 bg-forest-700 rounded-lg flex items-center justify-center ring-1 ring-forest-500/30">
								<i class="bi bi-server text-forest-300 text-sm"></i>
							</div>
							<span class="font-mono text-sm font-semibold text-forest-100">{inst.instance_id}</span>
						</div>

						<!-- State badge -->
						<span class="inline-flex items-center gap-1.5 px-3 py-1 text-xs font-semibold rounded-full border {stateBadgeClass(state)}">
							<span class="w-1.5 h-1.5 rounded-full {stateDotClass(state)}"></span>
							{stateLabel(state)}
						</span>
					</div>

					<!-- Stats grid -->
					<div class="grid grid-cols-2 gap-px bg-forest-700">

						<div class="bg-forest-900 px-5 py-4">
							<p class="text-xs text-forest-400 font-medium uppercase tracking-wider mb-1">Instance Type</p>
							<p class="text-sm font-semibold text-forest-100">
								{inst.instance_type || '—'}
							</p>
						</div>

						<div class="bg-forest-900 px-5 py-4">
							<p class="text-xs text-forest-400 font-medium uppercase tracking-wider mb-1">Public IP</p>
							{#if inst.public_ip}
								<a href="rdp://{inst.public_ip}" class="text-sm font-semibold text-forest-300 hover:text-forest-100 font-mono transition-colors">
									{inst.public_ip}
								</a>
							{:else}
								<p class="text-sm font-semibold text-forest-500">—</p>
							{/if}
						</div>

						{#if data.db_instance}
							{@const db = data.db_instance}
							<div class="bg-forest-900 px-5 py-4">
								<p class="text-xs text-forest-400 font-medium uppercase tracking-wider mb-1">XRDP Activity</p>
								{#if db.status === 'active'}
									<span class="inline-flex items-center gap-1.5 text-sm font-semibold text-emerald-400">
										<span class="w-1.5 h-1.5 bg-emerald-500 rounded-full animate-pulse"></span>Active
									</span>
								{:else if db.status === 'idle'}
									<span class="inline-flex items-center gap-1.5 text-sm font-semibold text-amber-400">
										<i class="bi bi-moon text-xs"></i>Idle
									</span>
								{:else}
									<span class="text-sm text-forest-500">Unknown</span>
								{/if}
							</div>

							<div class="bg-forest-900 px-5 py-4">
								<p class="text-xs text-forest-400 font-medium uppercase tracking-wider mb-1">Last Heartbeat</p>
								<p class="text-sm font-semibold text-forest-100">
									{db.last_heartbeat_at ?? '—'}
								</p>
							</div>
						{/if}

					</div>

					<!-- Actions -->
					<div class="px-5 py-4 border-t border-forest-700 flex gap-3">
						{#if state === 'stopped' || state === ''}
							<button onclick={startInstance} disabled={actionLoading}
								class="flex-1 flex items-center justify-center gap-2 bg-emerald-600 hover:bg-emerald-700 disabled:opacity-60 text-white font-medium py-2.5 px-4 rounded-xl text-sm transition-colors shadow-sm cursor-pointer">
								<i class="bi bi-play-circle-fill"></i>Start Instance
							</button>
						{/if}

						{#if state === 'running'}
							<button onclick={stopInstance} disabled={actionLoading}
								class="flex-1 flex items-center justify-center gap-2 bg-red-600 hover:bg-red-700 disabled:opacity-60 text-white font-medium py-2.5 px-4 rounded-xl text-sm transition-colors shadow-sm cursor-pointer">
								<i class="bi bi-stop-circle-fill"></i>Stop Instance
							</button>
						{/if}

						{#if state === 'pending' || state === 'stopping'}
							<button disabled
								class="flex-1 flex items-center justify-center gap-2 bg-forest-800 text-forest-400 font-medium py-2.5 px-4 rounded-xl text-sm cursor-not-allowed">
								<svg class="animate-spin h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
								</svg>
								Please wait…
							</button>
						{/if}
					</div>
				</div>

				{#if state === 'pending' || state === 'stopping'}
					<p class="text-center text-xs text-forest-400">
						<i class="bi bi-arrow-repeat mr-1"></i>Polling for updates automatically…
					</p>
				{/if}

			</div>
		{/if}

		<!-- MFA Status Card -->
		{#if user}
			<div class="max-w-2xl mt-5">
				<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-lg shadow-forest-950/30 px-5 py-4 flex items-center justify-between">
					<div class="flex items-center gap-3">
						<div class="w-9 h-9 bg-forest-700 rounded-xl flex items-center justify-center shrink-0 ring-1 ring-forest-500/30">
							<i class="bi bi-shield-lock text-forest-300"></i>
						</div>
						<div>
							<p class="text-sm font-semibold text-forest-100">Two-Factor Authentication</p>
							{#if user.totp_enabled}
								<p class="text-xs text-emerald-400 font-medium mt-0.5"><i class="bi bi-check-circle-fill mr-1"></i>Enabled</p>
							{:else}
								<p class="text-xs text-forest-400 mt-0.5">Not enabled — your account has no MFA protection</p>
							{/if}
						</div>
					</div>

					{#if user.totp_enabled}
						<button onclick={() => { showDisableMfaModal = true; mfaCode = ''; mfaError = ''; }}
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
			</div>
		{/if}
	{/if}
</main>

<footer class="border-t border-forest-800 mt-12">
	<div class="max-w-7xl mx-auto px-4 py-4 text-center text-xs text-forest-500"></div>
</footer>

<!-- Disable MFA Modal -->
{#if showDisableMfaModal}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div class="absolute inset-0 bg-black/60 backdrop-blur-sm" onclick={() => (showDisableMfaModal = false)}></div>
		<div class="relative bg-forest-900 rounded-2xl shadow-2xl shadow-forest-950/80 w-full max-w-sm border border-forest-600 p-6 space-y-4">
			<div class="flex items-center gap-3">
				<div class="w-9 h-9 bg-red-500/15 rounded-xl flex items-center justify-center shrink-0 ring-1 ring-red-500/30">
					<i class="bi bi-shield-x text-red-400"></i>
				</div>
				<h3 class="text-base font-semibold text-forest-50">Disable MFA</h3>
			</div>
			<p class="text-sm text-forest-300">Enter your current authenticator code to confirm.</p>

			{#if mfaError}
				<div class="flex items-start gap-2 p-3 bg-red-500/10 border border-red-500/30 rounded-xl">
					<i class="bi bi-exclamation-circle-fill text-red-400 mt-0.5 shrink-0 text-sm"></i>
					<p class="text-sm text-red-400">{mfaError}</p>
				</div>
			{/if}

			<form onsubmit={(e) => { e.preventDefault(); disableMfa(); }} autocomplete="off">
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
					<button type="button" onclick={() => (showDisableMfaModal = false)}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-forest-100 bg-forest-800 hover:bg-forest-700 rounded-xl transition-colors cursor-pointer">
						Cancel
					</button>
					<button type="submit" disabled={mfaLoading}
						class="flex-1 px-4 py-2.5 text-sm font-medium text-white bg-red-600 hover:bg-red-700 disabled:opacity-60 rounded-xl transition-colors cursor-pointer">
						{mfaLoading ? 'Disabling…' : 'Disable MFA'}
					</button>
				</div>
			</form>
		</div>
	</div>
{/if}
