
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
import { assessmentVersionLabel } from '../utils/format';

export default function DispositionDecisionPage() {
  const store = useDispositionDecisionStore();
  const excursions = useExcursionEventStore();
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [target, setTarget] = useState('');
  const [hint, setHint] = useState('');
  const session = getSession();
  const canPropose = roleAtLeast(session?.role, 'operator');
  const canApprove = roleAtLeast(session?.role, 'reviewer');
  const refresh = async () => { await Promise.all([store.load('dispositions'), excursions.load('excursions')]); };
  useEffect(() => { void refresh(); }, [store.load, excursions.load]);
  usePolling(refresh, 15000);
  useEffect(() => { if (selectedId === null && store.items[0]) setSelectedId(store.items[0].id); }, [store.items, selectedId]);
  const selected = store.items.find((item) => item.id === selectedId) || null;
  const linked = selected ? excursions.items.find((item) => item.code === (selected.excursionCode || selected.relatedCode)) || null : null;
  const versionCurrent = Boolean(selected && linked && (selected.assessmentVersion ?? 0) === (linked.assessmentVersion ?? 0));
  const approvable = Boolean(canApprove && selected && selected.status === 'draft' && !selected.invalidatedAt && linked && linked.status === 'decided' && versionCurrent && (selected.proposedBy || '').toLowerCase() !== (session?.username || '').toLowerCase());
  const decidedExcursion = useMemo(() => excursions.items.find((item) => item.status === 'decided') || null, [excursions.items]);
  const drafts = useMemo(() => store.items.filter((item) => item.status === 'draft' && !item.invalidatedAt).length, [store.items]);
  const released = useMemo(() => store.items.filter((item) => item.status === 'release' && !item.invalidatedAt).length, [store.items]);
  const invalidatedCount = useMemo(() => store.items.filter((item) => item.invalidatedAt).length, [store.items]);
  const createProposal = async () => {
    if (!decidedExcursion) { setHint('当前没有已评估（decided）状态的偏差，无法新建处置提议。'); setCreateOpen(false); return; }
    setHint('');
    const suffix = Date.now().toString().slice(-5); const now = new Date().toISOString();
    const evidence = decidedExcursion.sensorEvidence || decidedExcursion.evidence;
    await store.createRecord('dispositions', { code: `DD-UI-${suffix}`, name: `${decidedExcursion.code} 处置提议`, description: `针对评估 ${assessmentVersionLabel(decidedExcursion.assessmentVersion)} 的处置提议`, facility: '质量放行组', owner: session?.username || 'operator', category: '处置提议', riskLevel: decidedExcursion.riskLevel, metricValue: decidedExcursion.observedTempC ?? decidedExcursion.metricValue, metricUnit: decidedExcursion.metricUnit || 'C', effectiveAt: now, evidence, relatedCode: decidedExcursion.code, excursionCode: decidedExcursion.code, decisionBasis: '依据当前影响评估版本提出，等待独立复核', sensorEvidence: evidence });
    setCreateOpen(false);
  };
  const approve = async () => {
    if (!selected || !target) return;
    const reason = target === 'release' ? '稳定性评估支持临床使用，批准放行' : target === 'discard' ? '偏差影响不可接受，批准报废' : '证据尚不足，维持物理隔离';
    await store.transition('dispositions', selected, target, reason, selected.sensorEvidence || selected.evidence);
    setTarget('');
  };
  return <main className="workspace"><header className="page-header"><div><p className="eyebrow">QUALITY DISPOSITION</p><h1>处置决定审核</h1><p>放行、隔离与报废均要求传感器证据，并由不同于提议人的质量角色复核。</p></div>{canPropose && <Button variant="contained" startIcon={<AddOutlinedIcon />} onClick={() => setCreateOpen(true)}>新建处置提议</Button>}</header>
    <section className="metrics"><MetricCard label="处置记录" value={store.meta.total} detail="全流程留痕" /><MetricCard label="待复核" value={drafts} detail="需要第二人" /><MetricCard label="已放行" value={released} detail="证据评估通过" /><MetricCard label="已失效" value={invalidatedCount} detail="仅作历史" /></section>
    {(store.error || hint) && <div className="alert" role="alert">{store.error || hint}</div>}
    <section className="split-workspace"><div className="record-list">{store.items.map((item) => <button key={item.id} className={selectedId === item.id ? 'record-row selected' : 'record-row'} onClick={() => setSelectedId(item.id)}><span><strong>{item.code}</strong><small>{item.excursionCode || item.relatedCode} · 评估 {assessmentVersionLabel(item.assessmentVersion)} · 提议人 {item.proposedBy || item.owner}{item.invalidatedAt ? ' · 已失效' : ''}</small></span><StatusBadge status={item.status} /></button>)}</div><aside className="detail-pane">{selected ? <><DecisionPanel decision={selected} currentVersion={linked?.assessmentVersion} />{linked && <div className="dual-control"><strong>版本一致性</strong><span>决定版本：{assessmentVersionLabel(selected.assessmentVersion)}</span><span>偏差当前版本：{assessmentVersionLabel(linked.assessmentVersion)}{versionCurrent ? '（一致）' : '（不一致）'}</span></div>}{approvable && <div className="decision-actions"><Button startIcon={<VerifiedUserOutlinedIcon />} onClick={() => setTarget('release')}>放行</Button><Button color="warning" startIcon={<BlockOutlinedIcon />} onClick={() => setTarget('quarantine')}>隔离</Button><Button color="error" startIcon={<DeleteSweepOutlinedIcon />} onClick={() => setTarget('discard')}>报废</Button></div>}<div className="dual-control"><strong>双人复核</strong><span>提议：{selected.proposedBy || '-'}</span><span>批准：{selected.approvedBy || '待不同账号复核'}</span></div></> : <div className="empty">选择一条处置记录</div>}</aside></section>
    <ConfirmDialog open={createOpen} title="创建处置提议" onCancel={() => setCreateOpen(false)} onConfirm={() => void createProposal()}>{decidedExcursion ? <><p>提议将引用偏差 {decidedExcursion.code} 的当前评估版本 {assessmentVersionLabel(decidedExcursion.assessmentVersion)}；系统自动记录当前登录人为提议人，提议人不能批准自己的处置。</p></> : <p>当前没有已评估状态的偏差，请先完成影响评估。</p>}</ConfirmDialog>
    <ConfirmDialog open={Boolean(target)} title={`确认${target === 'release' ? '放行' : target === 'discard' ? '报废' : '隔离'}决定`} onCancel={() => setTarget('')} onConfirm={() => void approve()}><p>最终决定会保存提议人、独立复核人、评估版本、传感器证据和 request ID；偏差退回重审后本决定将失效。</p><strong>{selected?.code} · {selected?.excursionCode} · 评估 {assessmentVersionLabel(selected?.assessmentVersion)}</strong></ConfirmDialog>
  </main>;
}
