<script lang="ts">
	import { browser } from '$app/environment';
	import Logo from '$lib/components/Logo.svelte';
	import SensitiveInput from '$lib/components/SensitiveInput.svelte';
	import { CircleAlert } from '@lucide/svelte';

	let params = $derived(browser ? new URL(window.location.href).searchParams : undefined);
	let rd = $derived(params?.get('rd') ?? '/');
	let error = $derived(params?.get('error'));
</script>

<svelte:head><title>Obot | LDAP Sign In</title></svelte:head>

<div class="text-base-content dark:from-base-300 to-base-200 flex h-dvh w-full flex-col items-center justify-center bg-radial-[at_50%_50%] from-gray-50 dark:to-black">
	<form method="POST" action="/oauth2/start" class="dark:border-base-400 dark:bg-base-200 bg-base-100 flex w-sm flex-col gap-4 rounded-xl border border-transparent p-6 shadow-sm">
		<Logo class="h-12 self-center" />
		<h1 class="text-center text-xl font-semibold">Sign in to Obot</h1>
		{#if error}<div class="notification-error flex items-center gap-2"><CircleAlert class="text-error size-5 shrink-0" /><p class="text-sm font-light">{error}</p></div>{/if}
		<input type="hidden" name="rd" value={rd} />
		<label class="flex flex-col gap-1 text-sm font-light" for="ldap-auth-username">Username or email<input id="ldap-auth-username" class="text-input-filled" name="email" autocomplete="username" required /></label>
		<label class="flex flex-col gap-1 text-sm font-light" for="ldap-auth-password">Password<SensitiveInput name="password" class="text-input-filled" autocomplete="current-password" required data1pIgnore={false} /></label>
		<button class="btn btn-primary w-full" type="submit">Sign in</button>
	</form>
</div>
