<script lang="ts">
	import { onMount } from 'svelte';
	import { createEventDispatcher } from 'svelte';
	import {
		getPackageConfig,
		setPackageConfig,
		uninstallPackage,
		type SkillPackage
	} from '$lib/tauri';

	export let pkg: SkillPackage;
	const dispatch = createEventDispatcher<{ close: void; uninstalled: void }>();

	let config: Record<string, string> = {};
	let saving = false;
	let saved = false;
	let confirmUninstall = false;

	onMount(async () => {
		try {
			config = await getPackageConfig(pkg.meta.id);
		} catch (e) {
			console.error('Failed to load package config:', e);
		}
	});

	async function handleSave() {
		saving = true;
		try {
			for (const field of pkg.config_schema) {
				const value = config[field.key] || '';
				await setPackageConfig(pkg.meta.id, field.key, value);
			}
			saved = true;
			setTimeout(() => (saved = false), 2000);
		} catch (e) {
			console.error('Failed to save config:', e);
		} finally {
			saving = false;
		}
	}

	async function handleUninstall() {
		try {
			await uninstallPackage(pkg.meta.id);
			dispatch('uninstalled');
		} catch (e) {
			console.error('Failed to uninstall package:', e);
		}
	}
</script>

<div class="skill-config">
	<div class="config-header">
		<button class="back-btn" on:click={() => dispatch('close')} aria-label="Back to gallery">
			<svg
				xmlns="http://www.w3.org/2000/svg"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				width="16"
				height="16"
			>
				<polyline points="15 18 9 12 15 6"></polyline>
			</svg>
		</button>
		<div>
			<h2>{pkg.meta.name}</h2>
			<span class="version">v{pkg.meta.version}</span>
		</div>
	</div>

	<p class="description">{pkg.meta.description}</p>

	{#if pkg.config_schema.length > 0}
		<div class="fields">
			{#each pkg.config_schema as field}
				<div class="field">
					<label for="cfg-{field.key}">
						{field.label}
						{#if field.required}<span class="required">*</span>{/if}
					</label>
					{#if field.hint}
						<p class="hint">{field.hint}</p>
					{/if}

					{#if field.field_type === 'boolean'}
						<label class="toggle">
							<input
								type="checkbox"
								checked={config[field.key] === 'true'}
								on:change={(e) => {
									config[field.key] = e.currentTarget.checked ? 'true' : 'false';
								}}
							/>
							<span class="toggle-label">{config[field.key] === 'true' ? 'On' : 'Off'}</span>
						</label>
					{:else if field.field_type === 'select' && field.options}
						<select
							id="cfg-{field.key}"
							bind:value={config[field.key]}
						>
							<option value="">Select...</option>
							{#each field.options as opt}
								<option value={opt}>{opt}</option>
							{/each}
						</select>
					{:else if field.field_type === 'number'}
						<input
							id="cfg-{field.key}"
							type="number"
							bind:value={config[field.key]}
							placeholder={field.default || ''}
						/>
					{:else if field.field_type === 'secret'}
						<input
							id="cfg-{field.key}"
							type="password"
							bind:value={config[field.key]}
							placeholder={field.default || ''}
							autocomplete="off"
						/>
					{:else}
						<input
							id="cfg-{field.key}"
							type="text"
							bind:value={config[field.key]}
							placeholder={field.field_type === 'cron' ? '0 8 * * *' : field.default || ''}
						/>
					{/if}
				</div>
			{/each}
		</div>
	{:else}
		<p class="no-config">This skill has no configurable options.</p>
	{/if}

	<div class="actions">
		{#if pkg.config_schema.length > 0}
			<button class="save-btn" on:click={handleSave} disabled={saving}>
				{#if saving}
					Saving...
				{:else if saved}
					Saved
				{:else}
					Save Configuration
				{/if}
			</button>
		{/if}

		{#if confirmUninstall}
			<div class="confirm-bar">
				<span>Uninstall {pkg.meta.name}?</span>
				<button class="confirm-yes" on:click={handleUninstall}>Yes, uninstall</button>
				<button class="confirm-no" on:click={() => (confirmUninstall = false)}>Cancel</button>
			</div>
		{:else}
			<button class="uninstall-btn" on:click={() => (confirmUninstall = true)}>Uninstall</button>
		{/if}
	</div>
</div>

<style>
	.skill-config {
		padding: 24px;
		max-width: 560px;
	}

	.config-header {
		display: flex;
		align-items: center;
		gap: 12px;
		margin-bottom: 8px;
	}

	.config-header h2 {
		font-size: 18px;
		font-weight: 600;
		color: #1e293b;
		margin: 0;
	}

	.back-btn {
		background: none;
		border: 1px solid #e2e8f0;
		border-radius: 8px;
		padding: 6px;
		color: #64748b;
		cursor: pointer;
		display: flex;
		align-items: center;
	}

	.back-btn:hover {
		background: #f1f5f9;
		color: #1e293b;
	}

	.version {
		font-size: 12px;
		color: #94a3b8;
		font-weight: 500;
	}

	.description {
		font-size: 14px;
		color: #64748b;
		margin: 0 0 20px;
		line-height: 1.5;
	}

	.fields {
		display: flex;
		flex-direction: column;
		gap: 16px;
		margin-bottom: 24px;
	}

	.field label {
		display: block;
		font-size: 13px;
		font-weight: 600;
		color: #374151;
		margin-bottom: 4px;
	}

	.required {
		color: #ef4444;
		margin-left: 2px;
	}

	.hint {
		font-size: 12px;
		color: #6b7280;
		margin: 0 0 6px;
	}

	.field input,
	.field select {
		width: 100%;
		padding: 9px 12px;
		border: 1px solid #d1d5db;
		border-radius: 8px;
		font-size: 14px;
		outline: none;
		box-sizing: border-box;
		transition: border-color 0.15s;
		background: #fff;
	}

	.field input:focus,
	.field select:focus {
		border-color: #2563eb;
	}

	.toggle {
		display: flex;
		align-items: center;
		gap: 8px;
		cursor: pointer;
		font-weight: 400;
	}

	.toggle input {
		width: auto;
	}

	.toggle-label {
		font-size: 13px;
		color: #64748b;
	}

	.no-config {
		font-size: 14px;
		color: #94a3b8;
		margin-bottom: 24px;
	}

	.actions {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
	}

	.save-btn {
		padding: 9px 20px;
		background: #2563eb;
		color: #fff;
		border: none;
		border-radius: 8px;
		font-size: 14px;
		font-weight: 500;
		cursor: pointer;
		min-width: 140px;
		transition: background 0.15s;
	}

	.save-btn:hover:not(:disabled) {
		background: #1d4ed8;
	}

	.save-btn:disabled {
		background: #93c5fd;
		cursor: not-allowed;
	}

	.uninstall-btn {
		padding: 9px 20px;
		background: none;
		border: 1px solid #fca5a5;
		color: #dc2626;
		border-radius: 8px;
		font-size: 14px;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.15s;
	}

	.uninstall-btn:hover {
		background: #fef2f2;
	}

	.confirm-bar {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		color: #dc2626;
	}

	.confirm-yes {
		padding: 6px 14px;
		background: #dc2626;
		color: #fff;
		border: none;
		border-radius: 6px;
		font-size: 13px;
		cursor: pointer;
	}

	.confirm-yes:hover {
		background: #b91c1c;
	}

	.confirm-no {
		padding: 6px 14px;
		background: none;
		border: 1px solid #d1d5db;
		border-radius: 6px;
		font-size: 13px;
		color: #64748b;
		cursor: pointer;
	}

	.confirm-no:hover {
		background: #f1f5f9;
	}
</style>
