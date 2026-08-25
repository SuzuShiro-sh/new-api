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
// 本文件验证额度换算单位必须保持为有限正数。
import { describe, expect, test } from 'vitest'

import { createPricingSchema } from './pricing-schema'

const schema = createPricingSchema((key) => key)

function pricingValues(quotaPerUnit: number) {
  return {
    QuotaPerUnit: quotaPerUnit,
    USDExchangeRate: 7,
    DisplayInCurrencyEnabled: true,
    DisplayTokenStatEnabled: false,
    general_setting: {
      quota_display_type: 'USD' as const,
      custom_currency_symbol: '$',
      custom_currency_exchange_rate: 1,
    },
  }
}

describe('pricing settings quota boundary', () => {
  test.each([0, -1, Number.NaN, Number.POSITIVE_INFINITY])(
    'rejects invalid quota per unit %s',
    (value) => {
      expect(schema.safeParse(pricingValues(value)).success).toBe(false)
    }
  )

  test('accepts a finite positive quota per unit', () => {
    expect(schema.safeParse(pricingValues(0.5)).success).toBe(true)
  })
})
