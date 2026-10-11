import { API_BASE, request } from './client'
import type { DestinationsView, PMOInstalled, PMOOptions, PMORequest, ReportDestinationInput, WorkOrderReports } from './types.delivery'

type Signal = AbortSignal | undefined

export const destinationsApi = {
  get: (scheduleId: string, signal?: Signal) => request<DestinationsView>('GET', `/schedules/${scheduleId}/report-destinations`, { signal }),
  put: (scheduleId: string, destinations: ReportDestinationInput[]) =>
    request<DestinationsView>('PUT', `/schedules/${scheduleId}/report-destinations`, { body: { destinations } }),
}

export const reportsApi = {
  list: (workOrderId: string, signal?: Signal) => request<WorkOrderReports>('GET', `/work-orders/${workOrderId}/reports`, { signal }),
  /** Sends only the deliveries that failed; what was sent is never sent again */
  retry: (workOrderId: string) => request<WorkOrderReports>('POST', `/work-orders/${workOrderId}/reports/retry`, { body: {} }),
  /** The report as a self-contained HTML page, for a download link (the session cookie authorizes it) */
  htmlUrl: (workOrderId: string, reportId: string) => `${API_BASE}/work-orders/${encodeURIComponent(workOrderId)}/reports/${encodeURIComponent(reportId)}/html`,
}

export const pmoApi = {
  options: (signal?: Signal) => request<PMOOptions>('GET', '/pmo/setup', { signal }),
  install: (body: PMORequest) => request<PMOInstalled>('POST', '/pmo/setup', { body }),
}
