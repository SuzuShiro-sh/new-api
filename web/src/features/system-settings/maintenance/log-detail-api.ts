/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// 本文件封装请求响应详情维护任务的 HTTP 接口。
import { api } from '@/lib/api'

import type { SystemTaskResponse } from '../types'
import type { LogDetailCleanupTask } from './log-detail-types'

const taskRequestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
}

export async function startLogDetailCleanupTask(
  targetTimestamp: number,
  reclaimSpace: boolean
) {
  const res = await api.post<SystemTaskResponse<LogDetailCleanupTask>>(
    '/api/system-task/log-detail-cleanup',
    null,
    {
      ...taskRequestConfig,
      params: {
        target_timestamp: targetTimestamp,
        reclaim_space: reclaimSpace,
      },
    }
  )
  return res.data
}

export async function startLogDetailClearAllTask() {
  const res = await api.post<SystemTaskResponse<LogDetailCleanupTask>>(
    '/api/system-task/log-detail-clear-all',
    null,
    taskRequestConfig
  )
  return res.data
}

export async function getCurrentLogDetailCleanupTask() {
  const res = await api.get<SystemTaskResponse<LogDetailCleanupTask | null>>(
    '/api/system-task/current',
    {
      ...taskRequestConfig,
      params: { type: 'log_detail_cleanup' },
    }
  )
  return res.data
}

export async function getLogDetailCleanupTask(taskId: string) {
  const res = await api.get<SystemTaskResponse<LogDetailCleanupTask>>(
    `/api/system-task/${taskId}`,
    taskRequestConfig
  )
  return res.data
}
