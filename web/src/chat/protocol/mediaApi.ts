import { requestJSON } from '../../shared/lib/api';

export type UploadedMedia = {
  media_asset_id: string;
  kind: 'image' | string;
  mime_type: string;
  byte_size: number;
  width?: number;
  height?: number;
  original_filename?: string;
};

export async function uploadMedia(file: File): Promise<UploadedMedia> {
  const form = new FormData();
  form.set('file', file);
  return requestJSON<UploadedMedia>('/api/media', {
    method: 'POST',
    body: form,
  });
}
