import GavelOutlinedIcon from '@mui/icons-material/GavelOutlined';
import HistoryOutlinedIcon from '@mui/icons-material/HistoryOutlined';
import BlockOutlinedIcon from '@mui/icons-material/BlockOutlined';
import type { DomainRecord } from '../../types/domain';
import { StatusBadge } from './StatusBadge';
import { EvidenceList } from './EvidenceList';
import { formatDate } from '../../utils/format';

export function DecisionPanel({ decision, currentAssessmentVersion, compact = false }: { decision?: DomainRecord | null; currentAssessmentVersion?: number | null; compact?: boolean }) {
  const invalidated = Boolean(decision?.invalidatedReason);
  const stale = !invalidated && decision?.assessmentVersion != null && currentAssessmentVersion != null && decision.assessmentVersion !== currentAssessmentVersion;
  const headerStatus = invalidated ? 'invalidated' : (decision ? decision.status : 'pending');
  return <section className={`decision-panel ${compact ? 'decision-panel--compact' : ''} ${invalidated ? 'decision-panel--invalid' : ''} ${stale ? 'decision-panel--stale' : ''}`}>
    <header><span><GavelOutlinedIcon />处置决定</span>{decision ? <StatusBadge status={headerStatus} /> : <StatusBadge status="pending" />}</header>
    {decision ? <><p>{decision.decisionBasis || decision.description}</p>
      <dl>
        <div><dt>偏差事件</dt><dd>{decision.excursionCode || decision.relatedCode}</dd></div>
        <div><dt>评估版本</dt><dd>{decision.assessmentVersion != null ? `V${decision.assessmentVersion}` : '-'} {decision.assessmentVersion != null && currentAssessmentVersion != null && decision.assessmentVersion === currentAssessmentVersion ? '· 当前' : ''}</dd></div>
        <div><dt>提议人</dt><dd>{decision.proposedBy || decision.owner}</dd></div>
        <div><dt>复核人</dt><dd>{decision.approvedBy || '待独立复核'}</dd></div>
        {decision.decidedAt && <div><dt>决定时间</dt><dd>{formatDate(decision.decidedAt)}</dd></div>}
        {invalidated && decision.invalidatedAt && <div><dt>失效时间</dt><dd>{formatDate(decision.invalidatedAt)}</dd></div>}
      </dl>
      {invalidated && <div className="decision-invalidation"><BlockOutlinedIcon /><div><strong>决定已失效（仅作历史，不能再次闭环）</strong><small>{decision.invalidatedReason}</small></div></div>}
      {stale && <div className="decision-invalidation decision-invalidation--stale"><HistoryOutlinedIcon /><div><strong>引用的评估版本 V{decision.assessmentVersion} 已不是当前版本 V{currentAssessmentVersion}</strong><small>请按新版本新建处置提议并独立批准。</small></div></div>}
      <EvidenceList evidence={decision.sensorEvidence || decision.evidence} /></>
      : <p className="muted">尚未形成处置提议。</p>}
  </section>;
}
