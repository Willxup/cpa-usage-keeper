export type Theme = 'light' | 'white' | 'dark' | 'auto';

export interface ProviderKeyConfig {
  apiKey: string;
  prefix?: string;
  name?: string;
}

export interface GeminiKeyConfig extends ProviderKeyConfig {}

export interface OpenAIApiKeyEntry {
  apiKey: string;
}

export interface OpenAIProviderConfig {
  name?: string;
  prefix?: string;
  apiKeyEntries?: OpenAIApiKeyEntry[];
}
