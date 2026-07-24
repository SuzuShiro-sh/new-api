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
import { zodResolver } from '@hookform/resolvers/zod'
import { Trash2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { DateTimePicker } from '@/components/datetime-picker'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'
import dayjs from '@/lib/dayjs'
import { formatTimestampToDate } from '@/lib/format'

import {
  getCurrentLogDetailCleanupTask,
  getCurrentLogCleanupTask,
  getLogDetailCleanupTask,
  getSystemTask,
  startLogDetailClearAllTask,
  startLogDetailCleanupTask,
  startLogCleanupTask,
} from '../api'
import {
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import type { LogCleanupTask, LogDetailCleanupTask } from '../types'

const logSettingsSchema = z.object({
  LogConsumeEnabled: z.boolean(),
  LogDetailEnabled: z.boolean(),
  LogDetailRetentionDays: z.number().int().min(0).max(3650),
  LogDetailMaxBodyKB: z.number().int().min(16).max(5120),
})

type LogSettingsFormValues = z.infer<typeof logSettingsSchema>

type LogSettingsSectionProps = {
  defaultValues: LogSettingsFormValues
}

type ServerLogInfo = {
  enabled: boolean
  log_dir: string
  file_count: number
  total_size: number
  oldest_time?: string
  newest_time?: string
}

const HOURS_IN_DAY = 24

function formatBytes(bytes: number, decimals = 2): string {
  if (!bytes || Number.isNaN(bytes)) return '0 Bytes'
  if (bytes === 0) return '0 Bytes'
  if (bytes < 0) return `-${formatBytes(-bytes, decimals)}`
  const k = 1024
  const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k))
  if (i < 0 || i >= sizes.length) return `${bytes} Bytes`
  return `${Number.parseFloat((bytes / Math.pow(k, i)).toFixed(decimals))} ${
    sizes[i]
  }`
}

const getDateHoursAgo = (hours: number) => {
  const date = new Date()
  date.setHours(date.getHours() - hours)
  return date
}

const getDateDaysAgo = (days: number) => getDateHoursAgo(days * HOURS_IN_DAY)

const quickSelectOptions = [
  {
    label: '24 hours ago',
    getValue: () => getDateHoursAgo(24),
  },
  {
    label: '7 days ago',
    getValue: () => getDateDaysAgo(7),
  },
  {
    label: '30 days ago',
    getValue: () => getDateDaysAgo(30),
  },
]

function isActiveLogCleanupTask(task: LogCleanupTask | null) {
  return task?.status === 'pending' || task?.status === 'running'
}

