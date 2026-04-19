import { ApiError } from '@/lib/api';
import type { AuthFileItem } from '@/types/authFile';

export async function fetchAuthFiles(signal?: AbortSignal): Promise<{ files: AuthFileItem[] }> {
  const response = await fetch('/api/v1/auth-files', {
    credentials: 'include',
    signal,
  });
  if (!response.ok) {
    let message = `Failed to load auth files: ${response.status}`;
    try {
      const payload = await response.json() as { error?: string };
      if (payload.error) message = payload.error;
    } catch {
      // ignore invalid error payloads
    }
    throw new ApiError(message, response.status);
  }
  return response.json();
}

export const authFilesApi = {
  list: fetchAuthFiles,
};
