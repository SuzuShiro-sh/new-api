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
// 本文件归一化详情维护任务的模式、进度与错误信息。
import axios from 'axios'

import type {
  LogDetailCleanupMode,
  LogDetailCleanupTask,
} from './log-detail-types'

function isLogDetailCleanupMode(value: unknown): value is LogDetailCleanupMode {
  return value === 'expired' || value === 'all'
}

export function isActiveLogDetailCleanupTask(
  task: LogDetailCleanupTask | null
) {
  return task?.status === 'pending' || task?.status === 'running'
}

export function resolveLogDetailCleanupMode(
  task: LogDetailCleanupTask
): LogDetailCleanupMode | null {
  const payloadMode = task.payload?.mode ?? 'expired'
  if (!isLogDetailCleanupMode(payloadMode)) return null

  const resultMode = task.result?.mode
  if (resultMode === undefined) return payloadMode
  if (!isLogDetailCleanupMode(resultMode) || resultMode !== payloadMode) {
    return null
  }
  return resultMode
}

export function hasLogDetailCleanupMode(
  task: LogDetailCleanupTask,
  expectedMode: LogDetailCleanupMode
) {
  return resolveLogDetailCleanupMode(task) === expectedMode
}

export function getLogDetailCleanupProgress(task: LogDetailCleanupTask | null) {
  return Math.min(100, Math.max(0, task?.state?.progress ?? 0))
}

export function normalizeLogDetailTaskError(error: unknown) {
  if (axios.isAxiosError<{ message?: string }>(error)) {
    return new Error(error.response?.data?.message || error.message)
  }
  if (error instanceof Error) return error
  return new Error('Request failed')
}
