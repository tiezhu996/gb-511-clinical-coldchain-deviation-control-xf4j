
import { useEffect, useMemo, useState } from 'react';
import Button from '@mui/material/Button';
import AddOutlinedIcon from '@mui/icons-material/AddOutlined';
import VerifiedUserOutlinedIcon from '@mui/icons-material/VerifiedUserOutlined';
import BlockOutlinedIcon from '@mui/icons-material/BlockOutlined';
import DeleteSweepOutlinedIcon from '@mui/icons-material/DeleteSweepOutlined';
import { useDispositionDecisionStore } from '../stores/disposition-decision';
import { useExcursionEventStore } from '../stores/excursion-event';
import type { DomainRecord } from '../types/domain';
import { roleAtLeast } from '../types/domain';
import { getSession } from '../api/client';
import { usePolling } from '../hooks/usePolling';
import { MetricCard } from '../components/common/MetricCard';
import { StatusBadge } from '../components/common/StatusBadge';
import { DecisionPanel } from '../components/common/DecisionPanel';
import { ConfirmDialog } from '../components/common/ConfirmDialog';

export default function DispositionDecisionPage() {
  const store = useDispositionDecisionStore();
  const excursions = useExcursionEventStore();
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [target, setTarget] = useState('');
  const session = getSession();
  const canPropose = roleAtLeast(session?.role, 'operator');
  const canApprove = roleAtLeast(session?.role, 'reviewer');
  const loadAll = async () => { await Promise.all([store.load('dispositions'), excursions.load('excursions')]); };
  useEffect(() => { void loadAll(); }, [store.load, excursions.load]);
  usePolling(loadAll, 15000);
  useEffect(() => { if (selectedId === null && store.items[0]) setSelectedId(store.items[0].id); }, [store.items, selectedId]);
  const selected = store.items.find((item) => item.id === selectedId) || null;
  const excursionOf = (item: DomainRecord) => excursions.items.find((excursion) => excursion.code === (item.excursionCode || item.relatedCode)) || null;
  const selectedExcursion = selected ? excursionOf(selected) : null;
  const selectedCurrentVersion = selectedExcursion?.currentAssessmentVersion ?? null;
  const selectedInvalidated = Boolean(selected?.invalidatedReason);
  const selectedStale = !selectedInvalidated && selected?.status === 'draft' && selected?.assessmentVersion != null && selectedCurrentVersion != null && selected.assessmentVersion !== selectedCurrentVersion;
  const drafts = useMemo(() => store.items.filter((item) => item.status === 'draft' && !item.invalidatedReason).length, [store.items]);
  const released = useMemo(() => store.items.filter((item) => item.status === 'release' && !item.invalidatedReason).length, [store.items]);
  const invalidated = useMemo(() => store.items.filter((item) => item.invalidatedReason).length, [store.items]);
  const createProposal = async () => {
    const suffix = Date.now().toString().slice(-5); const now = new Date().toISOString();
    await store.createRecord('dispositions', { code: `DD-UI-${suffix}`, name: 'EE-004 隔离处置提议', description: '8.9C 持续 22 分钟超出允许窗口，建议保持隔离', facility: '质量放行组', owner: session?.username || 'operator', category: '隔离提议', riskLevel: 'high', metricValue: 8.9, metricUnit: 'C', effectiveAt: now, evidence: 'minio://sensor/tc-002/over-window.csv', relatedCode: 'EE-004', excursionCode: 'EE-004', decisionBasis: '超过允许窗口，等待独立复核', sensorEvidence: 'minio://sensor/tc-002/over-window.csv' });
    setCreateOpen(false);
  };
  const approve = async () => {
    if (!selected || !target) return;
    const reason = target === 'release' ? '稳定性评估支持临床使用，批准放行' : target === 'discard' ? '偏差影响不可接受，批准报废' : '证据尚不足，维持物理隔离';
    await store.transition('dispositions', selected, target, reason, selected.sensorEvidence || selected.evidence);
    setTarget('');
  };
  return <main className="workspace"><header className="page-header"><div><p className="eyebrow">QUALITY DISPOSITION</p><h1>处置决定审核</h1><p>处置提议只引用偏差的当前影响评估版本；放行、隔离与报废均需传感器证据，并由不同于提议人的质量角色独立批准。</p></div>{canPropose && <Button variant="contained" startIcon={<AddOutlinedIcon />} onClick={() => setCreateOpen(true)}>新建处置提议</Button>}</header>
    <section className="metrics"><MetricCard label="处置记录" value={store.meta.total} detail="全流程留痕" /><MetricCard label="待复核" value={drafts} detail="需要第二人" /><MetricCard label="已放行" value={released} detail="当前版本有效" /><MetricCard label="已失效" value={invalidated} detail="仅作历史" /></section>
    {store.error && <div className="alert" role="alert">{store.error}</div>}
    <section className="split-workspace"><div className="record-list">{store.items.map((item) => { const excursion = excursionOf(item); const stale = item.invalidatedReason || (item.assessmentVersion != null && excursion?.currentAssessmentVersion != null && item.assessmentVersion !== excursion.currentAssessmentVersion); return <button key={item.id} className={selectedId === item.id ? 'record-row selected' : 'record-row'} onClick={() => setSelectedId(item.id)}><span><strong>{item.code}</strong><small>{item.excursionCode || item.relatedCode} · V{item.assessmentVersion ?? '-'} · 提议人 {item.proposedBy || item.owner}</small>{stale && <small className="record-row-flag">已失效 · 历史版本</small>}</span><StatusBadge status={item.invalidatedReason ? 'invalidated' : item.status} /></button>; })}</div><aside className="detail-pane">{selected ? <>
      <div className="detail-pane-head"><small>{selected.excursionCode || selected.relatedCode} 当前评估版本：V{selectedCurrentVersion ?? '-'}，本决定引用：V{selected.assessmentVersion ?? '-'}</small></div>
      <DecisionPanel decision={selected} currentAssessmentVersion={selectedCurrentVersion} />
      {canApprove && selected.status === 'draft' && !selectedInvalidated && !selectedStale && <div className="decision-actions"><Button startIcon={<VerifiedUserOutlinedIcon />} onClick={() => setTarget('release')}>放行</Button><Button color="warning" startIcon={<BlockOutlinedIcon />} onClick={() => setTarget('quarantine')}>隔离</Button><Button color="error" startIcon={<DeleteSweepOutlinedIcon />} onClick={() => setTarget('discard')}>报废</Button></div>}
      {(selectedInvalidated || selectedStale) && <div className="alert alert--inline" role="note">该决定引用的评估版本已被退回重审取代，不能批准或再次闭环；请按偏差当前版本新建处置决定。</div>}
      <div className="dual-control"><strong>双人复核</strong><span>提议：{selected.proposedBy || '-'}</span><span>批准：{selected.approvedBy || '待不同账号复核'}</span></div></> : <div className="empty">选择一条处置记录</div>}</aside></section>
    <ConfirmDialog open={createOpen} title="创建隔离处置提议" onCancel={() => setCreateOpen(false)} onConfirm={() => void createProposal()}><p>系统会自动把提议钉住偏差当前的影响评估版本，并记录当前登录人为提议人；提议人不能批准自己的处置。</p></ConfirmDialog>
    <ConfirmDialog open={Boolean(target)} title={`确认${target === 'release' ? '放行' : target === 'discard' ? '报废' : '隔离'}决定`} onCancel={() => setTarget('')} onConfirm={() => void approve()}><p>最终决定会保存评估版本、提议人、独立复核人、传感器证据和 request ID；批准时与退回重审并发只会保留一个有效结果。</p><strong>{selected?.code} · {selected?.excursionCode} · V{selected?.assessmentVersion ?? '-'}</strong></ConfirmDialog>
  </main>;
}
