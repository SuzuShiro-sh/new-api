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
import { describe, expect, test } from 'vitest'

import {
  getLogDetailCleanupProgress,
  hasLogDetailCleanupMode,
  isActiveLogDetailCleanupTask,
  normalizeLogDetailTaskError,
  resolveLogDetailCleanupMode,
} from './log-detail-task'
import type { LogDetailCleanupTask } from './log-detail-types'

function taskFixture(
  overrides: Partial<LogDetailCleanupTask> = {}
): LogDetailCleanupTask {
  return {
    id: 1,
    task_id: 'task-1',
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
    ...overrides,
  }
}

describe('log detail cleanup task contract', () => {
  test('defaults legacy payloads without a mode to expired cleanup', () => {
    const task = taskFixture({
      payload: {
        target_timestamp: 1,
        batch_size: 500,
        reclaim_space: false,
      },
    })

    expect(resolveLogDetailCleanupMode(task)).toBe('expired')
    expect(hasLogDetailCleanupMode(task, 'expired')).toBe(true)
    expect(hasLogDetailCleanupMode(task, 'all')).toBe(false)
  })

  test('rejects conflicting payload and result modes', () => {
    const task = taskFixture({
      status: 'succeeded',
      result: {
        mode: 'all',
        deleted_count: 10,
        space_reclaimed: true,
      },
    })

    expect(resolveLogDetailCleanupMode(task)).toBeNull()
    expect(hasLogDetailCleanupMode(task, 'all')).toBe(false)
  })

  test('rejects an all-cleanup result attached to a legacy expired payload', () => {
    const task = taskFixture({
      status: 'succeeded',
      payload: {
        target_timestamp: 1,
        batch_size: 500,
        reclaim_space: false,
      },
      result: {
        mode: 'all',
        deleted_count: 10,
        space_reclaimed: true,
      },
    })

    expect(resolveLogDetailCleanupMode(task)).toBeNull()
  })

  test('rejects unknown task modes', () => {
    const task = taskFixture({
      payload: {
        mode: 'unexpected' as 'all',
        target_timestamp: 1,
        batch_size: 500,
        reclaim_space: false,
      },
    })

    expect(resolveLogDetailCleanupMode(task)).toBeNull()
  })

  test('preserves the server message for maintenance conflicts', () => {
    const error = {
      isAxiosError: true,
      message: 'Request failed with status code 409',
      response: {
        data: {
          message:
            'log detail cleanup mode all conflicts with active mode expired',
        },
      },
    }

    expect(normalizeLogDetailTaskError(error).message).toBe(
      'log detail cleanup mode all conflicts with active mode expired'
    )
  })

  test('recognizes active states and clamps untrusted progress values', () => {
    expect(isActiveLogDetailCleanupTask(taskFixture())).toBe(true)
    expect(
      isActiveLogDetailCleanupTask(taskFixture({ status: 'succeeded' }))
    ).toBe(false)
    expect(
      getLogDetailCleanupProgress(
        taskFixture({
          state: { total: 10, processed: 11, progress: 140, remaining: 0 },
        })
      )
    ).toBe(100)
    expect(
      getLogDetailCleanupProgress(
        taskFixture({
          state: { total: 10, processed: 0, progress: -20, remaining: 10 },
        })
      )
    ).toBe(0)
  })
})
