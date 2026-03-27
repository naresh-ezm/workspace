<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api';

	onMount(async () => {
		try {
			const user = await api.me();
			goto(user.role === 'admin' ? '/admin' : '/dashboard');
		} catch {
			goto('/login');
		}
	});
</script>

<!-- Blank while redirecting -->
