
import { useEffect, useMemo, useState } from 'react';
import Button from '@mui/material/Button';
import AddAlertOutlinedIcon from '@mui/icons-material/AddAlertOutlined';
import TaskAltOutlinedIcon from '@mui/icons-material/TaskAltOutlined';
import UndoOutlinedIcon from '@mui/icons-material/UndoOutlined';
import { useExcursionEventStore } from '../stores/excursion-event';
import { useDispositionDecisionStore } from '../stores/disposition-decision';
import { useTemperatureWindowStore } from '../stores/temperature-window';
import { useSensorEvidenceStore } from '../stores/sensor-evidence';
import { registerSensorEvidence } from '../api/sensor-evidence';
import type { DomainRecord } from '../types/domain';
import { roleAtLeast } from '../types/domain';
import { getSession } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { MetricCard } from '../components/common/MetricCard';
import { StatusBadge } from '../components/common/StatusBadge';
import { TemperatureBadge } from '../components/common/TemperatureBadge';
import { EvidenceList } from '../components/common/EvidenceList';
import { DecisionPanel } from '../components/common/DecisionPanel';
import { ConfirmDialog } from '../components/common/ConfirmDialog';
import { assessmentVersionLabel, formatDate } from '../utils/format';

function nextExcursionState(item: DomainRecord) {
  if (item.status === 'open') return 'in_review';
  if (item.status === 'in_review') return 'decided';
  if (item.status === 'decided') return 'closed';
  return '';
}

function transitionReason(item: DomainRecord, state: string) {
  if (state === 'in_review') return item.status === 'decided' ? '评估结论退回重审，当前版本决定转入历史' : '质量复核员接收偏差并核对传感器曲线';
  if (state === 'decided') return '传感器证据完整，偏差影响评估完成';
  return '关联处置决定已完成';
}

export default function ExcursionEventPage() {
  const excursions = useExcursionEventStore();
  const dispositions = useDispositionDecisionStore();
  const windows = useTemperatureWindowStore();
  const evidenceStore = useSensorEvidenceStore();
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [pending, setPending] = useState<{ item: DomainRecord; state: string } | null>(null);
  const session = getSession();
  const canReport = roleAtLeast(session?.role, 'operator');
  const canReview = roleAtLeast(session?.role, 'reviewer');
  const refresh = async () => { await Promise.all([excursions.load('excursions'), dispositions.load('dispositions'), windows.load('windows'), evidenceStore.load()]); };
  useEffect(() => { void refresh(); }, [excursions.load, dispositions.load, windows.load, evidenceStore.load]);
  usePolling(refresh, 15000);
  useEffect(() => { if (selectedId === null && excursions.items[0]) setSelectedId(excursions.items[0].id); }, [excursions.items, selectedId]);
  const selected = excursions.items.find((item) => item.id === selectedId) || null;
  const decisionCandidates = useMemo(() => selected ? dispositions.items.filter((item) => (item.excursionCode || item.relatedCode) === selected.code) : [], [dispositions.items, selected]);
  const decision = decisionCandidates.find((item) => !item.invalidatedAt && (item.assessmentVersion ?? 0) === (selected?.assessmentVersion ?? 0)) || decisionCandidates[0] || null;
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
    await excursions.transition('excursions', pending.item, pending.state, transitionReason(pending.item, pending.state), pending.item.sensorEvidence || pending.item.evidence);
    setPending(null);
  };
  const isReturn = Boolean(pending && pending.item.status === 'decided' && pending.state === 'in_review');
  return <main className="workspace"><header className="page-header"><div><p className="eyebrow">EXCURSION RESPONSE</p><h1>偏差处理</h1><p>关联运输容器、温控规则和原始传感器证据，完成影响评估。</p></div>{canReport && <Button variant="contained" startIcon={<AddAlertOutlinedIcon />} onClick={() => setCreateOpen(true)}>登记偏差</Button>}</header>
    <section className="metrics"><MetricCard label="偏差事件" value={excursions.meta.total} detail="全量可追溯" /><MetricCard label="待闭环" value={pendingCount} detail="待质量评估" /><MetricCard label="严重偏差" value={critical} detail="优先隔离" /></section>
    {(excursions.error || evidenceStore.error) && <div className="alert" role="alert">{excursions.error || evidenceStore.error}</div>}
    <section className="split-workspace"><div className="record-list">{excursions.items.map((item) => { const rule = windows.items.find((window) => window.code === item.windowCode); return <button key={item.id} className={selectedId === item.id ? 'record-row selected' : 'record-row'} onClick={() => setSelectedId(item.id)}><span><strong>{item.code}</strong><small>{item.containerCode || item.relatedCode} · {item.windowCode || '未绑定规则'} · 评估 {assessmentVersionLabel(item.assessmentVersion)}</small></span><TemperatureBadge value={item.observedTempC ?? item.metricValue} minimum={rule?.minimumCelsius} maximum={rule?.maximumCelsius} /><StatusBadge status={item.status} /></button>; })}</div>
      <aside className="detail-pane">{selected ? <><header><div><small>{selected.code}</small><h2>{selected.name}</h2></div><StatusBadge status={selected.status} /></header><div className="detail-grid"><span><small>运输容器</small>{selected.containerCode || selected.relatedCode}</span><span><small>温控规则</small>{selected.windowCode || '-'}</span><span><small>持续时长</small>{selected.durationMinutes || 0} 分钟</span><span><small>检测时间</small>{formatDate(selected.detectedAt || selected.effectiveAt)}</span><span><small>当前评估版本</small>{assessmentVersionLabel(selected.assessmentVersion)}</span></div><h3>传感器证据</h3><EvidenceList evidence={selectedEvidence.length ? selectedEvidence : selected.sensorEvidence || selected.evidence} /><DecisionPanel decision={decision} compact currentVersion={selected.assessmentVersion} />{canReview && nextExcursionState(selected) && <Button variant="contained" startIcon={<TaskAltOutlinedIcon />} onClick={() => setPending({ item: selected, state: nextExcursionState(selected) })}>{selected.status === 'open' ? '接收复核' : selected.status === 'in_review' ? '完成影响评估' : '关闭事件'}</Button>}{canReview && selected.status === 'decided' && <Button color="warning" variant="outlined" startIcon={<UndoOutlinedIcon />} onClick={() => setPending({ item: selected, state: 'in_review' })}>退回重审</Button>}</> : <div className="empty">选择一个偏差事件</div>}</aside></section>
    <ConfirmDialog open={createOpen} title="登记温度偏差" onCancel={() => setCreateOpen(false)} onConfirm={() => void createExcursion()}><p>将保存容器、温控规则、峰值温度、持续时长和 MinIO 传感器证据。</p></ConfirmDialog>
    <ConfirmDialog open={Boolean(pending)} title={isReturn ? '确认退回重审' : '确认偏差状态迁移'} onCancel={() => setPending(null)} onConfirm={() => void transition()}>{isReturn ? <><p>退回后当前评估版本 v{pending?.item.assessmentVersion} 的处置决定将全部失效，仅作历史留痕，不能再次闭环；重新评估会生成新版本，需按新版本新建决定并独立批准。</p><strong>{pending?.item.status} → {pending?.state}</strong></> : <><p>偏差不能跳过复核；形成影响评估时必须存在传感器证据，并生成新的评估版本。</p><strong>{pending?.item.status} → {pending?.state}</strong></>}</ConfirmDialog>
  </main>;
}
