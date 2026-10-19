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

import { RequestResponseDetail } from '../dialogs/request-response-detail'

afterEach(() => vi.restoreAllMocks())

test('shows and copies large integers and whitespace without rewriting the original body', async () => {
  const body = ' {"seed":9007199254740993,"value":1.0000000000000001} \n'
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: { request_id: 'request', request_body: body },
    },
  })
  const user = userEvent.setup()
  const copy = vi.spyOn(navigator.clipboard, 'writeText')
  const queryClient = new QueryClient()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RequestResponseDetail requestId='request' isAdmin />
    </QueryClientProvider>
  )
  await screen.findByRole('button', { name: 'Copy to clipboard' })
  expect(view.container.querySelector('pre')?.textContent).toBe(body)
  await user.click(screen.getByRole('button', { name: 'Copy to clipboard' }))
  expect(copy).toHaveBeenCalledWith(body)
  queryClient.clear()
})

test('reopening a deleted detail fetches its current state', async () => {
  const get = vi
    .spyOn(api, 'get')
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: { request_id: 'deleted', request_body: 'removed-body' },
      },
    })
    .mockResolvedValue({ data: { success: false } })
  const queryClient = new QueryClient()
  const component = (
    <QueryClientProvider client={queryClient}>
      <RequestResponseDetail requestId='deleted' isAdmin />
    </QueryClientProvider>
  )
  const first = render(component)
  await screen.findByText('removed-body')
  first.unmount()
  render(component)
  await screen.findByText(
    'No request or response detail was recorded for this log.'
  )
  expect(screen.queryByText('removed-body')).not.toBeInTheDocument()
  await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
  queryClient.clear()
})

test('shows incomplete archive status to administrators and hides export from users', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        request_id: 'partial',
        request_body: '',
        archive_bytes: 1024,
        archive_complete: false,
      },
    },
  })
  const queryClient = new QueryClient()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RequestResponseDetail requestId='partial' isAdmin />
    </QueryClientProvider>
  )
  await screen.findByRole('button', { name: /Download original traffic/ })
  expect(
    screen.getByText('Original traffic capture is incomplete')
  ).toBeVisible()
  view.rerender(
    <QueryClientProvider client={queryClient}>
      <RequestResponseDetail requestId='partial' isAdmin={false} />
    </QueryClientProvider>
  )
  await screen.findByRole('tab', { name: 'Request' })
  expect(
    screen.queryByRole('button', { name: /Download original traffic/ })
  ).not.toBeInTheDocument()
  queryClient.clear()
})
