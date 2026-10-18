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
// 本文件管理详情维护任务的启动、恢复和串行轮询生命周期。
import { useCallback, useEffect, useRef, useState } from 'react'

import {
  getCurrentLogDetailCleanupTask,
  getLogDetailCleanupTask,
  startLogDetailCleanupTask,
  startLogDetailClearAllTask,
} from './log-detail-api'
import {
  hasLogDetailCleanupMode,
  isActiveLogDetailCleanupTask,
  normalizeLogDetailTaskError,
  resolveLogDetailCleanupMode,
} from './log-detail-task'
import type {
  LogDetailCleanupMode,
  LogDetailCleanupTask,
} from './log-detail-types'

const POLL_INTERVAL_MS = 1000

type UseLogDetailCleanupTaskOptions = {
  onFinished: (task: LogDetailCleanupTask) => void
}

function requireTaskResponse(
  response: {
    success: boolean
    message: string
    data?: LogDetailCleanupTask
  },
  expectedMode: LogDetailCleanupMode
) {
  if (!response.success || !response.data) {
    throw new Error(response.message || 'Request failed')
  }
  if (!hasLogDetailCleanupMode(response.data, expectedMode)) {
    throw new Error('Cleanup task returned an unexpected mode')
  }
  return response.data
}

export function useLogDetailCleanupTask(
  options: UseLogDetailCleanupTaskOptions
) {
  const { onFinished } = options
  const [task, setTask] = useState<LogDetailCleanupTask | null>(null)
  const [taskError, setTaskError] = useState<Error | null>(null)
  const [isStarting, setIsStarting] = useState(false)
  const [currentRefresh, setCurrentRefresh] = useState(0)
  const [pollRefresh, setPollRefresh] = useState(0)
  const mountedRef = useRef(true)
  const taskRevisionRef = useRef(0)

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    const taskRevision = taskRevisionRef.current

    async function loadCurrentTask() {
      try {
        const response = await getCurrentLogDetailCleanupTask()
        if (
          cancelled ||
          !mountedRef.current ||
          taskRevision !== taskRevisionRef.current
        ) {
          return
        }
        if (!response.success) {
          throw new Error(response.message || 'Request failed')
        }
        if (
          response.data &&
          resolveLogDetailCleanupMode(response.data) === null
        ) {
          setTask(null)
          throw new Error('Cleanup task returned an unexpected mode')
        }
        setTask(response.data ?? null)
        setTaskError(null)
      } catch (error) {
        if (
          !cancelled &&
          mountedRef.current &&
          taskRevision === taskRevisionRef.current
        ) {
          setTaskError(normalizeLogDetailTaskError(error))
        }
      }
    }

    void loadCurrentTask()
    return () => {
      cancelled = true
    }
  }, [currentRefresh])

  const taskId = task?.task_id
  const taskActive = isActiveLogDetailCleanupTask(task)

  useEffect(() => {
    if (!taskId || !taskActive) return
    const activeTaskId = taskId

    let cancelled = false
    let timer: number | undefined

    async function pollTask() {
      try {
        const response = await getLogDetailCleanupTask(activeTaskId)
        if (cancelled || !mountedRef.current) return
        if (!response.success || !response.data) {
          throw new Error(response.message || 'Request failed')
        }
        if (response.data.task_id !== activeTaskId) {
          setTask(null)
          throw new Error('Cleanup task identity changed unexpectedly')
        }
        if (resolveLogDetailCleanupMode(response.data) === null) {
          setTask(null)
          throw new Error('Cleanup task returned an unexpected mode')
        }

        setTask(response.data)
        setTaskError(null)
        if (isActiveLogDetailCleanupTask(response.data)) {
          timer = window.setTimeout(pollTask, POLL_INTERVAL_MS)
        } else {
          onFinished(response.data)
        }
      } catch (error) {
        if (!cancelled && mountedRef.current) {
          setTaskError(normalizeLogDetailTaskError(error))
        }
      }
    }

    timer = window.setTimeout(pollTask, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [onFinished, pollRefresh, taskActive, taskId])

  const startTask = useCallback(
    async (
      mode: LogDetailCleanupMode,
      request: () => ReturnType<typeof startLogDetailClearAllTask>
    ) => {
      setIsStarting(true)
      setTaskError(null)
      try {
        const response = await request()
        const nextTask = requireTaskResponse(response, mode)
        if (mountedRef.current) {
          taskRevisionRef.current += 1
          setTask(nextTask)
        }
        return nextTask
      } catch (error) {
        throw normalizeLogDetailTaskError(error)
      } finally {
        if (mountedRef.current) setIsStarting(false)
      }
    },
    []
  )

  const startExpired = useCallback(
    (targetTimestamp: number, reclaimSpace: boolean) =>
      startTask('expired', () =>
        startLogDetailCleanupTask(targetTimestamp, reclaimSpace)
      ),
    [startTask]
  )

  const startAll = useCallback(
    () => startTask('all', startLogDetailClearAllTask),
    [startTask]
  )

  const retryTaskStatus = useCallback(() => {
    setTaskError(null)
    if (isActiveLogDetailCleanupTask(task)) {
      setPollRefresh((value) => value + 1)
    } else {
      setCurrentRefresh((value) => value + 1)
    }
  }, [task])

  return {
    task,
    taskError,
    isStarting,
    taskActive,
    startExpired,
    startAll,
    retryTaskStatus,
  }
}
