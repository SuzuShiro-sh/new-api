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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { apiKeySchema, type ApiKey } from '../../types'
import {
  getApiKeyFormDefaultValues,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from '../api-key-form'

const apiKeyFixture: ApiKey = {
  id: 1,
  name: 'diagnostic-key',
  key: 'masked-key',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  log_detail_enabled: true,
  expired_time: -1,
  created_time: 1,
  accessed_time: 1,
  group: 'default',
  cross_group_retry: false,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
}

describe('API key log detail form', () => {
  test('defaults new and legacy API keys to disabled', () => {
    const defaults = getApiKeyFormDefaultValues(false)
    const legacy = { ...apiKeyFixture }
    delete (legacy as Partial<ApiKey>).log_detail_enabled

    assert.equal(defaults.log_detail_enabled, false)
    assert.equal(apiKeySchema.parse(legacy).log_detail_enabled, false)
  })

  test('preserves explicit opt-in through create and edit transformations', () => {
    const values = getApiKeyFormDefaultValues(false)
    values.log_detail_enabled = true

    assert.equal(transformFormDataToPayload(values).log_detail_enabled, true)
    assert.equal(
      transformApiKeyToFormDefaults(apiKeyFixture).log_detail_enabled,
      true
    )
  })
})
