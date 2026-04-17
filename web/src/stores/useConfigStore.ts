import { create } from 'zustand';
import type { GeminiKeyConfig, OpenAIProviderConfig, ProviderKeyConfig } from '@/types';

export interface ConfigStateShape {
  geminiApiKeys: GeminiKeyConfig[];
  claudeApiKeys: ProviderKeyConfig[];
  codexApiKeys: ProviderKeyConfig[];
  vertexApiKeys: ProviderKeyConfig[];
  openaiCompatibility: OpenAIProviderConfig[];
}

interface ConfigState {
  config: ConfigStateShape;
}

const emptyConfig: ConfigStateShape = {
  geminiApiKeys: [],
  claudeApiKeys: [],
  codexApiKeys: [],
  vertexApiKeys: [],
  openaiCompatibility: []
};

export const useConfigStore = create<ConfigState>(() => ({
  config: emptyConfig
}));
