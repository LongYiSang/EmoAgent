import { arrayField, formatTime, stringField } from '../../shared/lib/data';
import { classNames } from '../../shared/lib/classNames';
import { Avatar } from '../../shared/components/Avatar';
import type { ApprovalRequest } from '../protocol/wsTypes';

export function ApprovalCard({ item, pending, sending, onAction, onDismiss }: {
  item: ApprovalRequest;
  pending: boolean;
  sending: boolean;
  onAction: (id: string, action: string, optionID?: string) => void;
  onDismiss: (id: string) => void;
}) {
  const id = stringField(item, 'id');
  const status = stringField(item, 'status') || 'pending';
  const consumed = status !== 'pending';
  const options = arrayField(item, 'options');
  const rejectID = stringField(item, 'reject_option_id') || stringField(item, 'rejectOptionID');
  const selectedID = stringField(item, 'selected_option_id') || stringField(item, 'selectedOptionID');
  const selected = options.find(option => stringField(option, 'id') === selectedID);
  const statusLabel = status === 'approved' ? '已批准' : status === 'rejected' ? '已拒绝' : consumed ? '已处理' : '等待审批';
  return (
    <div className="msg assistant msg-approval">
      <Avatar role="emotion" />
      <div className={classNames('approval', consumed && 'consumed settled')}>
        <div className="approval-top">
          <div className="approval-tag"><span className="glyph">{consumed ? '✓' : '!'}</span>{consumed ? '审批已完成' : '需要人工审批'}</div>
          <div className="approval-meta-row">
            <span className="approval-expire">有效期至：{formatTime(stringField(item, 'expires_at') || stringField(item, 'expiresAt'))}</span>
            <span className="approval-badge">{statusLabel}</span>
            {consumed && <button className="approval-close" type="button" onClick={() => onDismiss(id)}>×</button>}
          </div>
        </div>
        <div className="approval-body">
          <div className="approval-title">{stringField(item, 'goal_summary') || stringField(item, 'goalSummary') || '需要人工审批'}</div>
          {consumed ? (
            <div className="approval-summary">{selected ? `${statusLabel} · ${stringField(selected, 'summary') || selectedID}` : statusLabel}</div>
          ) : (
            <>
              <ApprovalQuestion item={item} />
              {!!options.length && (
                <div className="approval-opts">
                  {options.map(option => <div className="approval-opt" key={stringField(option, 'id')}><span>{stringField(option, 'summary')}</span><span className="opt-key">{stringField(option, 'id')}</span></div>)}
                </div>
              )}
              <div className="approval-actions">
                {rejectID && <button className="approval-btn deny" type="button" disabled={pending || sending} onClick={() => onAction(id, 'reject', '')}>拒绝</button>}
                {options.filter(option => stringField(option, 'id') !== rejectID).map(option => {
                  const optionID = stringField(option, 'id');
                  return <button className="approval-btn allow" type="button" key={optionID} disabled={pending || sending} onClick={() => onAction(id, 'approve', optionID)}>批准 {stringField(option, 'summary') || optionID}</button>;
                })}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function ApprovalQuestion({ item }: { item: ApprovalRequest }) {
  const lines = String(stringField(item, 'question') || '').split(/\r?\n/).map(line => line.trim()).filter(Boolean);
  const reason = stringField(item, 'recommendation_reason') || stringField(item, 'recommendationReason');
  return (
    <>
      {lines.map((line, index) => {
        if (line.startsWith('命令：') || line.startsWith('调用：')) {
          return <div className="approval-code-wrap" key={index}><div className="approval-note-label">{line.startsWith('命令：') ? '命令' : '调用'}</div><pre className="approval-code">{line.slice(3).trim()}</pre></div>;
        }
        return <div className="approval-q" key={index}>{line}</div>;
      })}
      {reason && <div className="approval-note"><div className="approval-note-label">原因</div><div className="approval-note-text">{reason}</div></div>}
    </>
  );
}
