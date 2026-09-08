<script lang="ts">
	import Confirm from '$lib/components/Confirm.svelte';
	import Layout from '$lib/components/Layout.svelte';
	import ResponsiveDialog from '$lib/components/ResponsiveDialog.svelte';
	import Loading from '$lib/icons/Loading.svelte';
	import { UserService, type CustomGroup, type OrgUser } from '$lib/services';
	import { Plus, Save, Trash2, Users } from '@lucide/svelte';

	let groups = $state<CustomGroup[]>([]);
	let users = $state<OrgUser[]>([]);
	let selected = $state<CustomGroup>();
	let groupName = $state('');
	let search = $state('');
	let loading = $state(true);
	let saving = $state(false);
	let error = $state('');
	let selectedUserIDs = $state<Set<string>>(new Set());
	let createGroupDialog = $state<ReturnType<typeof ResponsiveDialog>>();
	let createGroupName = $state('');
	let createGroupError = $state('');
	let groupToDelete = $state<CustomGroup>();

	const visibleUsers = $derived(
		users.filter((user) => {
			const value = `${user.displayName ?? ''} ${user.username} ${user.email}`.toLowerCase();
			return value.includes(search.toLowerCase());
		})
	);

	async function load() {
		loading = true;
		error = '';
		try {
			[groups, users] = await Promise.all([UserService.listCustomGroups(), UserService.listUsers()]);
			if (selected) {
				await selectGroup(selected.id);
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unable to load custom groups.';
		} finally {
			loading = false;
		}
	}

	async function selectGroup(id: string) {
		error = '';
		try {
			selected = await UserService.getCustomGroup(id);
			groupName = selected.name;
			selectedUserIDs = new Set((selected.memberUserIDs ?? []).map(String));
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unable to load group members.';
		}
	}

	function openCreateGroupDialog() {
		createGroupName = '';
		createGroupError = '';
		createGroupDialog?.open();
	}

	function closeCreateGroupDialog() {
		if (saving) return;
		createGroupDialog?.close();
	}

	async function createGroup() {
		const name = createGroupName.trim();
		if (!name) {
			createGroupError = 'A group name is required.';
			return;
		}

		saving = true;
		createGroupError = '';
		try {
			const group = await UserService.createCustomGroup(name);
			groups = [...groups, group].sort((a, b) => a.name.localeCompare(b.name));
			createGroupDialog?.close();
			await selectGroup(group.id);
		} catch (err) {
			createGroupError = err instanceof Error ? err.message : 'Unable to create group.';
		} finally {
			saving = false;
		}
	}

	async function saveGroup() {
		if (!selected) return;
		saving = true;
		error = '';
		try {
			await UserService.updateCustomGroup(selected.id, groupName);
			selected = await UserService.setCustomGroupMembers(selected.id, [...selectedUserIDs]);
			groups = await UserService.listCustomGroups();
			groupName = selected.name;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unable to save group.';
		} finally {
			saving = false;
		}
	}

	async function deleteGroup() {
		const group = groupToDelete;
		if (!group) return;

		saving = true;
		error = '';
		try {
			await UserService.deleteCustomGroup(group.id);
			groups = groups.filter((item) => item.id !== group.id);
			selected = undefined;
			groupName = '';
			selectedUserIDs = new Set();
			groupToDelete = undefined;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unable to delete group.';
		} finally {
			saving = false;
		}
	}

	function toggleUser(userID: string) {
		const next = new Set(selectedUserIDs);
		if (next.has(userID)) next.delete(userID);
		else next.add(userID);
		selectedUserIDs = next;
	}

	void load();
</script>

<Layout title="Custom Groups">
	<div class="flex h-full min-h-0 flex-col gap-5">
		<div class="flex items-center justify-between gap-3">
			<p class="text-muted-content text-sm">
				Create Obot-managed groups and assign synchronized or local users. LDAP membership is not changed.
			</p>
			<button class="btn btn-primary" disabled={saving} onclick={openCreateGroupDialog}>
				<Plus class="size-4" /> Create group
			</button>
		</div>
		{#if error}<div class="alert alert-error text-sm" role="alert">{error}</div>{/if}
		{#if loading}
			<div class="flex flex-1 items-center justify-center"><Loading class="size-7" /></div>
		{:else}
			<div class="grid min-h-0 flex-1 gap-5 lg:grid-cols-[20rem_1fr]">
				<div class="border-base-300 min-h-0 overflow-auto rounded-lg border">
					{#if groups.length === 0}
						<div class="text-muted-content p-5 text-sm">No custom groups yet.</div>
					{:else}
						{#each groups as group (group.id)}
							<button
								class:btn-primary={selected?.id === group.id}
								class="flex w-full items-center justify-between gap-3 border-b border-base-300 p-4 text-left hover:bg-base-200"
								onclick={() => selectGroup(group.id)}
							>
								<span class="font-medium">{group.name}</span>
								<span class="text-muted-content text-xs">{group.memberCount ?? 0} users</span>
							</button>
						{/each}
					{/if}
				</div>
				{#if selected}
					<div class="min-h-0 rounded-lg border border-base-300 p-5">
						<div class="mb-5 flex items-end gap-3">
							<label class="form-control flex-1">
								<span class="label-text">Group name</span>
								<input class="input input-bordered w-full" bind:value={groupName} />
							</label>
							<button class="btn btn-primary" disabled={saving} onclick={saveGroup}>
								<Save class="size-4" /> Save
							</button>
							<button
								class="btn btn-ghost text-error"
								disabled={saving}
								onclick={() => (groupToDelete = selected)}
								aria-label="Delete group"
							>
								<Trash2 class="size-4" />
							</button>
						</div>
						<div class="mb-3 flex items-center gap-2">
							<Users class="size-4" /><h2 class="font-semibold">Members ({selectedUserIDs.size})</h2>
						</div>
						<input class="input input-bordered mb-3 w-full" bind:value={search} placeholder="Search users..." />
						<div class="max-h-[55vh] overflow-auto rounded border border-base-300">
							{#each visibleUsers as user (user.id)}
								<label class="flex cursor-pointer items-center gap-3 border-b border-base-300 p-3 last:border-0 hover:bg-base-200">
									<input
										type="checkbox"
										class="checkbox checkbox-primary"
										checked={selectedUserIDs.has(user.id)}
										onchange={() => toggleUser(user.id)}
									/>
									<span class="flex min-w-0 flex-1 flex-col">
										<span class="truncate font-medium">{user.displayName || user.username}</span>
										<span class="text-muted-content truncate text-xs">
											{user.email || user.username}{user.disabled ? ' · Disabled' : ''}
										</span>
									</span>
								</label>
							{/each}
						</div>
					</div>
				{:else}
					<div class="text-muted-content flex items-center justify-center rounded-lg border border-dashed border-base-300 p-8 text-sm">
						Select or create a custom group.
					</div>
				{/if}
			</div>
		{/if}
	</div>
</Layout>

<ResponsiveDialog
	bind:this={createGroupDialog}
	title="Create custom group"
	class="w-full md:max-w-md"
	onClose={() => {
		if (!saving) {
			createGroupName = '';
			createGroupError = '';
		}
	}}
>
	<form
		class="flex flex-col gap-4 p-4"
		onsubmit={(event) => {
			event.preventDefault();
			void createGroup();
		}}
	>
		<p class="text-muted-content text-sm">
			Custom groups are managed in Obot. Creating one does not change your LDAP directory.
		</p>
		<label class="form-control gap-2">
			<span class="label-text">Group name</span>
			<input
				class="input input-bordered w-full"
				bind:value={createGroupName}
				placeholder="For example, Developers"
				disabled={saving}
				autofocus
			/>
		</label>
		{#if createGroupError}<div class="alert alert-error text-sm" role="alert">{createGroupError}</div>{/if}
		<div class="flex justify-end gap-2">
			<button type="button" class="btn btn-secondary" disabled={saving} onclick={closeCreateGroupDialog}>Cancel</button>
			<button type="submit" class="btn btn-primary" disabled={saving || !createGroupName.trim()}>
				{#if saving}<Loading class="size-4" />{/if}Create group
			</button>
		</div>
	</form>
</ResponsiveDialog>

<Confirm
	show={Boolean(groupToDelete)}
	msg={`Delete ${groupToDelete?.name || 'this group'}?`}
	note="Removing a custom group does not delete users or modify LDAP."
	loading={saving}
	onsuccess={deleteGroup}
	oncancel={() => (groupToDelete = undefined)}
/>