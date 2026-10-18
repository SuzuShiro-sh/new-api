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
// 本文件定义请求响应详情维护任务的前端契约。
import type { LogCleanupTaskState, SystemTask } from '../types'

export type LogDetailCleanupMode = 'expired' | 'all'

export type LogDetailCleanupTaskPayload = {
  mode?: LogDetailCleanupMode
  target_timestamp: number
  batch_size: number
  reclaim_space: boolean
}

export type LogDetailCleanupTaskResult = {
  mode?: LogDetailCleanupMode
  deleted_count: number
  space_reclaimed: boolean
  partial?: boolean
}

export type LogDetailCleanupTask = SystemTask<
  LogDetailCleanupTaskPayload,
  LogCleanupTaskState,
  LogDetailCleanupTaskResult
>
