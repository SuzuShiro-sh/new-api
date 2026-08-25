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
// 本文件呈现详情清理、全量清空及任务进度控件。
import { RefreshCw, Trash2 } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import { Switch } from '@/components/ui/switch'

import { SettingsControlGroup } from '../components/settings-form-layout'
import {
  getLogDetailCleanupProgress,
  isActiveLogDetailCleanupTask,
  resolveLogDetailCleanupMode,
} from './log-detail-task'
import type { LogDetailCleanupTask } from './log-detail-types'
import { useLogDetailCleanupTask } from './use-log-detail-cleanup-task'

const SECONDS_PER_DAY = 86400

type LogDetailCleanupControlProps = {
  retentionDays: number
}

export function LogDetailCleanupControl(props: LogDetailCleanupControlProps) {
  const { t } = useTranslation()
  const [reclaimSpace, setReclaimSpace] = useState(false)
  const [showCleanupDialog, setShowCleanupDialog] = useState(false)
  const [showClearAllDialog, setShowClearAllDialog] = useState(false)
  const [confirmClearAll, setConfirmClearAll] = useState(false)

  const handleTaskFinished = useCallback(
    (task: LogDetailCleanupTask) => {
      const mode = resolveLogDetailCleanupMode(task)
      if (mode === null) {
        toast.error(t('Cleanup task returned an unexpected mode.'))
        return
      }
      if (task.status === 'failed') {
        if (task.result?.partial) {
          toast.warning(
            t(
              '{{count}} request details were deleted, but storage reclaim failed.',
              { count: task.result.deleted_count }
            )
          )
          return
        }
        toast.error(task.error || t('Failed to clean request details'))
        return
      }
      if (task.status !== 'succeeded') return

      const count = task.result?.deleted_count ?? task.state?.processed ?? 0
      if (mode === 'all') {
        toast.success(
          count > 0
            ? t('{{count}} request details permanently deleted.', { count })
            : t('All request details cleared.')
        )
      } else {
        toast.success(
          count > 0
            ? t('{{count}} request details removed.', { count })
            : t('No expired request details found.')
        )
      }
      if (task.result?.space_reclaimed) {
        toast.success(t('Request detail storage reclaimed.'))
      }
    },
    [t]
  )

  const cleanupTask = useLogDetailCleanupTask({
    onFinished: handleTaskFinished,
  })
  const busy = cleanupTask.isStarting || cleanupTask.taskActive
  const validRetention =
    Number.isInteger(props.retentionDays) && props.retentionDays > 0

  const handleStartExpired = async () => {
    if (!validRetention) {
      toast.error(t('Set a positive detail retention period first.'))
      return
    }
    const targetTimestamp =
      Math.floor(Date.now() / 1000) - props.retentionDays * SECONDS_PER_DAY
    try {
      const task = await cleanupTask.startExpired(targetTimestamp, reclaimSpace)
      setShowCleanupDialog(false)
      if (isActiveLogDetailCleanupTask(task)) {
        toast.success(t('Request detail cleanup task started.'))
      } else {
        handleTaskFinished(task)
      }
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to clean request details')
      )
    }
  }

  const handleStartAll = async () => {
    if (!confirmClearAll) return
    try {
      const task = await cleanupTask.startAll()
      setShowClearAllDialog(false)
      setConfirmClearAll(false)
      if (isActiveLogDetailCleanupTask(task)) {
        toast.success(t('All request details cleanup task started.'))
      } else {
        handleTaskFinished(task)
      }
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to clear all request details')
      )
    }
  }

  const progress = getLogDetailCleanupProgress(cleanupTask.task)
  const processed = cleanupTask.task?.state?.processed ?? 0
  const total = cleanupTask.task?.state?.total ?? 0

  return (
    <SettingsControlGroup className='space-y-4'>
      <div>
        <h4 className='text-sm font-medium'>{t('Request detail storage')}</h4>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Remove expired request and response content without deleting usage or billing logs.'
          )}
        </p>
      </div>

      <div className='flex min-w-0 items-start justify-between gap-4'>
        <div className='min-w-0 space-y-0.5'>
          <Label
            htmlFor='reclaim-log-detail-space'
            className='text-sm font-medium'
          >
            {t('Reclaim database space')}
          </Label>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Database compaction may temporarily lock log storage and requires additional free disk space.'
            )}
          </p>
        </div>
        <Switch
          id='reclaim-log-detail-space'
          checked={reclaimSpace}
          onCheckedChange={setReclaimSpace}
          disabled={busy}
        />
      </div>

      <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
        <div className='min-w-0'>
          <div className='text-sm font-medium'>
            {t('Clean expired request details')}
          </div>
          <div className='text-muted-foreground text-xs'>
            {validRetention
              ? t('Delete details older than {{count}} days.', {
                  count: props.retentionDays,
                })
              : t('Set a positive detail retention period first.')}
          </div>
        </div>
        <Button
          type='button'
          variant='destructive'
          onClick={() => setShowCleanupDialog(true)}
          disabled={!validRetention || busy}
        >
          <Trash2 className='size-4' aria-hidden='true' />
          {busy ? t('Cleaning...') : t('Clean details')}
        </Button>
      </div>

      <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
        <div className='min-w-0'>
          <div className='text-destructive text-sm font-medium'>
            {t('Clear all request details')}
          </div>
          <div className='text-muted-foreground text-xs'>
            {t(
              'Immediately remove every stored request and response detail and release its table storage. Usage and billing logs are kept.'
            )}
          </div>
        </div>
        <Button
          type='button'
          variant='destructive'
          className='shrink-0'
          onClick={() => {
            setConfirmClearAll(false)
            setShowClearAllDialog(true)
          }}
          disabled={busy}
        >
          <Trash2 className='size-4' aria-hidden='true' />
          {t('Clear all details')}
        </Button>
      </div>

      {(cleanupTask.task || cleanupTask.taskError) && (
        <div className='rounded-md border p-3'>
          {cleanupTask.task && (
            <>
              <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
                <span className='font-medium'>
                  {t('Request detail cleanup progress')}
                </span>
                <span className='text-muted-foreground tabular-nums'>
                  {progress}%
                </span>
              </div>
              <Progress value={progress} />
              <div className='text-muted-foreground mt-2 text-xs'>
                {t('{{processed}} of {{total}} details processed.', {
                  processed,
                  total,
                })}
              </div>
            </>
          )}
          {cleanupTask.task?.status === 'failed' && cleanupTask.task.error && (
            <div className='text-destructive mt-2 text-xs'>
              {cleanupTask.task.error}
            </div>
          )}
          {cleanupTask.taskError && (
            <div className='mt-2 flex flex-wrap items-center justify-between gap-2'>
              <div className='text-destructive text-xs'>
                {cleanupTask.taskError.message}
              </div>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={cleanupTask.retryTaskStatus}
              >
                <RefreshCw className='size-4' aria-hidden='true' />
                {t('Retry')}
              </Button>
            </div>
          )}
        </div>
      )}

      <AlertDialog open={showCleanupDialog} onOpenChange={setShowCleanupDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Confirm request detail cleanup')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will permanently remove request and response details older than {{days}} days. Usage and billing logs will be kept.',
                { days: props.retentionDays }
              )}{' '}
              {reclaimSpace &&
                t(
                  'The database will also be compacted to return reusable space to the operating system.'
                )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cleanupTask.isStarting}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleStartExpired}
              disabled={cleanupTask.isStarting}
            >
              <Trash2 className='size-4' aria-hidden='true' />
              {cleanupTask.isStarting ? t('Cleaning...') : t('Clean details')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={showClearAllDialog}
        onOpenChange={(open) => {
          setShowClearAllDialog(open)
          if (!open) setConfirmClearAll(false)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Clear all request details?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This permanently deletes every stored request and response detail and reclaims the detail table storage. Usage and billing logs will be kept.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className='flex items-start gap-3 rounded-md border p-3'>
            <Checkbox
              id='confirm-clear-all-request-details'
              checked={confirmClearAll}
              onCheckedChange={setConfirmClearAll}
              disabled={cleanupTask.isStarting}
            />
            <Label
              htmlFor='confirm-clear-all-request-details'
              className='cursor-pointer text-sm leading-5'
            >
              {t(
                'I understand that all stored request and response details will be permanently deleted.'
              )}
            </Label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cleanupTask.isStarting}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleStartAll}
              disabled={!confirmClearAll || cleanupTask.isStarting}
            >
              <Trash2 className='size-4' aria-hidden='true' />
              {cleanupTask.isStarting
                ? t('Cleaning...')
                : t('Clear all details')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsControlGroup>
  )
}
