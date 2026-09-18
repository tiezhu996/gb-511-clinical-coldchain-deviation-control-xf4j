
import { statusTone } from '../../utils/format';
export function StatusBadge({ status }: { status: string }) {
  const labels: Record<string, string> = { ready: '待命', in_transit: '运输中', quarantine: '隔离', cleared: '已放行', draft: '草稿', active: '生效', expired: '失效', superseded: '历史版本', open: '待处理', in_review: '复核中', decided: '已评估', closed: '已关闭', release: '放行', discard: '报废', pending: '待决定', current: '当前版本', invalidated: '已失效' };
  return <span className={`status status--${statusTone(status)}`}>{labels[status] || status.replaceAll('_', ' ')}</span>;
}
