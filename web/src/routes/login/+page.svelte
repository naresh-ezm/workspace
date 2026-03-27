<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api } from '$lib/api';

	let username = $state('');
	let pin = $state('');
	let error = $state('');
	let loading = $state(false);

	onMount(async () => {
		// Redirect if already authenticated
		try {
			const user = await api.me();
			goto(user.role === 'admin' ? '/admin' : '/dashboard');
		} catch {
			// Not authenticated — stay on login page
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = '';
		loading = true;
		try {
			const result = await api.login(username, pin);
			if (result.mfa_required) {
				goto('/mfa/verify');
			} else {
				goto(result.role === 'admin' ? '/admin' : '/dashboard');
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Login failed.';
		} finally {
			loading = false;
		}
	}
</script>

<div class="min-h-[80vh] flex items-center justify-center px-4">
	<div class="w-full max-w-sm">

		<!-- Logo block -->
		<div class="text-center mb-8">
			<div class="w-16 h-16 bg-indigo-600 rounded-2xl flex items-center justify-center mx-auto mb-4 shadow-xl shadow-indigo-500/25">
				<i class="bi bi-pc-display text-white text-2xl"></i>
			</div>
			<h1 class="text-2xl font-bold text-gray-900 tracking-tight">EC2 Desktop Manager</h1>
			<p class="text-gray-500 text-sm mt-1">Sign in to access your workspace</p>
		</div>

		<!-- Card -->
		<div class="bg-white rounded-2xl border border-gray-200 shadow-sm p-8">

			{#if error}
				<div class="mb-5 flex items-start gap-2.5 p-3.5 bg-red-50 border border-red-200 rounded-xl">
					<i class="bi bi-exclamation-circle-fill text-red-500 mt-0.5 flex-shrink-0"></i>
					<p class="text-red-700 text-sm">{error}</p>
				</div>
			{/if}

			<form onsubmit={handleSubmit} autocomplete="off" novalidate>

				<div class="mb-4">
					<label for="username" class="block text-sm font-medium text-gray-700 mb-1.5">Username</label>
					<div class="relative">
						<i class="bi bi-person absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none"></i>
						<input
							type="text" id="username" bind:value={username}
							class="w-full pl-9 pr-3 py-2.5 text-sm border border-gray-300 rounded-xl bg-gray-50 focus:bg-white focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent transition placeholder-gray-400"
							placeholder="your-username" required autofocus maxlength="64"
						/>
					</div>
				</div>

				<div class="mb-6">
					<label for="pin" class="block text-sm font-medium text-gray-700 mb-1.5">PIN</label>
					<div class="relative">
						<i class="bi bi-key absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none"></i>
						<input
							type="password" id="pin" bind:value={pin}
							class="w-full pl-9 pr-3 py-2.5 text-sm border border-gray-300 rounded-xl bg-gray-50 focus:bg-white focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent transition placeholder-gray-400"
							placeholder="••••••••" required maxlength="128"
						/>
					</div>
				</div>

				<button type="submit" disabled={loading}
					class="w-full flex items-center justify-center gap-2 bg-indigo-600 hover:bg-indigo-700 active:bg-indigo-800 disabled:opacity-60 text-white font-medium py-2.5 px-4 rounded-xl text-sm transition-colors shadow-sm shadow-indigo-500/30 cursor-pointer">
					{#if loading}
						<svg class="animate-spin h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
							<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
							<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
						</svg>
						Signing in…
					{:else}
						<i class="bi bi-box-arrow-in-right"></i>
						Sign In
					{/if}
				</button>

			</form>
		</div>

		<p class="text-center text-gray-400 text-xs mt-5">
			<i class="bi bi-lock mr-1"></i>Contact your administrator if you cannot log in.
		</p>

	</div>
</div>
