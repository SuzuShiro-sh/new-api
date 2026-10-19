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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { LogDetailSettingsSection } from '../log-detail-settings-section'

afterEach(() => vi.restoreAllMocks())

test('full capture remains off until explicitly enabled and saved', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: null },
  })
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const queryClient = new QueryClient()
  const actions = document.createElement('div')
  document.body.append(actions)
  const user = userEvent.setup()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <SettingsPageProvider actionsContainer={actions}>
        <LogDetailSettingsSection
          defaultValues={{
            LogDetailEnabled: true,
            LogDetailFullCaptureEnabled: false,
            LogDetailRetentionDays: 3,
            LogDetailMaxBodyKB: 256,
          }}
        />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  try {
    const toggle = screen.getByRole('switch', {
      name: 'Capture original traffic for debugging',
    })
    expect(toggle).not.toBeChecked()
    expect(put).not.toHaveBeenCalled()
    await user.click(toggle)
    expect(toggle).toBeChecked()
    await user.click(
      screen.getByRole('button', { name: 'Save request detail settings' })
    )
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/api/option/', {
        key: 'LogDetailFullCaptureEnabled',
        value: true,
      })
    )
  } finally {
    view.unmount()
    queryClient.clear()
    actions.remove()
  }
})
