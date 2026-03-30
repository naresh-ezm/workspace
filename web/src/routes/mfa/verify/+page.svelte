<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';

	let code = $state('');
	let error = $state('');
	let loading = $state(false);

	async function handleSubmit(e: Event) {
		e.preventDefault();
		error = '';
		loading = true;
		try {
			const result = await api.mfaVerify(code.replace(/\s/g, ''));
			goto(result.role === 'admin' ? '/admin' : '/dashboard');
		} catch (err) {
			error = err instanceof Error ? err.message : 'Verification failed.';
			code = '';
		} finally {
			loading = false;
		}
	}
</script>

<div class="min-h-[70vh] flex items-center justify-center">
	<div class="bg-forest-900 border border-forest-600 rounded-2xl shadow-xl shadow-forest-950/50 w-full max-w-sm p-8">

		<div class="flex items-center gap-3 mb-6">
			<div class="w-10 h-10 bg-forest-700 rounded-xl flex items-center justify-center shrink-0 ring-1 ring-forest-400/30">
				<i class="bi bi-shield-lock text-forest-300 text-lg"></i>
			</div>
			<div>
				<h1 class="text-base font-semibold text-forest-50">Two-Factor Authentication</h1>
				<p class="text-xs text-forest-400 mt-0.5">Enter the 6-digit code from your authenticator app</p>
			</div>
		</div>

		{#if error}
			<div class="mb-4 flex items-start gap-2 p-3 bg-red-500/10 border border-red-500/30 rounded-xl">
				<i class="bi bi-exclamation-circle-fill text-red-400 mt-0.5 shrink-0 text-sm"></i>
				<p class="text-sm text-red-400">{error}</p>
			</div>
		{/if}

		<form onsubmit={handleSubmit} autocomplete="off">
			<input
				type="text"
				bind:value={code}
				inputmode="numeric"
				maxlength="6"
				required
				placeholder="000000"
				class="w-full text-center text-3xl font-mono tracking-[0.4em] px-4 py-3.5 border border-forest-600 rounded-xl bg-forest-800 text-forest-50 focus:outline-none focus:ring-2 focus:ring-forest-400 focus:border-transparent transition mb-4"
			/>
			<button type="submit" disabled={loading}
				class="w-full bg-forest-400 hover:bg-forest-500 disabled:opacity-60 text-white font-medium py-2.5 rounded-xl text-sm transition-colors shadow-sm cursor-pointer">
				{loading ? 'Verifying…' : 'Verify'}
			</button>
		</form>

		<p class="mt-5 text-center text-xs text-forest-400">
			Code expired or no access to your app?<br />Contact an admin to reset your MFA.
		</p>

	</div>
</div>
