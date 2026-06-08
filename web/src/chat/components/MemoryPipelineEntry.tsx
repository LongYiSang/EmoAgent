export function MemoryPipelineEntry({ snapshot, onOpen }: { snapshot: unknown; onOpen: (snapshot: unknown) => void }) {
  return (
    <div className="memory-pipeline-entry">
      <div className="memory-pipeline-av">记</div>
      <div className="memory-pipeline-card"><div className="memory-pipeline-title">记忆管线快照</div><button className="memory-pipeline-btn" type="button" onClick={() => onOpen(snapshot)}>记忆管线</button></div>
    </div>
  );
}
