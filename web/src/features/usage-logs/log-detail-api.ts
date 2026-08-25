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
// 本文件定义并读取单条用量日志关联的请求响应详情。
import { api } from '@/lib/api'

export interface LogDetail {
  id: number
  request_id: string
  user_id: number
  created_at: number
  updated_at: number
  request_model: string
  request_path: string
  request_method: string
  relay_format: string
  is_stream: boolean
  status_code: number
  stored_bytes: number
  request_body: string
  request_params: string
  response_body: string
  raw_response_body: string
  error_body: string
  content_truncated: boolean
  content_omitted: boolean
  omit_reason: string
}

type GetLogDetailResponse = {
  success: boolean
  message?: string
  data?: LogDetail
}

export async function getLogDetail(
  requestId: string,
  isAdmin: boolean
): Promise<GetLogDetailResponse> {
  const endpoint = isAdmin
    ? `/api/log/detail/${encodeURIComponent(requestId)}`
    : `/api/log/self/detail/${encodeURIComponent(requestId)}`
  const res = await api.get<GetLogDetailResponse>(endpoint, {
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  return res.data
}
