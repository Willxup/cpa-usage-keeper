import type { GeminiKeyConfig, OpenAIProviderConfig, ProviderKeyConfig } from '@/types';
import type { CredentialInfo } from '@/types/sourceInfo';

interface SourceInfoInput {
  geminiApiKeys: GeminiKeyConfig[];
  claudeApiKeys: ProviderKeyConfig[];
  codexApiKeys: ProviderKeyConfig[];
  vertexApiKeys: ProviderKeyConfig[];
  openaiCompatibility: OpenAIProviderConfig[];
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
      if (item.apiKey) map.set(item.apiKey, { name: label, type });
      if (item.prefix) map.set(item.prefix, { name: label, type });
    });
  };

  addProviderEntries(geminiApiKeys, 'gemini');
  addProviderEntries(claudeApiKeys, 'claude');
  addProviderEntries(codexApiKeys, 'codex');
  addProviderEntries(vertexApiKeys, 'vertex');

  openaiCompatibility.forEach((provider, index) => {
    const label = provider.prefix?.trim() || provider.name?.trim() || `openai #${index + 1}`;
    if (provider.prefix) map.set(provider.prefix, { name: label, type: 'openai' });
    provider.apiKeyEntries?.forEach((entry) => {
      if (entry.apiKey) map.set(entry.apiKey, { name: label, type: 'openai' });
    });
  });

  return map;
}

export function resolveSourceDisplay(
  sourceRaw: string,
  authIndex: unknown,
  sourceInfoMap: Map<string, CredentialInfo>,
  authFileMap: Map<string, CredentialInfo>
): { displayName: string; type: string } {
  const sourceKey = sourceRaw?.trim();
  if (sourceKey && sourceInfoMap.has(sourceKey)) {
    const source = sourceInfoMap.get(sourceKey)!;
    return { displayName: source.name, type: source.type };
  }

  const normalizedAuthIndex =
    authIndex === null || authIndex === undefined || String(authIndex).trim() === ''
      ? ''
      : String(authIndex).trim();
  if (normalizedAuthIndex && authFileMap.has(normalizedAuthIndex)) {
    const source = authFileMap.get(normalizedAuthIndex)!;
    return { displayName: source.name, type: source.type };
  }

  return { displayName: sourceKey || '-', type: '' };
}