export function LogSettingsSection(props: LogSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaultValues = useMemo<LogSettingsFormValues>(
    () => ({
      LogConsumeEnabled: props.defaultValues.LogConsumeEnabled,
      LogDetailEnabled: props.defaultValues.LogDetailEnabled,
      LogDetailRetentionDays: props.defaultValues.LogDetailRetentionDays,
      LogDetailMaxBodyKB: props.defaultValues.LogDetailMaxBodyKB,
    }),
    [
      props.defaultValues.LogConsumeEnabled,
      props.defaultValues.LogDetailEnabled,
      props.defaultValues.LogDetailMaxBodyKB,
      props.defaultValues.LogDetailRetentionDays,
    ]
  )
  const form = useForm<LogSettingsFormValues>({
    resolver: zodResolver(logSettingsSchema),
    defaultValues,
  })

  const [purgeDate, setPurgeDate] = useState<Date | undefined>(() =>
    getDateDaysAgo(30)
  )
  const [isStartingLogCleanup, setIsStartingLogCleanup] = useState(false)
  const [logCleanupTask, setLogCleanupTask] = useState<LogCleanupTask | null>(
    null
  )
  const [logDetailCleanupTask, setLogDetailCleanupTask] =
    useState<LogDetailCleanupTask | null>(null)
  const [isStartingLogDetailCleanup, setIsStartingLogDetailCleanup] =
    useState(false)
  const [reclaimLogDetailSpace, setReclaimLogDetailSpace] = useState(false)
  const [showLogDetailCleanupDialog, setShowLogDetailCleanupDialog] =
    useState(false)
  const [showLogDetailClearAllDialog, setShowLogDetailClearAllDialog] =
    useState(false)
  const [confirmLogDetailClearAll, setConfirmLogDetailClearAll] =
    useState(false)
  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [serverLogInfo, setServerLogInfo] = useState<ServerLogInfo | null>(null)
  const [serverLogCleanupMode, setServerLogCleanupMode] = useState('by_count')
  const [serverLogCleanupValue, setServerLogCleanupValue] = useState(10)
  const [serverLogCleanupLoading, setServerLogCleanupLoading] = useState(false)

  const fetchServerLogInfo = useCallback(async () => {
    try {
      const res = await api.get('/api/performance/logs')
      if (res.data.success) setServerLogInfo(res.data.data)
    } catch {
      /* ignore */
    }
  }, [])

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  useEffect(() => {
    fetchServerLogInfo()
  }, [fetchServerLogInfo])

  useEffect(() => {
    let cancelled = false

    async function fetchCurrentCleanupTasks() {
      try {
        const [logResponse, detailResponse] = await Promise.all([
          getCurrentLogCleanupTask(),
          getCurrentLogDetailCleanupTask(),
        ])
        if (cancelled) return
        if (logResponse.success && logResponse.data) {
          setLogCleanupTask(logResponse.data)
        }
        if (detailResponse.success && detailResponse.data) {
          setLogDetailCleanupTask(detailResponse.data)
        }
      } catch {
        /* ignore */
      }
    }

    fetchCurrentCleanupTasks()

    return () => {
      cancelled = true
    }
  }, [])

  const purgeTimestamp = useMemo(() => {
    if (!purgeDate) return null
    return Math.floor(purgeDate.getTime() / 1000)
  }, [purgeDate])

  const formattedPurgeDate = useMemo(() => {
    if (!purgeDate) return ''
    return formatTimestampToDate(purgeDate.getTime(), 'milliseconds')
  }, [purgeDate])

  const logCleanupActive = isActiveLogCleanupTask(logCleanupTask)
  const logCleanupState = logCleanupTask?.state
  const logCleanupProgress = Math.min(
    100,
    Math.max(0, logCleanupState?.progress ?? 0)
  )
  const logCleanupProcessed = logCleanupState?.processed ?? 0
  const logCleanupTotal = logCleanupState?.total ?? 0
  const logCleanupTaskId = logCleanupTask?.task_id
  const logDetailCleanupActive = isActiveLogCleanupTask(logDetailCleanupTask)
  const logDetailCleanupState = logDetailCleanupTask?.state
  const logDetailCleanupProgress = Math.min(
    100,
    Math.max(0, logDetailCleanupState?.progress ?? 0)
  )
  const logDetailCleanupProcessed = logDetailCleanupState?.processed ?? 0
  const logDetailCleanupTotal = logDetailCleanupState?.total ?? 0
  const logDetailCleanupTaskId = logDetailCleanupTask?.task_id
  const detailRetentionDays = useWatch({
    control: form.control,
    name: 'LogDetailRetentionDays',
  })

  useEffect(() => {
    if (!logCleanupTaskId || !logCleanupActive) return

    let cancelled = false
    const interval = window.setInterval(async () => {
      try {
        const res = await getSystemTask(logCleanupTaskId)
        if (cancelled || !res.success || !res.data) return

        setLogCleanupTask(res.data)
        if (!isActiveLogCleanupTask(res.data)) {
          if (res.data.status === 'succeeded') {
            const count =
              res.data.result?.deleted_count ?? res.data.state?.processed ?? 0
            toast.success(
              count > 0
                ? t('{{count}} log entries removed.', { count })
                : t('No log entries matched the selected time.')
            )
          } else if (res.data.status === 'failed') {
            toast.error(res.data.error || t('Failed to clean logs'))
          }
        }
      } catch {
        /* keep polling */
      }
    }, 1000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [logCleanupActive, logCleanupTaskId, t])

  useEffect(() => {
    if (!logDetailCleanupTaskId || !logDetailCleanupActive) return

    let cancelled = false
    const interval = window.setInterval(async () => {
      try {
        const res = await getLogDetailCleanupTask(logDetailCleanupTaskId)
        if (cancelled || !res.success || !res.data) return

        setLogDetailCleanupTask(res.data)
        if (!isActiveLogCleanupTask(res.data)) {
          if (res.data.status === 'succeeded') {
            const count =
              res.data.result?.deleted_count ?? res.data.state?.processed ?? 0
            const mode =
              res.data.result?.mode ?? res.data.payload?.mode ?? 'expired'
            if (mode === 'all') {
              toast.success(
                count > 0
                  ? t('{{count}} request details permanently deleted.', {
                      count,
                    })
                  : t('All request details cleared.')
              )
            } else {
              toast.success(
                count > 0
                  ? t('{{count}} request details removed.', { count })
                  : t('No expired request details found.')
              )
            }
            if (res.data.result?.space_reclaimed) {
              toast.success(t('Request detail storage reclaimed.'))
            }
          } else if (res.data.status === 'failed') {
            toast.error(res.data.error || t('Failed to clean request details'))
          }
        }
      } catch {
        /* keep polling */
      }
    }, 1000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [logDetailCleanupActive, logDetailCleanupTaskId, t])

  const onSubmit = async (values: LogSettingsFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof LogSettingsFormValues]
    )
    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value })
    }
  }

  const handleRequestCleanLogs = () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setShowConfirmDialog(true)
  }

  const handleCleanLogs = async () => {
    if (!purgeTimestamp) {
      toast.error(t('Select a timestamp before clearing logs.'))
      return
    }

    setIsStartingLogCleanup(true)
    try {
      const res = await startLogCleanupTask(purgeTimestamp)
      if (!res.success) {
        throw new Error(res.message || t('Failed to clean logs'))
      }
      if (!res.data) {
        throw new Error(t('Failed to clean logs'))
      }
      setLogCleanupTask(res.data)
      setShowConfirmDialog(false)
      toast.success(t('Log cleanup task started.'))
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t('Failed to clean logs')
      toast.error(message)
    } finally {
      setIsStartingLogCleanup(false)
    }
  }

  const handleRequestCleanLogDetails = () => {
    if (!Number.isInteger(detailRetentionDays) || detailRetentionDays <= 0) {
      toast.error(t('Set a positive detail retention period first.'))
      return
    }
    setShowLogDetailCleanupDialog(true)
  }

  const handleCleanLogDetails = async () => {
    if (!Number.isInteger(detailRetentionDays) || detailRetentionDays <= 0) {
      toast.error(t('Set a positive detail retention period first.'))
      return
    }

    const targetTimestamp = Math.floor(
      (Date.now() - detailRetentionDays * HOURS_IN_DAY * 60 * 60 * 1000) / 1000
    )
    setIsStartingLogDetailCleanup(true)
    try {
      const res = await startLogDetailCleanupTask(
        targetTimestamp,
        reclaimLogDetailSpace
      )
      if (!res.success || !res.data) {
        throw new Error(res.message || t('Failed to clean request details'))
      }
      setLogDetailCleanupTask(res.data)
      setShowLogDetailCleanupDialog(false)
      toast.success(t('Request detail cleanup task started.'))
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : t('Failed to clean request details')
      toast.error(message)
    } finally {
      setIsStartingLogDetailCleanup(false)
    }
  }

  const handleRequestClearAllLogDetails = () => {
    setConfirmLogDetailClearAll(false)
    setShowLogDetailClearAllDialog(true)
  }

  const handleClearAllLogDetails = async () => {
    if (!confirmLogDetailClearAll) return

    setIsStartingLogDetailCleanup(true)
    try {
      const res = await startLogDetailClearAllTask()
      if (!res.success || !res.data) {
        throw new Error(res.message || t('Failed to clear all request details'))
      }
      setLogDetailCleanupTask(res.data)
      setShowLogDetailClearAllDialog(false)
      setConfirmLogDetailClearAll(false)
      toast.success(t('All request details cleanup task started.'))
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : t('Failed to clear all request details')
      toast.error(message)
    } finally {
      setIsStartingLogDetailCleanup(false)
    }
  }

  const cleanupServerLogFiles = async () => {
    if (
      !serverLogCleanupValue ||
      Number.isNaN(serverLogCleanupValue) ||
      serverLogCleanupValue < 1
    ) {
      toast.error(t('Please enter a valid number'))
      return
    }

    setServerLogCleanupLoading(true)
    try {
      const res = await api.delete(
        `/api/performance/logs?mode=${serverLogCleanupMode}&value=${serverLogCleanupValue}`
      )
      if (res.data.success) {
        const { deleted_count, freed_bytes } = res.data.data
        toast.success(
          t('Cleaned up {{count}} log files, freed {{size}}', {
            count: deleted_count,
            size: formatBytes(freed_bytes),
          })
        )
      } else {
        toast.error(res.data.message || t('Cleanup failed'))
      }
      fetchServerLogInfo()
    } catch {
      toast.error(t('Cleanup failed'))
    } finally {
      setServerLogCleanupLoading(false)
    }
  }

  return (
    <SettingsSection title={t('Log Maintenance')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save log settings'
          />
          <FormField
            control={form.control}
            name='LogConsumeEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Record quota usage')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Track per-request consumption to power usage analytics. Keeping this on increases database writes.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='LogDetailEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Record request and response details')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Store request and response bodies for recent usage logs. This content can be sensitive and increases database usage.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </SettingsSwitchItem>
            )}
          />

          <SettingsControlGroup className='space-y-4'>
            <div>
              <h4 className='text-sm font-medium'>
                {t('Request detail storage')}
              </h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Remove expired request and response content without deleting usage or billing logs.'
                )}
              </p>
            </div>

            <div className='grid min-w-0 gap-4 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='LogDetailRetentionDays'
                render={({ field }) => (
                  <FormItem className='min-w-0'>
                    <FormLabel>{t('Detail retention (days)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={3650}
                        step={1}
                        {...field}
                        onChange={(event) =>
                          field.onChange(Number(event.target.value))
                        }
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Details older than this are removed hourly without deleting usage logs. Set 0 to disable automatic expiration.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='LogDetailMaxBodyKB'
                render={({ field }) => (
                  <FormItem className='min-w-0'>
                    <FormLabel>
                      {t('Maximum content per section (KiB)')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={16}
                        max={5120}
                        step={16}
                        {...field}
                        onChange={(event) =>
                          field.onChange(Number(event.target.value))
                        }
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Request, response, raw response, and error content are truncated independently at this limit.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <div className='flex min-w-0 flex-col gap-3 border-t pt-3'>
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
                  checked={reclaimLogDetailSpace}
                  onCheckedChange={setReclaimLogDetailSpace}
                  disabled={
                    isStartingLogDetailCleanup || logDetailCleanupActive
                  }
                />
              </div>

              <div className='flex flex-wrap items-center justify-between gap-3'>
                <div className='min-w-0'>
                  <div className='text-sm font-medium'>
                    {t('Clean expired request details')}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {detailRetentionDays > 0
                      ? t('Delete details older than {{count}} days.', {
                          count: detailRetentionDays,
                        })
                      : t('Set a positive detail retention period first.')}
                  </div>
                </div>
                <Button
                  type='button'
                  variant='destructive'
                  onClick={handleRequestCleanLogDetails}
                  disabled={
                    detailRetentionDays <= 0 ||
                    isStartingLogDetailCleanup ||
                    logDetailCleanupActive
                  }
                >
                  {isStartingLogDetailCleanup || logDetailCleanupActive
                    ? t('Cleaning...')
                    : t('Clean details')}
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
                  onClick={handleRequestClearAllLogDetails}
                  disabled={
                    isStartingLogDetailCleanup || logDetailCleanupActive
                  }
                >
                  <Trash2 className='size-4' aria-hidden='true' />
                  {t('Clear all details')}
                </Button>
              </div>

              {logDetailCleanupTask && (
                <div className='rounded-md border p-3'>
                  <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
                    <span className='font-medium'>
                      {t('Request detail cleanup progress')}
                    </span>
                    <span className='text-muted-foreground tabular-nums'>
                      {logDetailCleanupProgress}%
                    </span>
                  </div>
                  <Progress value={logDetailCleanupProgress} />
                  <div className='text-muted-foreground mt-2 text-xs'>
                    {t('{{processed}} of {{total}} details processed.', {
                      processed: logDetailCleanupProcessed,
                      total: logDetailCleanupTotal,
                    })}
                  </div>
                  {logDetailCleanupTask.status === 'failed' &&
                    logDetailCleanupTask.error && (
                      <div className='text-destructive mt-2 text-xs'>
                        {logDetailCleanupTask.error}
                      </div>
                    )}
                </div>
              )}
            </div>
          </SettingsControlGroup>

          <SettingsControlGroup className='space-y-3'>
            <div>
              <h4 className='text-sm font-medium'>{t('Clean history logs')}</h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Remove all log entries created before the selected timestamp.'
                )}
              </p>
            </div>
            <DateTimePicker value={purgeDate} onChange={setPurgeDate} />
            <div className='flex flex-wrap gap-3'>
              {quickSelectOptions.map((option) => (
                <Button
                  key={option.label}
                  type='button'
                  variant='outline'
                  onClick={() => setPurgeDate(option.getValue())}
                >
                  {t(option.label)}
                </Button>
              ))}
              <Button
                type='button'
                variant='destructive'
                onClick={handleRequestCleanLogs}
                disabled={isStartingLogCleanup || logCleanupActive}
              >
                {isStartingLogCleanup || logCleanupActive
                  ? t('Cleaning...')
                  : t('Clean logs')}
              </Button>
            </div>
            {logCleanupTask && (
              <div className='rounded-md border p-3'>
                <div className='mb-2 flex items-center justify-between gap-3 text-sm'>
                  <span className='font-medium'>
                    {t('Log cleanup progress')}
                  </span>
                  <span className='text-muted-foreground tabular-nums'>
                    {logCleanupProgress}%
                  </span>
                </div>
                <Progress value={logCleanupProgress} />
                <div className='text-muted-foreground mt-2 text-xs'>
                  {t('{{processed}} of {{total}} log entries processed.', {
                    processed: logCleanupProcessed,
                    total: logCleanupTotal,
                  })}
                </div>
                {logCleanupTask.status === 'failed' && logCleanupTask.error && (
                  <div className='text-destructive mt-2 text-xs'>
                    {logCleanupTask.error}
                  </div>
                )}
              </div>
            )}
          </SettingsControlGroup>
        </SettingsForm>
      </Form>

      <Separator />

      <div className='space-y-4'>
        <div>
          <h4 className='font-medium'>{t('Server Log Management')}</h4>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t(
              'Manage server log files. Log files accumulate over time; regular cleanup is recommended to free disk space.'
            )}
          </p>
        </div>

        {serverLogInfo !== null &&
          (serverLogInfo.enabled ? (
            <div className='space-y-4'>
              <div className='rounded-lg border p-4'>
                <div className='grid grid-cols-2 gap-2 text-sm md:grid-cols-4'>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Log Directory')}:
                    </span>{' '}
                    <span className='font-mono text-xs'>
                      {serverLogInfo.log_dir}
                    </span>
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Log File Count')}:
                    </span>{' '}
                    {serverLogInfo.file_count}
                  </div>
                  <div>
                    <span className='text-muted-foreground'>
                      {t('Total Log Size')}:
                    </span>{' '}
                    {formatBytes(serverLogInfo.total_size)}
                  </div>
                  {serverLogInfo.oldest_time && serverLogInfo.newest_time && (
                    <div>
                      <span className='text-muted-foreground'>
                        {t('Date Range')}:
                      </span>{' '}
                      {dayjs(serverLogInfo.oldest_time).format('YYYY-MM-DD')} ~{' '}
                      {dayjs(serverLogInfo.newest_time).format('YYYY-MM-DD')}
                    </div>
                  )}
                </div>
              </div>

              <div className='flex flex-wrap items-end gap-3'>
                <div className='grid gap-1.5'>
                  <Label className='text-xs'>{t('Cleanup Mode')}</Label>
                  <Select
                    items={[
                      { value: 'by_count', label: t('Retain last N files') },
                      { value: 'by_days', label: t('Retain last N days') },
                    ]}
                    value={serverLogCleanupMode}
                    onValueChange={(value) =>
                      value !== null && setServerLogCleanupMode(value)
                    }
                  >
                    <SelectTrigger className='w-[160px]'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        <SelectItem value='by_count'>
                          {t('Retain last N files')}
                        </SelectItem>
                        <SelectItem value='by_days'>
                          {t('Retain last N days')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
                <div className='grid gap-1.5'>
                  <Label className='text-xs'>
                    {serverLogCleanupMode === 'by_count'
                      ? t('Files to Retain')
                      : t('Days to Retain')}
                  </Label>
                  <Input
                    type='number'
                    min={1}
                    max={serverLogCleanupMode === 'by_count' ? 1000 : 3650}
                    value={serverLogCleanupValue}
                    onChange={(event) =>
                      setServerLogCleanupValue(Number(event.target.value))
                    }
                    className='w-[120px]'
                  />
                </div>
                <AlertDialog>
                  <AlertDialogTrigger
                    render={
                      <Button
                        type='button'
                        variant='destructive'
                        size='sm'
                        disabled={serverLogCleanupLoading}
                      />
                    }
                  >
                    {serverLogCleanupLoading
                      ? t('Cleaning...')
                      : t('Clean Up Log Files')}
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>
                        {t('Confirm log file cleanup?')}
                      </AlertDialogTitle>
                      <AlertDialogDescription>
                        {serverLogCleanupMode === 'by_count'
                          ? t(
                              'Only the last {{value}} log files will be retained; the rest will be deleted.',
                              {
                                value: serverLogCleanupValue,
                              }
                            )
                          : t(
                              'Log files older than {{value}} days will be deleted.',
                              {
                                value: serverLogCleanupValue,
                              }
                            )}
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
                      <AlertDialogAction
                        variant='destructive'
                        onClick={cleanupServerLogFiles}
                      >
                        {t('Confirm Cleanup')}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            </div>
          ) : (
            <Alert>
              <AlertDescription>
                {t(
                  'Server logging is not enabled (log directory not configured)'
                )}
              </AlertDescription>
            </Alert>
          ))}
      </div>

      <AlertDialog open={showConfirmDialog} onOpenChange={setShowConfirmDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Confirm log cleanup')}</AlertDialogTitle>
            <AlertDialogDescription>
              {formattedPurgeDate
                ? t(
                    'This will permanently remove all log entries created before {{date}}.',
                    { date: formattedPurgeDate }
                  )
                : t(
                    'This will permanently remove log entries before the selected timestamp.'
                  )}{' '}
              {t('This action cannot be undone.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isStartingLogCleanup}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleCleanLogs}
              disabled={isStartingLogCleanup}
            >
              {isStartingLogCleanup ? t('Cleaning...') : t('Delete logs')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={showLogDetailCleanupDialog}
        onOpenChange={setShowLogDetailCleanupDialog}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Confirm request detail cleanup')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will permanently remove request and response details older than {{days}} days. Usage and billing logs will be kept.',
                { days: detailRetentionDays }
              )}{' '}
              {reclaimLogDetailSpace &&
                t(
                  'The database will also be compacted to return reusable space to the operating system.'
                )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isStartingLogDetailCleanup}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleCleanLogDetails}
              disabled={isStartingLogDetailCleanup}
            >
              {isStartingLogDetailCleanup
                ? t('Cleaning...')
                : t('Clean details')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={showLogDetailClearAllDialog}
        onOpenChange={(open) => {
          setShowLogDetailClearAllDialog(open)
          if (!open) setConfirmLogDetailClearAll(false)
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
              checked={confirmLogDetailClearAll}
              onCheckedChange={setConfirmLogDetailClearAll}
              disabled={isStartingLogDetailCleanup}
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
            <AlertDialogCancel disabled={isStartingLogDetailCleanup}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={handleClearAllLogDetails}
              disabled={!confirmLogDetailClearAll || isStartingLogDetailCleanup}
            >
              <Trash2 className='size-4' aria-hidden='true' />
              {isStartingLogDetailCleanup
                ? t('Cleaning...')
                : t('Clear all details')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
