<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, type User } from '$lib/api';

	let { user }: { user: User } = $props();

	async function logout() {
		try {
			await api.logout();
		} finally {
			goto('/login');
		}
	}
</script>

<nav class="bg-forest-950 border-b border-forest-700 sticky top-0 z-40">
	<div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
		<div class="flex items-center justify-between h-15 py-3">

			<!-- Brand -->
			<a href="/" class="flex items-center gap-2.5">
				<div class="w-8 h-8 bg-forest-400 rounded-lg flex items-center justify-center shadow-lg shadow-forest-950/50">
					<i class="bi bi-pc-display text-white text-sm"></i>
				</div>
				<span class="text-forest-50 font-semibold tracking-tight">EC2 Desktop Manager</span>
			</a>

			<!-- Right side -->
			<div class="flex items-center gap-2">
				<!-- User pill -->
				<div class="flex items-center gap-2 px-3 py-1.5 bg-forest-800/60 rounded-lg border border-forest-600/50">
					<div class="w-5 h-5 bg-forest-400 rounded-full flex items-center justify-center">
						<i class="bi bi-person-fill text-white" style="font-size:0.6rem"></i>
					</div>
					<span class="text-forest-100 text-sm font-medium">{user.username}</span>
					{#if user.role === 'admin'}
						<span class="px-1.5 py-0.5 bg-amber-400/15 text-amber-400 text-xs rounded font-medium border border-amber-400/20">Admin</span>
					{:else}
						<span class="px-1.5 py-0.5 bg-forest-400/15 text-forest-300 text-xs rounded font-medium border border-forest-400/20">Dev</span>
					{/if}
				</div>

				<!-- Nav link -->
				{#if user.role === 'admin'}
					<a href="/admin" class="flex items-center gap-1.5 px-3 py-1.5 text-forest-300 hover:text-forest-50 hover:bg-forest-800 rounded-lg text-sm transition-colors">
						<i class="bi bi-shield-lock text-xs"></i>Admin
					</a>
				{:else}
					<a href="/dashboard" class="flex items-center gap-1.5 px-3 py-1.5 text-forest-300 hover:text-forest-50 hover:bg-forest-800 rounded-lg text-sm transition-colors">
						<i class="bi bi-speedometer2 text-xs"></i>Dashboard
					</a>
				{/if}

				<!-- Logout -->
				<button
					onclick={logout}
					class="flex items-center gap-1.5 px-3 py-1.5 text-forest-300 hover:text-red-400 hover:bg-forest-800 rounded-lg text-sm transition-colors cursor-pointer"
				>
					<i class="bi bi-box-arrow-right text-xs"></i>Logout
				</button>
			</div>

		</div>
	</div>
</nav>
