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
import {
  AlertCircleIcon,
  Database01Icon,
  FileExportIcon,
  FileInputIcon,
  FileNotFoundIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import { getLogDetail } from '../../api'
import type { LogDetail } from '../../types'

const MAX_FORMATTABLE_JSON_BYTES = 512 * 1024

type RequestResponseDetailProps = {
  requestId: string
  isAdmin: boolean
}

type DetailCodeBlockProps = {
  title: string
  value?: string
  emptyText: string
  tone?: 'default' | 'danger'
}

function formatDetailText(value: string): string {
  const trimmed = value.trim()
  if (!trimmed || value.length > MAX_FORMATTABLE_JSON_BYTES) return value
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2)
  } catch {
    return value
  }
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB']
  const index = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1
  )
  const value = bytes / Math.pow(1024, index)
  return `${value.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function DetailCodeBlock(props: DetailCodeBlockProps) {
  const { t } = useTranslation()
  const text = formatDetailText(props.value ?? '')
  const hasText = text.trim().length > 0

  return (
    <div
      className={cn(
        'bg-background min-w-0 overflow-hidden rounded-md border',
        props.tone === 'danger' && 'border-destructive/50'
      )}
    >
      <div
        className={cn(
          'bg-muted/40 flex min-h-9 items-center justify-between gap-2 border-b px-2.5',
          props.tone === 'danger' && 'bg-destructive/5'
        )}
      >
        <span
          className={cn(
            'truncate text-xs font-medium',
            props.tone === 'danger' && 'text-destructive'
          )}
        >
          {props.title}
        </span>
        {hasText && (
          <CopyButton
            value={text}
            className='size-7'
            iconClassName='size-3.5'
            tooltip={t('Copy to clipboard')}
          />
        )}
      </div>
      <pre
        className={cn(
          'max-h-[min(48dvh,32rem)] min-h-32 overflow-auto p-3 font-mono text-[11px] leading-5 break-words whitespace-pre-wrap',
          !hasText && 'text-muted-foreground flex items-center justify-center'
        )}
      >
        {hasText ? text : props.emptyText}
      </pre>
    </div>
  )
}

function DetailLoadingState() {
  return (
    <div className='space-y-3' aria-busy='true'>
      <Skeleton className='h-8 w-full rounded-md' />
      <Skeleton className='h-48 w-full rounded-md' />
    </div>
  )
}

function DetailEmptyState(props: { message: string }) {
  const { t } = useTranslation()
  return (
    <Empty className='min-h-52 rounded-md border'>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <HugeiconsIcon icon={FileNotFoundIcon} strokeWidth={2} />
        </EmptyMedia>
        <EmptyTitle>{t('No content recorded')}</EmptyTitle>
        <EmptyDescription>{props.message}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

function DetailMetadata(props: { detail: LogDetail }) {
  const { t } = useTranslation()
  const rows = [
    [t('Request ID'), props.detail.request_id],
    [t('Model'), props.detail.request_model],
    [t('Path'), props.detail.request_path],
    [t('Method'), props.detail.request_method],
    [t('Format'), props.detail.relay_format],
    [t('Status Code'), String(props.detail.status_code || '-')],
    [t('Stream'), props.detail.is_stream ? t('Enabled') : t('Disabled')],
    [t('Storage'), formatBytes(props.detail.stored_bytes)],
  ].filter((row) => row[1])

  return (
    <div className='space-y-3'>
      <dl className='grid min-w-0 grid-cols-1 gap-x-5 gap-y-2 sm:grid-cols-2'>
        {rows.map(([label, value]) => (
          <div
            key={label}
            className='grid min-w-0 grid-cols-[6rem_minmax(0,1fr)] gap-2 border-b py-1.5 text-xs last:border-b-0 sm:grid-cols-[5.5rem_minmax(0,1fr)]'
          >
            <dt className='text-muted-foreground'>{label}</dt>
            <dd className='min-w-0 font-mono break-all'>{value}</dd>
          </div>
        ))}
      </dl>
      <DetailCodeBlock
        title={t('Request Parameters')}
        value={props.detail.request_params}
        emptyText={t('No content recorded')}
      />
    </div>
  )
}

function DetailContent(props: { detail: LogDetail }) {
  const { t } = useTranslation()
  const emptyText = t('No content recorded')

  return (
    <div className='min-w-0 space-y-3'>
      {(props.detail.content_truncated || props.detail.content_omitted) && (
        <Alert>
          <HugeiconsIcon icon={AlertCircleIcon} strokeWidth={2} />
          <AlertTitle className='flex flex-wrap items-center gap-1.5'>
            {props.detail.content_truncated && (
              <StatusBadge
                label={t('Truncated')}
                variant='orange'
                size='sm'
                copyable={false}
              />
            )}
            {props.detail.content_omitted && (
              <StatusBadge
                label={t('Content omitted')}
                variant='yellow'
                size='sm'
                copyable={false}
              />
            )}
          </AlertTitle>
          {props.detail.omit_reason && (
            <AlertDescription className='break-words'>
              {props.detail.omit_reason}
            </AlertDescription>
          )}
        </Alert>
      )}

      <Tabs defaultValue='request' className='min-w-0'>
        <TabsList className='grid h-9 w-full grid-cols-3'>
          <TabsTrigger value='request'>
            <HugeiconsIcon icon={FileInputIcon} strokeWidth={2} />
            {t('Request')}
          </TabsTrigger>
          <TabsTrigger value='response'>
            <HugeiconsIcon icon={FileExportIcon} strokeWidth={2} />
            {t('Response')}
          </TabsTrigger>
          <TabsTrigger value='metadata'>
            <HugeiconsIcon icon={Database01Icon} strokeWidth={2} />
            {t('Metadata')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value='request' className='min-w-0 pt-1'>
          <DetailCodeBlock
            title={t('Request Input')}
            value={props.detail.request_body}
            emptyText={emptyText}
          />
        </TabsContent>

        <TabsContent value='response' className='min-w-0 space-y-3 pt-1'>
          <DetailCodeBlock
            title={t('Response Output')}
            value={props.detail.response_body}
            emptyText={emptyText}
          />
          {props.detail.raw_response_body && (
            <DetailCodeBlock
              title={t('Raw Response')}
              value={props.detail.raw_response_body}
              emptyText={emptyText}
            />
          )}
          {props.detail.error_body && (
            <DetailCodeBlock
              title={t('Error Content')}
              value={props.detail.error_body}
              emptyText={emptyText}
              tone='danger'
            />
          )}
        </TabsContent>

        <TabsContent value='metadata' className='min-w-0 pt-1'>
          <DetailMetadata detail={props.detail} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

export function RequestResponseDetail(props: RequestResponseDetailProps) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['log-detail', props.isAdmin, props.requestId],
    queryFn: () => getLogDetail(props.requestId, props.isAdmin),
    staleTime: 30_000,
  })

  if (query.isPending) return <DetailLoadingState />
  if (query.isError) {
    return (
      <DetailEmptyState
        message={t('Failed to load request and response details')}
      />
    )
  }
  if (!query.data?.success || !query.data.data) {
    return (
      <DetailEmptyState
        message={t('No request or response detail was recorded for this log.')}
      />
    )
  }

  return <DetailContent detail={query.data.data} />
}
