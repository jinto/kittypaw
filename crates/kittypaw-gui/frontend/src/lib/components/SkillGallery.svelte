<script lang="ts">
	import { onMount } from 'svelte';
	import { listPackages, type SkillPackage } from '$lib/tauri';
	import SkillConfig from './SkillConfig.svelte';

	let packages: SkillPackage[] = [];
	let selectedPackage: SkillPackage | null = null;
	let loading = true;
	let filter = '';
	let activeCategory = '';

	onMount(async () => {
		await loadPackages();
	});

	async function loadPackages() {
		loading = true;
		try {
			packages = await listPackages();
		} catch (e) {
			console.error('Failed to load packages:', e);
		} finally {
			loading = false;
		}
	}

	$: categories = [...new Set(packages.map((p) => p.meta.category))];

	$: filtered = packages.filter((p) => {
		const matchesFilter =
			!filter ||
			p.meta.name.toLowerCase().includes(filter.toLowerCase()) ||
			p.meta.category.toLowerCase().includes(filter.toLowerCase()) ||
			p.meta.tags.some((t) => t.toLowerCase().includes(filter.toLowerCase()));
		const matchesCategory = !activeCategory || p.meta.category === activeCategory;
		return matchesFilter && matchesCategory;
	});

	function handleUninstalled() {
		selectedPackage = null;
		loadPackages();
	}
</script>

{#if selectedPackage}
	<SkillConfig
		pkg={selectedPackage}
		on:close={() => (selectedPackage = null)}
		on:uninstalled={handleUninstalled}
	/>
{:else}
	<div class="gallery">
		<div class="toolbar">
			<input
				class="search"
				type="text"
				placeholder="Search skills..."
				bind:value={filter}
			/>
			{#if categories.length > 0}
				<div class="categories">
					<button
						class="cat-btn"
						class:active={activeCategory === ''}
						on:click={() => (activeCategory = '')}
					>
						All
					</button>
					{#each categories as cat}
						<button
							class="cat-btn"
							class:active={activeCategory === cat}
							on:click={() => (activeCategory = activeCategory === cat ? '' : cat)}
						>
							{cat}
						</button>
					{/each}
				</div>
			{/if}
		</div>

		{#if loading}
			<div class="empty-state">
				<p>Loading skills...</p>
			</div>
		{:else if packages.length === 0}
			<div class="empty-state">
				<div class="empty-icon">
					<svg
						xmlns="http://www.w3.org/2000/svg"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.5"
						width="48"
						height="48"
					>
						<path
							d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"
						></path>
						<polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline>
						<line x1="12" y1="22.08" x2="12" y2="12"></line>
					</svg>
				</div>
				<h2>No skills installed yet</h2>
				<p>Skills will appear here once installed.</p>
			</div>
		{:else if filtered.length === 0}
			<div class="empty-state">
				<h2>No matching skills</h2>
				<p>Try adjusting your search or filter.</p>
			</div>
		{:else}
			<div class="grid">
				{#each filtered as pkg (pkg.meta.id)}
					<button class="card" on:click={() => (selectedPackage = pkg)}>
						<div class="card-icon">
							<svg
								xmlns="http://www.w3.org/2000/svg"
								viewBox="0 0 24 24"
								fill="none"
								stroke="currentColor"
								stroke-width="1.5"
								width="24"
								height="24"
							>
								<path
									d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"
								></path>
								<polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline>
								<line x1="12" y1="22.08" x2="12" y2="12"></line>
							</svg>
						</div>
						<div class="card-body">
							<div class="card-title">{pkg.meta.name}</div>
							<div class="card-desc">{pkg.meta.description}</div>
							<div class="card-footer">
								<span class="category-badge">{pkg.meta.category}</span>
								<span class="card-version">v{pkg.meta.version}</span>
							</div>
						</div>
					</button>
				{/each}
			</div>
		{/if}
	</div>
{/if}

<style>
	.gallery {
		flex: 1;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}

	.toolbar {
		padding: 16px 20px 12px;
		display: flex;
		flex-direction: column;
		gap: 10px;
		border-bottom: 1px solid #f1f5f9;
	}

	.search {
		width: 100%;
		padding: 9px 12px;
		border: 1px solid #d1d5db;
		border-radius: 8px;
		font-size: 14px;
		outline: none;
		box-sizing: border-box;
		transition: border-color 0.15s;
	}

	.search:focus {
		border-color: #2563eb;
	}

	.categories {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}

	.cat-btn {
		padding: 4px 12px;
		border: 1px solid #e2e8f0;
		border-radius: 20px;
		background: #fff;
		color: #64748b;
		font-size: 12px;
		font-weight: 500;
		cursor: pointer;
		transition:
			background 0.15s,
			color 0.15s,
			border-color 0.15s;
	}

	.cat-btn:hover {
		background: #f1f5f9;
		color: #334155;
	}

	.cat-btn.active {
		background: #2563eb;
		color: #fff;
		border-color: #2563eb;
	}

	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
		gap: 12px;
		padding: 16px 20px;
		overflow-y: auto;
		flex: 1;
	}

	.card {
		display: flex;
		gap: 12px;
		padding: 16px;
		border: 1px solid #e2e8f0;
		border-radius: 12px;
		background: #fff;
		cursor: pointer;
		text-align: left;
		transition:
			border-color 0.15s,
			box-shadow 0.15s;
	}

	.card:hover {
		border-color: #93c5fd;
		box-shadow: 0 2px 8px rgba(59, 130, 246, 0.08);
	}

	.card-icon {
		flex-shrink: 0;
		width: 40px;
		height: 40px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: #eff6ff;
		border-radius: 10px;
		color: #3b82f6;
	}

	.card-body {
		flex: 1;
		min-width: 0;
	}

	.card-title {
		font-size: 14px;
		font-weight: 600;
		color: #1e293b;
		margin-bottom: 4px;
	}

	.card-desc {
		font-size: 12px;
		color: #64748b;
		line-height: 1.4;
		margin-bottom: 8px;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}

	.card-footer {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.category-badge {
		font-size: 11px;
		font-weight: 500;
		padding: 2px 8px;
		border-radius: 10px;
		background: #f1f5f9;
		color: #475569;
	}

	.card-version {
		font-size: 11px;
		color: #94a3b8;
	}

	.empty-state {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		text-align: center;
		padding: 40px;
	}

	.empty-icon {
		color: #94a3b8;
		margin-bottom: 16px;
	}

	.empty-state h2 {
		font-size: 18px;
		font-weight: 600;
		color: #1e293b;
		margin: 0 0 8px;
	}

	.empty-state p {
		font-size: 14px;
		color: #64748b;
		margin: 0;
	}
</style>
