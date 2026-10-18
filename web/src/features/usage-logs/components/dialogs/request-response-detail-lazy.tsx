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
// 本文件隔离请求响应详情查看器的懒加载边界。
import { lazy, Suspense } from 'react'

const RequestResponseDetail = lazy(() =>
  import('./request-response-detail').then((module) => ({
    default: module.RequestResponseDetail,
  }))
)

type RequestResponseDetailLazyProps = {
  requestId: string
  isAdmin: boolean
}

export function RequestResponseDetailLazy(
  props: RequestResponseDetailLazyProps
) {
  return (
    <Suspense
      fallback={
        <div className='space-y-3' aria-busy='true'>
          <div className='bg-muted h-8 animate-pulse rounded-md' />
          <div className='bg-muted h-48 animate-pulse rounded-md' />
        </div>
      }
    >
      <RequestResponseDetail {...props} />
    </Suspense>
  )
}
