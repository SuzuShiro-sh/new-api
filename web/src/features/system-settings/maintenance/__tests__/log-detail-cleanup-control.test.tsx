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
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { LogDetailCleanupControl } from '../log-detail-cleanup-control'

afterEach(() => vi.restoreAllMocks())

test('failed clear-all request keeps confirmation open and allows retry', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, message: '', data: null },
  })
  let rejectRequest: (error: Error) => void = () => {}
  const request = new Promise<never>((_resolve, reject) => {
    rejectRequest = reject
  })
  const post = vi.spyOn(api, 'post').mockReturnValue(request)
  const user = userEvent.setup()
  render(<LogDetailCleanupControl retentionDays={3} />)

  await user.click(screen.getByRole('button', { name: 'Clear all details' }))
  const dialog = await screen.findByRole('alertdialog')
  const confirm = within(dialog).getByRole('button', {
    name: 'Clear all details',
  })
  expect(confirm).toBeDisabled()
  await user.click(within(dialog).getByRole('checkbox'))
  await user.click(confirm)

  expect(screen.getByRole('alertdialog')).toBeVisible()
  expect(within(dialog).getByRole('checkbox')).toHaveAttribute(
    'aria-disabled',
    'true'
  )
  expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeDisabled()
  expect(post).toHaveBeenCalledTimes(1)
  await act(async () => rejectRequest(new Error('cleanup unavailable')))

  expect(screen.getByRole('alertdialog')).toBeVisible()
  expect(
    within(dialog).getByRole('button', { name: 'Clear all details' })
  ).toBeEnabled()
  expect(within(dialog).getByRole('checkbox')).toBeChecked()
})
