
import { useEffect, useMemo, useState } from 'react';
import Button from '@mui/material/Button';
import AddAlertOutlinedIcon from '@mui/icons-material/AddAlertOutlined';
import TaskAltOutlinedIcon from '@mui/icons-material/TaskAltOutlined';
import ReplayOutlinedIcon from '@mui/icons-material/ReplayOutlined';
import { useExcursionEventStore } from '../stores/excursion-event';
import { useDispositionDecisionStore } from '../stores/disposition-decision';
import { useTemperatureWindowStore } from '../stores/temperature-window';
import { useSensorEvidenceStore } from '../stores/sensor-evidence';
import { registerSensorEvidence } from '../api/sensor-evidence';
import { listImpactAssessments, listExcursionDecisions } from '../api/excursion-event';
import type { DomainRecord, ImpactAssessment } from '../types/domain';
import { roleAtLeast } from '../types/domain';
import { getSession } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { MetricCard } from '../components/common/MetricCard';
import { StatusBadge } from '../components/common/StatusBadge';
import { TemperatureBadge } from '../components/common/TemperatureBadge';
import { EvidenceList } from '../components/common/EvidenceList';
import { AssessmentPanel } from '../components/common/AssessmentPanel';
import { DecisionPanel } from '../components/common/DecisionPanel';
import { ConfirmDialog } from '../components/common/ConfirmDialog';
import { formatDate } from '../utils/format';

function nextExcursionState(item: DomainRecord) {
  if (item.status === 'open') return 'in_review';
  if (item.status === 'in_review') return 'decided';
  if (item.status === 'decided') return 'closed';
  return '';
}

