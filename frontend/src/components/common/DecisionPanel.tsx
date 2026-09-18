import GavelOutlinedIcon from '@mui/icons-material/GavelOutlined';
import type { DomainRecord } from '../../types/domain';
import { StatusBadge } from './StatusBadge';
import { EvidenceList } from './EvidenceList';
import { assessmentVersionLabel, invalidationReasonLabel } from '../../utils/format';

export function DecisionPanel({ decision, compact = false, currentVersion }: { decision?: DomainRecord | null; compact?: boolean; currentVersion?: number }) {
  const invalidated = Boolean(decision?.invalidatedAt);
  return <section className={`decision-panel ${compact ? 'decision-panel--compact' : ''}`}>
    <header><span><GavelOutlinedIcon />处置决定</span>{decision ? <StatusBadge status={decision.status} /> : <StatusBadge status="pending" />}</header>
    {decision ? <><p>{decision.decisionBasis || decision.description}</p><dl><div><dt>偏差事件</dt><dd>{decision.excursionCode || decision.relatedCode}</dd></div><div><dt>评估版本</dt><dd>{assessmentVersionLabel(decision.assessmentVersion)}{currentVersion !== undefined && currentVersion !== decision.assessmentVersion ? ` · 当前 ${assessmentVersionLabel(currentVersion)}` : ''}</dd></div><div><dt>提议人</dt><dd>{decision.proposedBy || decision.owner}</dd></div><div><dt>复核人</dt><dd>{decision.approvedBy || '待独立复核'}</dd></div></dl>{invalidated && <p className="invalidation-banner" role="note">已失效 · {invalidationReasonLabel(decision.invalidatedReason)}，本决定仅作历史留痕</p>}<EvidenceList evidence={decision.sensorEvidence || decision.evidence} /></> : <p className="muted">尚未形成处置提议。</p>}
  </section>;
}
