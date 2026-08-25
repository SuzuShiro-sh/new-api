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
// 本文件验证 API Key 有限额度的显示、换算与后端整数边界。
import type { TFunction } from 'i18next'
import { afterEach, describe, expect, test } from 'vitest'

import { getCurrencyDisplay } from '@/lib/currency'
import { quotaUnitsToDollars } from '@/lib/format'
import {
  DEFAULT_CURRENCY_CONFIG,
  type CurrencyConfig,
  type CurrencyDisplayType,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { MAX_API_KEY_QUOTA_UNITS } from '../../constants'
import type { ApiKey } from '../../types'
import {
  getApiKeyFormDefaultValues,
  getApiKeyFormSchema,
  getApiKeyQuotaLimit,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from '../api-key-form'

const t = ((key: string, options?: Record<string, unknown>) => {
  if (options?.max !== undefined) {
    return key.replace('{{max}}', String(options.max))
  }
  return key
}) as TFunction

function setQuotaDisplay(
  quotaDisplayType: CurrencyDisplayType,
  overrides: Partial<CurrencyConfig> = {}
) {
  useSystemConfigStore.getState().setConfig({
    currency: {
      ...DEFAULT_CURRENCY_CONFIG,
      quotaDisplayType,
      ...overrides,
    },
  })
  return getCurrencyDisplay()
}

function finiteQuotaValues(amount: number, display = getCurrencyDisplay()) {
  return {
    ...getApiKeyFormDefaultValues(false, display),
    name: 'limited-key',
    unlimited_quota: false,
    remain_quota_dollars: amount,
  }
}

function apiKeyFixture(remainQuota: number): ApiKey {
  return {
    id: 1,
    name: 'quota-key',
    key: 'sk-test',
    status: 1,
    remain_quota: remainQuota,
    used_quota: 0,
    unlimited_quota: false,
    log_detail_enabled: false,
    expired_time: -1,
    created_time: 1,
    accessed_time: 0,
    group: '',
    auto_groups: null,
    cross_group_retry: false,
    model_limits_enabled: false,
    model_limits: '',
    allow_ips: '',
  }
}

afterEach(() => {
  setQuotaDisplay('USD')
  localStorage.clear()
})

describe('API key quota validation', () => {
  test.each([
    ['USD', {}],
    ['CNY', { usdExchangeRate: 7 }],
    ['CUSTOM', { customCurrencyExchangeRate: 0.9 }],
    ['TOKENS', {}],
  ] as const)(
    'accepts the exact %s maximum and rejects the next quota unit',
    (displayType, overrides) => {
      const display = setQuotaDisplay(displayType, overrides)
      const quotaLimit = getApiKeyQuotaLimit(display)
      const maximumValues = finiteQuotaValues(
        quotaLimit.maximumDisplayAmount,
        display
      )
      const aboveMaximumValues = finiteQuotaValues(
        quotaUnitsToDollars(quotaLimit.maximumUnits + 1, display),
        display
      )
      const schema = getApiKeyFormSchema(t, 5, display)

      expect(schema.safeParse(maximumValues).success).toBe(true)
      expect(
        transformFormDataToPayload(maximumValues, {
          quotaDisplay: display,
        }).remain_quota
      ).toBe(quotaLimit.maximumUnits)
      expect(schema.safeParse(aboveMaximumValues).success).toBe(false)
    }
  )

  test('accepts 10000 as raw quota in Tokens mode', () => {
    const display = setQuotaDisplay('TOKENS')
    const values = finiteQuotaValues(10_000, display)

    expect(getApiKeyFormSchema(t, 5, display).safeParse(values).success).toBe(
      true
    )
    expect(
      transformFormDataToPayload(values, { quotaDisplay: display }).remain_quota
    ).toBe(10_000)
  })

  test('uses the billion-dollar cap below the int32 threshold', () => {
    const display = setQuotaDisplay('USD', { quotaPerUnit: 0.5 })

    expect(getApiKeyQuotaLimit(display).maximumUnits).toBe(500_000_000)
  })

  test('uses the int32 cap above the billion-dollar threshold', () => {
    const display = setQuotaDisplay('USD', { quotaPerUnit: 3 })

    expect(getApiKeyQuotaLimit(display).maximumUnits).toBe(
      MAX_API_KEY_QUOTA_UNITS
    )
  })

  test('preserves untouched raw quota when global currency settings change', () => {
    const snapshot = setQuotaDisplay('USD', { quotaPerUnit: 500_000 })
    const rawQuota = 500_000
    const values = transformApiKeyToFormDefaults(
      apiKeyFixture(rawQuota),
      [],
      5,
      snapshot
    )
    setQuotaDisplay('CNY', { quotaPerUnit: 500_000, usdExchangeRate: 7 })

    expect(
      transformFormDataToPayload(values, {
        quotaDisplay: snapshot,
        originalQuotaUnits: rawQuota,
        preserveOriginalQuota: true,
      }).remain_quota
    ).toBe(rawQuota)
  })

  test('recalculates a dirty quota with the drawer display snapshot', () => {
    const snapshot = setQuotaDisplay('USD', { quotaPerUnit: 500_000 })
    const values = transformApiKeyToFormDefaults(
      apiKeyFixture(500_000),
      [],
      5,
      snapshot
    )
    values.remain_quota_dollars = 2
    setQuotaDisplay('CNY', { quotaPerUnit: 500_000, usdExchangeRate: 7 })

    expect(
      transformFormDataToPayload(values, {
        quotaDisplay: snapshot,
        originalQuotaUnits: 500_000,
        preserveOriginalQuota: false,
      }).remain_quota
    ).toBe(1_000_000)
  })

  test('blocks finite quotas when given an invalid display context', () => {
    const display = setQuotaDisplay('USD')
    const invalidDisplay = {
      ...display,
      config: { ...display.config, quotaPerUnit: Number.POSITIVE_INFINITY },
    }

    expect(getApiKeyQuotaLimit(invalidDisplay).maximumUnits).toBe(0)
    expect(
      getApiKeyFormSchema(t, 5, invalidDisplay).safeParse(
        finiteQuotaValues(1, invalidDisplay)
      ).success
    ).toBe(false)
  })
})
