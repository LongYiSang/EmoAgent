import { requestJSON } from '../../shared/lib/api';

export type MemorySegment = {
  id?: string;
  ID?: string;
  segment_index?: number;
  SegmentIndex?: number;
  extraction_status?: string;
  ExtractionStatus?: string;
  status?: string;
  Status?: string;
};

export type MemoryJob = {
  id?: string;
  ID?: string;
  trigger?: string;
  Trigger?: string;
  status?: string;
  Status?: string;
  extraction_status?: string;
  ExtractionStatus?: string;
};

export async function loadMemoryStatus(sessionID: string): Promise<{ segments: MemorySegment[]; jobs: MemoryJob[] }> {
  if (!sessionID) return { segments: [], jobs: [] };
  const [segments, jobs] = await Promise.all([
    requestJSON<{ segments?: MemorySegment[] }>(`/api/memory/segments?session_id=${encodeURIComponent(sessionID)}`),
    requestJSON<{ jobs?: MemoryJob[] }>(`/api/memory/extractions?session_id=${encodeURIComponent(sessionID)}&limit=10`),
  ]);
  return { segments: segments.segments || [], jobs: jobs.jobs || [] };
}

export async function queueMemoryExtraction(sessionID: string): Promise<void> {
  await requestJSON('/api/memory/extractions', {
    method: 'POST',
    body: { session_id: sessionID, scope: 'session', mode: 'apply' },
  });
}
