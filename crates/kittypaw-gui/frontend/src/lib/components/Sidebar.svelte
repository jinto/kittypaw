<script lang="ts">
	import { createEventDispatcher } from 'svelte';
	import { open } from '@tauri-apps/plugin-dialog';
	import { invoke } from '@tauri-apps/api/core';
	import { currentWorkspace, fileTree, selectedFile } from '$lib/stores/workspace';
	import type { Workspace, FileEntry } from '$lib/stores/workspace';
	import FileTree from './FileTree.svelte';
	import SearchBar from './SearchBar.svelte';

	export let showSettings = false;

	const dispatch = createEventDispatcher<{ openSettings: void; newChat: void; openSkills: void }>();

	async function openWorkspace() {
		try {
			const selected = await open({ directory: true, multiple: false, title: 'Open Workspace' });
			if (!selected || typeof selected !== 'string') return;

			const ws: Workspace = await invoke('open_workspace', { path: selected });
			currentWorkspace.set(ws);

			const files: FileEntry[] = await invoke('list_files', { workspaceId: ws.id });
			fileTree.set(files);
		} catch (e) {
			console.error('open_workspace error:', e);
		}
	}
</script>

<aside class="sidebar">
	<div class="logo">
		<span class="logo-icon">◉</span>
		<span class="logo-text">KittyPaw</span>
	</div>

	<nav class="nav">
		<button class="nav-item active" on:click={() => dispatch('newChat')}>
			<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
				<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
			</svg>
			New Chat
		</button>

		<button class="nav-item workspace-btn" on:click={openWorkspace}>
			<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
				<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
			</svg>
			Open Workspace
		</button>

		<button class="nav-item" on:click={() => dispatch('openSkills')}>
			<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
				<path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path>
				<polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline>
				<line x1="12" y1="22.08" x2="12" y2="12"></line>
			</svg>
			Skills
		</button>
	</nav>

	{#if $currentWorkspace}
		<SearchBar
			workspaceId={$currentWorkspace.id}
			onFileSelect={(path) => selectedFile.set(path)}
		/>
		<div class="file-tree-wrap">
			<FileTree />
		</div>
	{:else}
		<div class="spacer"></div>
	{/if}

	<button
		class="nav-item settings-btn"
		class:active={showSettings}
		on:click={() => dispatch('openSettings')}
	>
		<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
			<circle cx="12" cy="12" r="3"></circle>
			<path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
		</svg>
		Settings
	</button>
</aside>

<style>
	.sidebar {
		width: 250px;
		min-width: 250px;
		height: 100%;
		background: #0f172a;
		color: #e2e8f0;
		display: flex;
		flex-direction: column;
		padding: 16px 12px;
		box-sizing: border-box;
	}

	.logo {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 8px 12px;
		margin-bottom: 16px;
	}

	.logo-icon {
		font-size: 20px;
		color: #3b82f6;
	}

	.logo-text {
		font-size: 18px;
		font-weight: 700;
		color: #f8fafc;
		letter-spacing: -0.5px;
	}

	.nav {
		display: flex;
		flex-direction: column;
		gap: 4px;
		margin-bottom: 8px;
	}

	.nav-item {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 9px 12px;
		border-radius: 8px;
		border: none;
		background: transparent;
		color: #94a3b8;
		font-size: 13px;
		cursor: pointer;
		text-align: left;
		width: 100%;
		transition: background 0.15s, color 0.15s;
	}

	.nav-item:hover {
		background: #1e293b;
		color: #e2e8f0;
	}

	.nav-item.active {
		background: #1e3a5f;
		color: #93c5fd;
	}

	.workspace-btn {
		color: #94a3b8;
	}

	.file-tree-wrap {
		flex: 1;
		overflow: hidden;
		margin: 4px -12px;
		border-top: 1px solid #1e293b;
		padding-top: 4px;
	}

	.spacer {
		flex: 1;
	}

	.settings-btn {
		margin-top: 8px;
	}
</style>
