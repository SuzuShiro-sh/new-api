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
// 本文件提供请求响应详情采集、保留与内容大小设置。
import { zodResolver } from '@hookform/resolvers/zod'
import { useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

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
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { LogDetailCleanupControl } from './log-detail-cleanup-control'

const logDetailSettingsSchema = z.object({
  LogDetailEnabled: z.boolean(),
  LogDetailRetentionDays: z.number().int().min(0).max(3650),
  LogDetailMaxBodyKB: z.number().int().min(16).max(5120),
})

type LogDetailSettingsFormValues = z.infer<typeof logDetailSettingsSchema>

type LogDetailSettingsSectionProps = {
  defaultValues: LogDetailSettingsFormValues
}

export function LogDetailSettingsSection(props: LogDetailSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const defaultValues = useMemo<LogDetailSettingsFormValues>(
    () => ({
      LogDetailEnabled: props.defaultValues.LogDetailEnabled,
      LogDetailRetentionDays: props.defaultValues.LogDetailRetentionDays,
      LogDetailMaxBodyKB: props.defaultValues.LogDetailMaxBodyKB,
    }),
    [
      props.defaultValues.LogDetailEnabled,
      props.defaultValues.LogDetailMaxBodyKB,
      props.defaultValues.LogDetailRetentionDays,
    ]
  )
  const form = useForm<LogDetailSettingsFormValues>({
    resolver: zodResolver(logDetailSettingsSchema),
    defaultValues,
  })
  const retentionDays = useWatch({
    control: form.control,
    name: 'LogDetailRetentionDays',
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (values: LogDetailSettingsFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof LogDetailSettingsFormValues]
    )
    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value })
    }
  }

  return (
    <SettingsSection title={t('Request Detail Logs')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save request detail settings'
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
                      'Allow API keys with detail logging enabled to store request and response bodies. This content can be sensitive and increases database usage.'
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

          <LogDetailCleanupControl retentionDays={retentionDays} />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
