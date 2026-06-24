/*
Copyright (C) 2025 QuantumNous

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

import React from 'react';
import { Modal, Button, Empty, Spin, Tag, Typography } from '@douyinfe/semi-ui';
import { IconCopy } from '@douyinfe/semi-icons';
import { copy, showError, showSuccess } from '../../../../helpers';

const { Text } = Typography;

function formatDetailText(value) {
  if (!value) {
    return '';
  }
  const trimmed = String(value).trim();
  if (!trimmed) {
    return String(value);
  }
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return String(value);
  }
}

function DetailBlock({ title, value, danger, t }) {
  const text = formatDetailText(value);
  const hasText = text.trim().length > 0;

  const copyText = async () => {
    if (!hasText) {
      return;
    }
    if (await copy(text)) {
      showSuccess(t('已复制'));
      return;
    }
    showError(t('无法复制到剪贴板，请手动复制'));
  };

  return (
    <div style={{ minWidth: 0 }}>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: 8,
          marginBottom: 6,
        }}
      >
        <Text
          strong
          size='small'
          style={danger ? { color: 'var(--semi-color-danger)' } : undefined}
        >
          {title}
        </Text>
        {hasText ? (
          <Button
            icon={<IconCopy />}
            theme='borderless'
            type='tertiary'
            size='small'
            onClick={copyText}
          >
            {t('复制')}
          </Button>
        ) : null}
      </div>
      <pre
        style={{
          maxHeight: 260,
          minHeight: 70,
          overflow: 'auto',
          margin: 0,
          padding: 12,
          borderRadius: 6,
          border: `1px solid ${
            danger ? 'var(--semi-color-danger-light-default)' : 'var(--semi-color-border)'
          }`,
          background: danger
            ? 'var(--semi-color-danger-light-default)'
            : 'var(--semi-color-fill-0)',
          color: hasText
            ? 'var(--semi-color-text-0)'
            : 'var(--semi-color-text-2)',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          fontSize: 12,
          lineHeight: 1.6,
        }}
      >
        {hasText ? text : t('暂无记录内容')}
      </pre>
    </div>
  );
}

const LogDetailModal = ({
  showLogDetailModal,
  setShowLogDetailModal,
  logDetailTarget,
  logDetail,
  loadingLogDetail,
  t,
}) => {
  const detail = logDetail || {};
  const hasDetail = [
    detail.request_body,
    detail.request_params,
    detail.response_body,
    detail.raw_response_body,
    detail.error_body,
  ].some((value) => value && String(value).trim().length > 0);

  return (
    <Modal
      title={t('请求与响应详情')}
      visible={showLogDetailModal}
      onCancel={() => setShowLogDetailModal(false)}
      footer={null}
      centered
      closable
      maskClosable
      width={860}
    >
      <Spin spinning={loadingLogDetail}>
        <div style={{ padding: '8px 20px 20px' }}>
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: 8,
              marginBottom: 14,
              color: 'var(--semi-color-text-2)',
              fontSize: 12,
            }}
          >
            {logDetailTarget?.requestId ? (
              <Text type='tertiary' size='small'>
                {t('Request ID')}: {logDetailTarget.requestId}
              </Text>
            ) : null}
            {logDetailTarget?.modelName ? (
              <Text type='tertiary' size='small'>
                {logDetailTarget.modelName}
              </Text>
            ) : null}
            {detail.content_truncated ? (
              <Tag color='orange' size='small'>
                {t('已截断')}
              </Tag>
            ) : null}
            {detail.content_omitted ? (
              <Tag color='yellow' size='small'>
                {t('内容已省略')}
              </Tag>
            ) : null}
            {detail.omit_reason ? (
              <Text type='tertiary' size='small'>
                {t('省略原因')}：{detail.omit_reason}
              </Text>
            ) : null}
          </div>

          {!loadingLogDetail && !hasDetail ? (
            <Empty
              description={t('该日志未记录请求或响应详情')}
              style={{ padding: '28px 0' }}
            />
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
              <DetailBlock
                title={t('请求输入')}
                value={detail.request_body}
                t={t}
              />
              <DetailBlock
                title={t('请求参数')}
                value={detail.request_params}
                t={t}
              />
              <DetailBlock
                title={t('返回输出')}
                value={detail.response_body}
                t={t}
              />
              <DetailBlock
                title={t('原始返回')}
                value={detail.raw_response_body}
                t={t}
              />
              <DetailBlock
                title={t('错误内容')}
                value={detail.error_body}
                danger
                t={t}
              />
            </div>
          )}
        </div>
      </Spin>
    </Modal>
  );
};

export default LogDetailModal;
