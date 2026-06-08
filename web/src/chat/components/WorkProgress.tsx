export function WorkProgress({ content }: { content: string }) {
  return (
    <div className="progress">
      <div className="progress-av">工</div>
      <div className="progress-card"><div className="progress-label"><span className="sparkle" />正在处理...</div><div className="progress-text message-content">{content}</div></div>
    </div>
  );
}
