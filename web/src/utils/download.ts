interface DownloadBlobOptions {
  filename: string;
  blob: Blob;
}

export function downloadBlob({ filename, blob }: DownloadBlobOptions): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}
