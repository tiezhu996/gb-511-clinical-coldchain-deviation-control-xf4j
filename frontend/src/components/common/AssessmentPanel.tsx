import FactCheckOutlinedIcon from '@mui/icons-material/FactCheckOutlined';
import HistoryOutlinedIcon from '@mui/icons-material/HistoryOutlined';
import type { ImpactAssessment } from '../../types/domain';
import { StatusBadge } from './StatusBadge';
import { EvidenceList } from './EvidenceList';
import { formatDate } from '../../utils/format';

export function AssessmentPanel({ current, history }: { current?: ImpactAssessment | null; history: ImpactAssessment[] }) {
  const previous = history.filter((item) => item.assessmentVersion !== current?.assessmentVersion);
  return <section className="assessment-panel">
    <header><span><FactCheckOutlinedIcon />影响评估版本</span>{current
      ? <span className="assessment-version-tag">当前 V{current.assessmentVersion}</span>
      : <StatusBadge status="pending" />}</header>
    {current ? <>
      <p>{current.summary}</p>
      <dl>
        <div><dt>影响等级</dt><dd>{current.impactLevel || '-'}</dd></div>
        <div><dt>峰值温度</dt><dd>{current.observedTempC} C</dd></div>
        <div><dt>持续时长</dt><dd>{current.durationMinutes} 分钟</dd></div>
        <div><dt>评估人</dt><dd>{current.evaluatedBy}</dd></div>
        <div><dt>评估时间</dt><dd>{formatDate(current.evaluatedAt)}</dd></div>
      </dl>
      {current.stabilityConclusion && <p className="assessment-conclusion">{current.stabilityConclusion}</p>}
      <EvidenceList evidence={current.sensorEvidence} />
    </> : <p className="muted">偏差完成影响评估后在此生成当前版本；退回重审后旧版本转为历史。</p>}
    {previous.length > 0 && <div className="assessment-history">
      <h4><HistoryOutlinedIcon />历史评估版本（{previous.length}）</h4>
      {previous.map((item) => <div key={item.id} className="assessment-history-row">
        <div className="assessment-history-head"><strong>V{item.assessmentVersion}</strong><StatusBadge status={item.status} /><small>{formatDate(item.evaluatedAt)} · {item.evaluatedBy}</small></div>
        <p>{item.summary}</p>
        {item.supersedeReason && <small className="assessment-supersede">退回重审：{item.supersedeReason}（{item.supersededBy} · {item.supersededAt ? formatDate(item.supersededAt) : '-'}）</small>}
      </div>)}
    </div>}
  </section>;
}
