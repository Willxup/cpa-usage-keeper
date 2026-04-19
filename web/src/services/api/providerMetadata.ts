import { ApiError } from '@/lib/api';
import type { ProviderMetadataItem } from '@/types/providerMetadata';

export async function fetchProviderMetadata(signal?: AbortSignal): Promise<{ items: ProviderMetadataItem[] }> {
  const response = await fetch('/api/v1/provider-metadata', {
    credentials: 'include',
    signal,
  });
  if (!response.ok) {
    let message = `Failed to load provider metadata: ${response.status}`;
    try {
      const payload = (await response.json()) as { error?: string };
      if (payload.error) message = payload.error;
    } catch {
      // ignore invalid error payloads
    }
    throw new ApiError(message, response.status);
  }
  return response.json();
}

export const providerMetadataApi = {
  list: fetchProviderMetadata,
};
