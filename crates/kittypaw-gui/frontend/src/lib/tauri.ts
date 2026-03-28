import { invoke } from '@tauri-apps/api/core';
import { listen } from '@tauri-apps/api/event';
import type {
	FilePermissionRule,
	GlobalPath,
	NetworkPermissionRule,
	PermissionProfile,
	PermissionRequest
} from '$lib/stores/permission';

export async function sendMessage(message: string): Promise<string> {
	return await invoke<string>('send_message', { message });
}

export async function getSettings(): Promise<string> {
	return await invoke<string>('get_settings');
}

export async function saveApiKey(apiKey: string): Promise<void> {
	await invoke('save_api_key', { apiKey });
}

export function onStreamToken(callback: (token: string) => void) {
	return listen<string>('llm-stream', (event) => {
		callback(event.payload);
	});
}

// ── Permission API ─────────────────────────────────────────────────────────

export async function getPermissionProfile(workspaceId: string): Promise<PermissionProfile> {
	return await invoke<PermissionProfile>('get_permission_profile', {
		workspaceId
	});
}

export async function saveFileRule(
	workspaceId: string,
	pathPattern: string,
	isException: boolean,
	canRead: boolean,
	canWrite: boolean,
	canDelete: boolean
): Promise<string> {
	return await invoke<string>('save_file_rule', {
		workspaceId,
		pathPattern,
		isException,
		canRead,
		canWrite,
		canDelete
	});
}

export async function deleteFileRule(ruleId: string): Promise<void> {
	await invoke('delete_file_rule', { ruleId });
}

export async function saveNetworkRule(
	workspaceId: string,
	domainPattern: string,
	allowedMethods: string[]
): Promise<string> {
	return await invoke<string>('save_network_rule', {
		workspaceId,
		domainPattern,
		allowedMethods
	});
}

export async function deleteNetworkRule(ruleId: string): Promise<void> {
	await invoke('delete_network_rule', { ruleId });
}

export async function saveGlobalPath(
	path: string,
	accessType: 'read' | 'write'
): Promise<string> {
	return await invoke<string>('save_global_path', { path, accessType });
}

export async function deleteGlobalPath(id: string): Promise<void> {
	await invoke('delete_global_path', { id });
}

export async function respondPermissionRequest(
	requestId: string,
	decision: 'allow_once' | 'allow_permanent' | 'deny'
): Promise<void> {
	await invoke('respond_permission_request', { requestId, decision });
}

export function onPermissionRequest(callback: (req: PermissionRequest) => void) {
	return listen<PermissionRequest>('permission-request', (event) => {
		callback(event.payload);
	});
}

// ── Packages API ──────────────────────────────────────────────────────────

export interface SkillPackage {
	meta: {
		id: string;
		name: string;
		version: string;
		description: string;
		author: string;
		category: string;
		tags: string[];
	};
	config_schema: ConfigField[];
	permissions: {
		primitives: string[];
		allowed_hosts: string[];
	};
}

export interface ConfigField {
	key: string;
	label: string;
	field_type: string;
	required: boolean;
	default?: string;
	hint?: string;
	options?: string[];
}

export async function listPackages(): Promise<SkillPackage[]> {
	return await invoke<SkillPackage[]>('list_packages');
}

export async function getPackageConfig(id: string): Promise<Record<string, string>> {
	return await invoke<Record<string, string>>('get_package_config', { id });
}

export async function setPackageConfig(id: string, key: string, value: string): Promise<void> {
	await invoke('set_package_config', { id, key, value });
}

export async function uninstallPackage(id: string): Promise<void> {
	await invoke('uninstall_package', { id });
}

