<script lang="ts">
	import appPreferences from '$lib/stores/appPreferences.svelte';
	import { twMerge } from 'tailwind-merge';

	interface Props {
		class?: string;
		variant?: 'chat' | 'community' | 'enterprise' | 'default';
	}
	let { class: klass, variant = 'default' }: Props = $props();
	const appName = $derived(appPreferences.current.appName);
	const logoIcon = $derived(appPreferences.current.logos?.logoIcon);

	let logos = $derived({
		dark: {
			chat: appPreferences.current.logos?.darkLogoChat,
			enterprise: appPreferences.current.logos?.darkLogoEnterprise,
			community: appPreferences.current.logos?.darkLogoCommunity,
			default: appPreferences.current.logos?.darkLogoDefault
		},
		light: {
			chat: appPreferences.current.logos?.logoChat,
			enterprise: appPreferences.current.logos?.logoEnterprise,
			community: appPreferences.current.logos?.logoCommunity,
			default: appPreferences.current.logos?.logoDefault
		}
	});

	const logoPair = $derived.by(() => {
		if (variant === 'chat') return { light: logos.light.chat, dark: logos.dark.chat };
		if (variant === 'enterprise')
			return { light: logos.light.enterprise, dark: logos.dark.enterprise };
		if (variant === 'community')
			return { light: logos.light.community, dark: logos.dark.community };
		return { light: logos.light.default, dark: logos.dark.default };
	});

	const heightClass = $derived(variant === 'chat' ? 'h-[43px]' : 'h-10');
	const paddingClass = $derived(variant === 'chat' ? 'pl-[1px]' : '');
	const imgClass = $derived(twMerge(heightClass, paddingClass));
</script>

<div class={twMerge('flex shrink-0', klass)}>
	{#if variant === 'default'}
		<img src={logoIcon} class="size-9" alt="{appName} logo" />
		<span class="ml-2 text-lg font-semibold text-base-content">{appName}</span>
	{:else}
		<img src={logoPair.light} class={twMerge(imgClass, 'dark:hidden')} alt="{appName} logo" />
		<img src={logoPair.dark} class={twMerge(imgClass, 'hidden dark:block')} alt="{appName} logo" />
	{/if}
</div>
