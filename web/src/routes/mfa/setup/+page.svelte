<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, type User } from '$lib/api';
	import Nav from '$lib/components/Nav.svelte';

	let user = $state<User | null>(null);
	let qrCode = $state('');
	let secret = $state('');
	let code = $state('');
	let error = $state('');
	let loading = $state(false);
	let pageLoading = $state(true);

	onMount(async () => {
		try {
			user = await api.me();
			const setup = await api.mfaSetupGet();
			qrCode = setup.qr_code;
			secret = setup.secret;
		} catch {
			goto('/login');
		} finally {
			pageLoading = false;
		}
	});

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = '';
		loading = true;
		try {
			await api.mfaSetupActivate(code);
			// Session is invalidated after MFA setup – redirect to login
			goto('/login');
		} catch (err) {
			error = err instanceof Error ? err.message : 'Activation failed.';
			code = '';
		} finally {
			loading = false;
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
		<div class="max-w-md mx-auto">
			<div class="mb-6">
				<h1 class="text-xl font-bold text-forest-50 tracking-tight">Set Up Two-Factor Authentication</h1>
				<p class="text-sm text-forest-300 mt-0.5">Scan the QR code with Google Authenticator, Authy, or any TOTP app</p>
			</div>

			<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-xl shadow-forest-950/40 overflow-hidden">

				{#if error}
					<div class="mx-6 mt-5 flex items-start gap-2 p-3 bg-red-500/10 border border-red-500/30 rounded-xl">
						<i class="bi bi-exclamation-circle-fill text-red-400 mt-0.5 shrink-0 text-sm"></i>
						<p class="text-sm text-red-400">{error}</p>
					</div>
				{/if}

				<!-- Step 1: Scan -->
				<div class="px-6 pt-6 pb-4 border-b border-forest-700">
					<p class="text-xs font-semibold text-forest-400 uppercase tracking-wider mb-4">Step 1 — Scan with your app</p>
					<div class="flex justify-center">
						<div class="p-3 bg-white border border-forest-600 rounded-xl inline-block shadow-lg">
							{#if qrCode}
								<img src="data:image/png;base64,{qrCode}" width="200" height="200" alt="MFA QR Code" />
							{/if}
						</div>
					</div>

					<details class="mt-4">
						<summary class="text-xs text-forest-300 hover:text-forest-100 cursor-pointer select-none">
							Can't scan? Enter the key manually
						</summary>
						<p class="mt-2 text-xs font-mono bg-forest-800 p-3 rounded-xl border border-forest-700 break-all select-all text-forest-100">
							{secret}
						</p>
					</details>
				</div>

				<!-- Step 2: Verify -->
				<div class="px-6 py-5">
					<p class="text-xs font-semibold text-forest-400 uppercase tracking-wider mb-4">Step 2 — Enter the 6-digit code to confirm</p>
					<form onsubmit={handleSubmit} autocomplete="off">
						<input
							type="text"
							bind:value={code}
							inputmode="numeric"
							pattern="[0-9]{6}"
							maxlength="6"
							required
							autofocus
							placeholder="000000"
							class="w-full text-center text-3xl font-mono tracking-[0.4em] px-4 py-3.5 border border-forest-600 rounded-xl bg-forest-800 text-forest-50 focus:outline-none focus:ring-2 focus:ring-forest-400 focus:border-transparent transition mb-4"
						/>
						<button type="submit" disabled={loading}
							class="w-full flex items-center justify-center gap-2 bg-forest-400 hover:bg-forest-500 disabled:opacity-60 text-white font-medium py-2.5 rounded-xl text-sm transition-colors shadow-sm cursor-pointer">
							{#if loading}
								<svg class="animate-spin h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
									<circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
									<path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
								</svg>
								Activating…
							{:else}
								<i class="bi bi-shield-check"></i>Activate MFA
							{/if}
						</button>
					</form>
				</div>

			</div>
		</div>
	{/if}
</main>

<footer class="border-t border-forest-800 mt-12">
	<div class="max-w-7xl mx-auto px-4 py-4 text-center text-xs text-forest-500"></div>
</footer>
