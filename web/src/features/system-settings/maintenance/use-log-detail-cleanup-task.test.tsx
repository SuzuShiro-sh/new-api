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
// 本文件验证详情维护轮询遇到协议异常时会解除活动任务状态。
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import {
  getCurrentLogDetailCleanupTask,
  getLogDetailCleanupTask,
} from './log-detail-api'
import type { LogDetailCleanupTask } from './log-detail-types'
import { useLogDetailCleanupTask } from './use-log-detail-cleanup-task'

vi.mock('./log-detail-api', () => ({
  getCurrentLogDetailCleanupTask: vi.fn(),
  getLogDetailCleanupTask: vi.fn(),
  startLogDetailCleanupTask: vi.fn(),
  startLogDetailClearAllTask: vi.fn(),
}))

function activeTask(taskId = 'task-1'): LogDetailCleanupTask {
  return {
    id: 1,
    task_id: taskId,
    type: 'log_detail_cleanup',
    status: 'running',
    payload: {
      mode: 'expired',
      target_timestamp: 1,
      batch_size: 500,
      reclaim_space: false,
    },
    state: { total: 10, processed: 2, progress: 20, remaining: 8 },
    created_at: 1,
    updated_at: 1,
  }
}

describe('useLogDetailCleanupTask polling', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  test('clears a stale active task when the polling identity changes', async () => {
    vi.mocked(getCurrentLogDetailCleanupTask).mockResolvedValue({
      success: true,
      message: '',
      data: activeTask(),
    })
    vi.mocked(getLogDetailCleanupTask).mockResolvedValue({
      success: true,
      message: '',
      data: activeTask('task-2'),
    })
    const onFinished = vi.fn()
    const { result } = renderHook(() => useLogDetailCleanupTask({ onFinished }))

    await act(async () => {
      await Promise.resolve()
    })
    expect(result.current.task?.task_id).toBe('task-1')
    expect(result.current.taskActive).toBe(true)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })

    expect(result.current.task).toBeNull()
    expect(result.current.taskActive).toBe(false)
    expect(result.current.taskError?.message).toBe(
      'Cleanup task identity changed unexpectedly'
    )
    expect(onFinished).not.toHaveBeenCalled()
  })
})