export default function ExcursionEventPage() {
  const excursions = useExcursionEventStore();
  const dispositions = useDispositionDecisionStore();
  const windows = useTemperatureWindowStore();
  const evidenceStore = useSensorEvidenceStore();
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [pending, setPending] = useState<{ item: DomainRecord; state: string } | null>(null);
  const [assessments, setAssessments] = useState<ImpactAssessment[]>([]);
  const [relatedDecisions, setRelatedDecisions] = useState<DomainRecord[]>([]);
  const session = getSession();
  const canReport = roleAtLeast(session?.role, 'operator');
  const canReview = roleAtLeast(session?.role, 'reviewer');
  const refresh = async () => { await Promise.all([excursions.load('excursions'), dispositions.load('dispositions'), windows.load('windows'), evidenceStore.load()]); };
  useEffect(() => { void refresh(); }, [excursions.load, dispositions.load, windows.load, evidenceStore.load]);
  usePolling(refresh, 15000);
  useEffect(() => { if (selectedId === null && excursions.items[0]) setSelectedId(excursions.items[0].id); }, [excursions.items, selectedId]);
  const selected = excursions.items.find((item) => item.id === selectedId) || null;
  useEffect(() => {
    if (!selected) { setAssessments([]); setRelatedDecisions([]); return; }
    let cancelled = false;
    void Promise.all([listImpactAssessments(selected.id), listExcursionDecisions(selected.id)])
      .then(([assessmentResult, decisionResult]) => {
        if (cancelled) return;
        setAssessments(assessmentResult.data || []);
        setRelatedDecisions(decisionResult.data || []);
      })
      .catch(() => { if (!cancelled) { setAssessments([]); setRelatedDecisions([]); } });
    return () => { cancelled = true; };
  }, [selectedId, excursions.items.length, selected?.version, selected?.status]);
  const currentVersion = selected?.currentAssessmentVersion ?? null;
  const currentAssessment = useMemo(() => assessments.find((item) => item.status === 'current') || assessments[0] || null, [assessments]);
  const currentDecision = relatedDecisions.find((item) => item.assessmentVersion != null && item.assessmentVersion === currentVersion && !item.invalidatedReason) || null;
  const historicalDecisions = relatedDecisions.filter((item) => item.invalidatedReason || (item.assessmentVersion != null && currentVersion != null && item.assessmentVersion !== currentVersion));
  const selectedEvidence = selected ? evidenceStore.items.filter((item) => item.excursionCode === selected.code).map((item) => `${item.code} · ${item.objectKey} · SHA256 ${item.sha256.slice(0, 10)}…`) : [];
  const critical = useMemo(() => excursions.items.filter((item) => item.riskLevel === 'critical').length, [excursions.items]);
  const pendingCount = useMemo(() => excursions.items.filter((item) => ['open', 'in_review'].includes(item.status)).length, [excursions.items]);
  const createExcursion = async () => {
    const suffix = Date.now().toString().slice(-5); const now = new Date().toISOString();
    const code = `EE-UI-${suffix}`; const objectKey = `sensor/tc-002/${code.toLowerCase()}.csv`;
    await excursions.createRecord('excursions', { code, name: 'TC-002 温度越界告警', description: '内置工作台登记的传感器高温偏差', facility: '沪杭运输线', owner: '未分配', category: '高温偏差', riskLevel: 'high', metricValue: 9.3, metricUnit: 'C', effectiveAt: now, evidence: `minio://clinical-evidence/${objectKey}`, relatedCode: 'TC-002', containerCode: 'TC-002', windowCode: 'TW-001', observedTempC: 9.3, durationMinutes: 18, detectedAt: now, sensorEvidence: `minio://clinical-evidence/${objectKey}` });
    await registerSensorEvidence({ code: `SE-${suffix}`, excursionCode: code, containerCode: 'TC-002', objectKey, sha256: 'd'.repeat(64), mediaType: 'text/csv', sizeBytes: 2048, capturedAt: now, source: 'ui-logger-import' });
    await evidenceStore.load();
    setCreateOpen(false);
  };
  const transition = async () => {
    if (!pending) return;
    const reason = pending.state === 'in_review'
      ? (pending.item.status === 'decided' ? '质量复核发现新的影响因素，偏差退回重审' : '质量复核员接收偏差并核对传感器曲线')
      : pending.state === 'decided' ? '传感器证据完整，偏差影响评估完成' : '关联处置决定已完成';
    await excursions.transition('excursions', pending.item, pending.state, reason, pending.item.sensorEvidence || pending.item.evidence);
    setPending(null);
  };
  const nextState = selected ? nextExcursionState(selected) : '';
  const closeReady = selected?.status === 'decided' && Boolean(currentDecision && ['release', 'quarantine', 'discard'].includes(currentDecision.status));
  return <main className="workspace"><header className="page-header"><div><p className="eyebrow">EXCURSION RESPONSE</p><h1>偏差处理</h1><p>关联运输容器、温控规则和原始传感器证据，按影响评估版本完成处置闭环。</p></div>{canReport && <Button variant="contained" startIcon={<AddAlertOutlinedIcon />} onClick={() => setCreateOpen(true)}>登记偏差</Button>}</header>
    <section className="metrics"><MetricCard label="偏差事件" value={excursions.meta.total} detail="全量可追溯" /><MetricCard label="待闭环" value={pendingCount} detail="待质量评估" /><MetricCard label="严重偏差" value={critical} detail="优先隔离" /></section>
    {(excursions.error || evidenceStore.error) && <div className="alert" role="alert">{excursions.error || evidenceStore.error}</div>}
    <section className="split-workspace"><div className="record-list">{excursions.items.map((item) => { const rule = windows.items.find((window) => window.code === item.windowCode); return <button key={item.id} className={selectedId === item.id ? 'record-row selected' : 'record-row'} onClick={() => setSelectedId(item.id)}><span><strong>{item.code}</strong><small>{item.containerCode || item.relatedCode} · {item.windowCode || '未绑定规则'}{item.currentAssessmentVersion ? ` · V${item.currentAssessmentVersion}` : ''}</small></span><TemperatureBadge value={item.observedTempC ?? item.metricValue} minimum={rule?.minimumCelsius} maximum={rule?.maximumCelsius} /><StatusBadge status={item.status} /></button>; })}</div>
      <aside className="detail-pane">{selected ? <><header><div><small>{selected.code}</small><h2>{selected.name}</h2></div><StatusBadge status={selected.status} /></header>
        <div className="detail-grid"><span><small>运输容器</small>{selected.containerCode || selected.relatedCode}</span><span><small>温控规则</small>{selected.windowCode || '-'}</span><span><small>持续时长</small>{selected.durationMinutes || 0} 分钟</span><span><small>检测时间</small>{formatDate(selected.detectedAt || selected.effectiveAt)}</span><span><small>当前评估版本</small>{selected.currentAssessmentVersion ? `V${selected.currentAssessmentVersion}` : '尚未评估'}</span><span><small>评估版本数</small>{assessments.length || 0}</span></div>
        <h3>传感器证据</h3><EvidenceList evidence={selectedEvidence.length ? selectedEvidence : selected.sensorEvidence || selected.evidence} />
        <AssessmentPanel current={currentAssessment} history={assessments} />
        <DecisionPanel decision={currentDecision} currentAssessmentVersion={currentVersion} compact />
        {historicalDecisions.length > 0 && <div className="decision-history"><h4>历史处置决定（{historicalDecisions.length}）</h4>{historicalDecisions.map((decision) => <DecisionPanel key={decision.id} decision={decision} currentAssessmentVersion={currentVersion} compact />)}</div>}
        {selected.status === 'decided' && !closeReady && <div className="alert alert--inline" role="note">关闭偏差需要与当前评估版本 V{currentVersion ?? '-'} 一致、且经另一人批准的最终放行/隔离/报废决定；不符的旧决定仅作历史。</div>}
        {canReview && nextState && <div className="detail-actions-row">
          {selected.status === 'decided' && <Button color="warning" variant="outlined" startIcon={<ReplayOutlinedIcon />} onClick={() => setPending({ item: selected, state: 'in_review' })}>退回重审</Button>}
          {(!(selected.status === 'decided') || closeReady) && <Button variant="contained" startIcon={<TaskAltOutlinedIcon />} onClick={() => setPending({ item: selected, state: nextState })}>{selected.status === 'open' ? '接收复核' : selected.status === 'in_review' ? '完成影响评估（生成新版本）' : '关闭事件'}</Button>}
        </div>}</> : <div className="empty">选择一个偏差事件</div>}</aside></section>
    <ConfirmDialog open={createOpen} title="登记温度偏差" onCancel={() => setCreateOpen(false)} onConfirm={() => void createExcursion()}><p>将保存容器、温控规则、峰值温度、持续时长和 MinIO 传感器证据。</p></ConfirmDialog>
    <ConfirmDialog open={Boolean(pending)} title={pending?.state === 'in_review' && pending.item.status === 'decided' ? '确认退回重审' : '确认偏差状态迁移'} onCancel={() => setPending(null)} onConfirm={() => void transition()}>{pending?.state === 'in_review' && pending.item.status === 'decided'
      ? <><p>退回后当前评估版本与已批准决定仅作历史，不能再次闭环；重新评估会生成新版本，需要新建决定并独立批准。</p><strong>{pending?.item.status} → in_review · 当前 V{pending?.item.currentAssessmentVersion ?? '-'}</strong></>
      : <><p>偏差不能跳过复核；形成影响评估时会生成新的评估版本，且必须存在传感器证据。</p><strong>{pending?.item.status} → {pending?.state}</strong></>}</ConfirmDialog>
  </main>;
}
