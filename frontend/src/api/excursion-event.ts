
import { request } from './client';
import type { DomainRecord, ImpactAssessment } from '../types/domain';

export async function listExcursionEvent(page = 1, pageSize = 20, search = '') {
  return request<DomainRecord[]>(`/excursions?page=${page}&pageSize=${pageSize}&search=${encodeURIComponent(search)}`);
}
export async function createExcursionEvent(input: Partial<DomainRecord>) {
  return request<DomainRecord>('/excursions', { method: 'POST', body: JSON.stringify(input) });
}
export async function transitionExcursionEvent(id: number, status: string, expectedVersion: number, reason: string) {
  return request<DomainRecord>(`/excursions/${id}/transition`, {
    method: 'POST', body: JSON.stringify({ status, expectedVersion, reason }),
  });
}
export async function listImpactAssessments(id: number) {
  return request<ImpactAssessment[]>(`/excursions/${id}/assessments`);
}
export async function listExcursionDecisions(id: number) {
  return request<DomainRecord[]>(`/excursions/${id}/decisions`);
}
