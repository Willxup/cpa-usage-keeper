import type { GeminiKeyConfig, OpenAIProviderConfig, ProviderKeyConfig } from '@/types';
import type { CredentialInfo } from '@/types/sourceInfo';

interface SourceInfoInput {
  geminiApiKeys: GeminiKeyConfig[];
  claudeApiKeys: ProviderKeyConfig[];
  codexApiKeys: ProviderKeyConfig[];
  vertexApiKeys: ProviderKeyConfig[];
  openaiCompatibility: OpenAIProviderConfig[];
}

export interface ResolvedSourceDisplay {
  displayName: string;
  type: string;
  key: string;
}

function maskValue(value: string): string {
  const normalized = value.trim();
  if (!normalized || normalized === '-') return '-';
  if (normalized.length <= 4) return '*'.repeat(normalized.length);
  if (normalized.length <= 8) return `${normalized.slice(0, 1)}${'*'.repeat(normalized.length - 2)}${normalized.slice(-1)}`;
  return `${normalized.slice(0, 4)}${'*'.repeat(normalized.length - 8)}${normalized.slice(-4)}`;
}

function looksLikeEmail(value: string): boolean {
  const normalized = value.trim();
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalized);
}

function inferProviderType(sourceKey: string): string {
  const value = sourceKey.trim().toLowerCase();
  if (!value) return '';
  if (value.startsWith('sk-ant-') || value.includes('anthropic') || value.includes('claude')) return 'claude';
  if (value.startsWith('sk-proj-') || value.startsWith('sk-') || value.includes('openai') || value.includes('gpt')) return 'openai';
  if (value.startsWith('aiza') || value.includes('gemini')) return 'gemini';
  if (value.includes('vertex')) return 'vertex';
  if (value.includes('codex')) return 'codex';
  if (value.includes('ampcode')) return 'ampcode';
  return '';
}

function buildFallbackSource(sourceKey: string): ResolvedSourceDisplay {
  if (looksLikeEmail(sourceKey)) {
    return {
      displayName: sourceKey,
      type: '',
      key: `email:${sourceKey}`,
    };
  }

  const inferredType = inferProviderType(sourceKey);
  if (inferredType) {
    return {
      displayName: inferredType,
      type: inferredType,
      key: `provider:fallback:${inferredType}`,
    };
  }

  const masked = maskValue(sourceKey || '-');
  return {
    displayName: masked,
    type: '',
    key: `raw:${masked}`,
  };
}

export function buildSourceInfoMap({
  geminiApiKeys,
  claudeApiKeys,
  codexApiKeys,
  vertexApiKeys,
  openaiCompatibility
}: SourceInfoInput): Map<string, CredentialInfo> {
  const map = new Map<string, CredentialInfo>();

  const addProviderEntries = (items: Array<ProviderKeyConfig | GeminiKeyConfig>, type: string) => {
    items.forEach((item, index) => {
      const label = item.prefix?.trim() || item.name?.trim() || `${type} #${index + 1}`;
      const key = `${type}:${label}`;
      if (item.apiKey) map.set(item.apiKey, { name: label, type, key });
      if (item.prefix) map.set(item.prefix, { name: label, type, key });
    });
  };

  addProviderEntries(geminiApiKeys, 'gemini');
  addProviderEntries(claudeApiKeys, 'claude');
  addProviderEntries(codexApiKeys, 'codex');
  addProviderEntries(vertexApiKeys, 'vertex');

  openaiCompatibility.forEach((provider, index) => {
    const label = provider.name?.trim() || provider.prefix?.trim() || `openai #${index + 1}`;
    const key = `openai:${label}`;
    if (provider.prefix) map.set(provider.prefix, { name: label, type: 'openai', key });
    provider.apiKeyEntries?.forEach((entry) => {
      if (entry.apiKey) map.set(entry.apiKey, { name: label, type: 'openai', key });
    });
  });

  return map;
}

export function resolveSourceDisplay(
  sourceRaw: string,
  authIndex: unknown,
  sourceInfoMap: Map<string, CredentialInfo>,
  authFileMap: Map<string, CredentialInfo>,
  providerMetadataMap?: Map<string, CredentialInfo>
): ResolvedSourceDisplay {
  const sourceKey = sourceRaw?.trim();
  const normalizedAuthIndex =
    authIndex === null || authIndex === undefined || String(authIndex).trim() === ''
      ? ''
      : String(authIndex).trim();

  if (normalizedAuthIndex && authFileMap.has(normalizedAuthIndex)) {
    const source = authFileMap.get(normalizedAuthIndex)!;
    return {
      displayName: source.name,
      type: source.type,
      key: source.key || `auth:${normalizedAuthIndex}`,
    };
  }

  if (sourceKey && providerMetadataMap?.has(sourceKey)) {
    const source = providerMetadataMap.get(sourceKey)!;
    return {
      displayName: source.name,
      type: source.type,
      key: source.key || `provider:${source.type}:${source.name}`,
    };
  }

  if (sourceKey && sourceInfoMap.has(sourceKey)) {
    const source = sourceInfoMap.get(sourceKey)!;
    return {
      displayName: source.name,
      type: source.type,
      key: source.key || `provider:${source.type}:${source.name}`,
    };
  }

  return buildFallbackSource(sourceKey || '-');
}
