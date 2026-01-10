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

import React, { useState, useRef } from 'react';
import {
  Button,
  Card,
  Form,
  Row,
  Col,
  Modal,
  Upload,
  Radio,
  RadioGroup,
  Checkbox,
  CheckboxGroup,
  Typography,
  Banner,
  Spin,
  Table,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess, showWarning } from '../../../helpers';
import { useTranslation } from 'react-i18next';

const { Text, Title } = Typography;

const SettingsBackup = () => {
  const { t } = useTranslation();
  const [exportLoading, setExportLoading] = useState(false);
  const [importLoading, setImportLoading] = useState(false);
  const [showImportModal, setShowImportModal] = useState(false);
  const [showResultModal, setShowResultModal] = useState(false);
  const [importResults, setImportResults] = useState([]);
  const [importFile, setImportFile] = useState(null);
  const [importSettings, setImportSettings] = useState({
    conflictStrategy: 'skip',
    dryRun: true,
  });
  const [exportSettings, setExportSettings] = useState({
    includeSensitive: false,
    tables: ['channels', 'users', 'tokens', 'options', 'prefill_groups'],
  });

  const tableOptions = [
    { label: t('渠道'), value: 'channels' },
    { label: t('用户'), value: 'users' },
    { label: t('令牌'), value: 'tokens' },
    { label: t('系统配置'), value: 'options' },
    { label: t('预填充组'), value: 'prefill_groups' },
  ];

  // 导出备份
  const handleExport = async () => {
    if (exportSettings.tables.length === 0) {
      showWarning(t('请至少选择一个要导出的表'));
      return;
    }

    setExportLoading(true);
    try {
      const res = await API.post('/api/backup/export', {
        include_sensitive: exportSettings.includeSensitive,
        tables: exportSettings.tables,
      });

      const { success, message, data } = res.data;
      if (success) {
        // 创建下载链接
        const blob = new Blob([JSON.stringify(data, null, 2)], {
          type: 'application/json',
        });
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `backup_${new Date().toISOString().slice(0, 10)}.json`;
        document.body.appendChild(a);
        a.click();
        window.URL.revokeObjectURL(url);
        document.body.removeChild(a);
        showSuccess(t('备份导出成功'));
      } else {
        showError(message || t('导出失败'));
      }
    } catch (error) {
      console.error('Export error:', error);
      showError(t('导出失败') + ': ' + (error.message || t('未知错误')));
    } finally {
      setExportLoading(false);
    }
  };

  // 处理文件上传
  const handleFileChange = (info) => {
    if (info.fileList.length > 0) {
      const file = info.fileList[info.fileList.length - 1].fileInstance;
      setImportFile(file);
    } else {
      setImportFile(null);
    }
  };

  // 导入备份
  const handleImport = async () => {
    if (!importFile) {
      showWarning(t('请先选择备份文件'));
      return;
    }

    setImportLoading(true);
    try {
      const formData = new FormData();
      formData.append('file', importFile);

      const queryParams = new URLSearchParams({
        conflict_strategy: importSettings.conflictStrategy,
        dry_run: importSettings.dryRun.toString(),
      });

      const res = await API.post(
        `/api/backup/import?${queryParams.toString()}`,
        formData,
        {
          headers: {
            'Content-Type': 'multipart/form-data',
          },
        },
      );

      const { success, message, data } = res.data;
      if (success) {
        setImportResults(data || []);
        setShowResultModal(true);
        setShowImportModal(false);
        if (importSettings.dryRun) {
          showSuccess(t('预览完成，请查看结果'));
        } else {
          showSuccess(t('导入成功'));
        }
      } else {
        showError(message || t('导入失败'));
      }
    } catch (error) {
      console.error('Import error:', error);
      showError(t('导入失败') + ': ' + (error.message || t('未知错误')));
    } finally {
      setImportLoading(false);
    }
  };

  // 结果表格列定义
  const resultColumns = [
    {
      title: t('表'),
      dataIndex: 'table',
      key: 'table',
    },
    {
      title: t('总数'),
      dataIndex: 'total',
      key: 'total',
    },
    {
      title: t('新增'),
      dataIndex: 'created',
      key: 'created',
    },
    {
      title: t('更新'),
      dataIndex: 'updated',
      key: 'updated',
    },
    {
      title: t('跳过'),
      dataIndex: 'skipped',
      key: 'skipped',
    },
    {
      title: t('错误'),
      dataIndex: 'errors',
      key: 'errors',
      render: (errors) => (errors && errors.length > 0 ? errors.length : 0),
    },
  ];

  return (
    <Row>
      <Col
        span={24}
        style={{
          marginTop: '10px',
          display: 'flex',
          flexDirection: 'column',
          gap: '10px',
        }}
      >
        {/* 导出备份 */}
        <Card>
          <Form>
            <Form.Section text={t('导出备份')}>
              <Banner
                type="info"
                description={t(
                  '导出数据库配置到 JSON 文件，可用于迁移或备份。敏感数据（如 API Key、密码）默认会被脱敏处理。',
                )}
                closeIcon={null}
                style={{ marginBottom: 16 }}
              />

              <Row gutter={16}>
                <Col span={24}>
                  <Form.Slot label={t('选择要导出的表')}>
                    <CheckboxGroup
                      value={exportSettings.tables}
                      onChange={(values) =>
                        setExportSettings({ ...exportSettings, tables: values })
                      }
                      options={tableOptions}
                      direction="horizontal"
                    />
                  </Form.Slot>
                </Col>
              </Row>

              <Row gutter={16} style={{ marginTop: 16 }}>
                <Col span={24}>
                  <Checkbox
                    checked={exportSettings.includeSensitive}
                    onChange={(e) =>
                      setExportSettings({
                        ...exportSettings,
                        includeSensitive: e.target.checked,
                      })
                    }
                  >
                    {t('包含敏感数据（API Key、密码等）')}
                  </Checkbox>
                  {exportSettings.includeSensitive && (
                    <Banner
                      type="warning"
                      description={t(
                        '警告：导出的文件将包含敏感信息，请妥善保管！',
                      )}
                      closeIcon={null}
                      style={{ marginTop: 8 }}
                    />
                  )}
                </Col>
              </Row>

              <Row style={{ marginTop: 16 }}>
                <Button
                  type="primary"
                  onClick={handleExport}
                  loading={exportLoading}
                >
                  {t('导出备份')}
                </Button>
              </Row>
            </Form.Section>
          </Form>
        </Card>

        {/* 导入备份 */}
        <Card>
          <Form>
            <Form.Section text={t('导入备份')}>
              <Banner
                type="info"
                description={t(
                  '从 JSON 备份文件恢复数据。建议先使用预览模式查看将要导入的内容。',
                )}
                closeIcon={null}
                style={{ marginBottom: 16 }}
              />

              <Row>
                <Button
                  type="primary"
                  onClick={() => setShowImportModal(true)}
                >
                  {t('选择文件并导入')}
                </Button>
              </Row>
            </Form.Section>
          </Form>
        </Card>
      </Col>

      {/* 导入设置弹窗 */}
      <Modal
        title={t('导入备份')}
        visible={showImportModal}
        onCancel={() => {
          setShowImportModal(false);
          setImportFile(null);
        }}
        footer={
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
            <Button onClick={() => setShowImportModal(false)}>
              {t('取消')}
            </Button>
            <Button
              type="primary"
              onClick={handleImport}
              loading={importLoading}
              disabled={!importFile}
            >
              {importSettings.dryRun ? t('预览') : t('导入')}
            </Button>
          </div>
        }
        width={600}
      >
        <Spin spinning={importLoading}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            <div>
              <Text strong>{t('选择备份文件')}</Text>
              <Upload
                action=""
                accept=".json"
                limit={1}
                onChange={handleFileChange}
                onRemove={() => setImportFile(null)}
                customRequest={({ onSuccess }) => {
                  setTimeout(() => onSuccess('ok'), 0);
                }}
              >
                <Button style={{ marginTop: 8 }}>{t('选择文件')}</Button>
              </Upload>
              {importFile && (
                <Text type="secondary" style={{ marginTop: 8, display: 'block' }}>
                  {t('已选择')}: {importFile.name}
                </Text>
              )}
            </div>

            <div>
              <Text strong>{t('冲突处理策略')}</Text>
              <RadioGroup
                value={importSettings.conflictStrategy}
                onChange={(e) =>
                  setImportSettings({
                    ...importSettings,
                    conflictStrategy: e.target.value,
                  })
                }
                style={{ marginTop: 8 }}
              >
                <Radio value="skip">{t('跳过已存在的记录')}</Radio>
                <Radio value="overwrite">{t('覆盖已存在的记录')}</Radio>
              </RadioGroup>
            </div>

            <div>
              <Checkbox
                checked={importSettings.dryRun}
                onChange={(e) =>
                  setImportSettings({
                    ...importSettings,
                    dryRun: e.target.checked,
                  })
                }
              >
                {t('预览模式（不实际写入数据）')}
              </Checkbox>
              <Text
                type="secondary"
                style={{ display: 'block', marginTop: 4, fontSize: 12 }}
              >
                {t('建议先开启预览模式查看导入结果，确认无误后再关闭预览模式进行实际导入')}
              </Text>
            </div>

            {!importSettings.dryRun && (
              <Banner
                type="warning"
                description={t(
                  '警告：关闭预览模式后，数据将被实际写入数据库！',
                )}
                closeIcon={null}
              />
            )}
          </div>
        </Spin>
      </Modal>

      {/* 导入结果弹窗 */}
      <Modal
        title={
          importSettings.dryRun ? t('预览结果') : t('导入结果')
        }
        visible={showResultModal}
        onCancel={() => setShowResultModal(false)}
        footer={
          <Button onClick={() => setShowResultModal(false)}>{t('关闭')}</Button>
        }
        width={700}
      >
        <Table
          columns={resultColumns}
          dataSource={importResults}
          pagination={false}
          rowKey="table"
        />
        {importResults.some((r) => r.errors && r.errors.length > 0) && (
          <div style={{ marginTop: 16 }}>
            <Text strong type="danger">
              {t('错误详情')}:
            </Text>
            {importResults
              .filter((r) => r.errors && r.errors.length > 0)
              .map((r) => (
                <div key={r.table} style={{ marginTop: 8 }}>
                  <Text strong>{r.table}:</Text>
                  <ul style={{ margin: '4px 0', paddingLeft: 20 }}>
                    {r.errors.map((err, idx) => (
                      <li key={idx}>
                        <Text type="danger">{err}</Text>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
          </div>
        )}
      </Modal>
    </Row>
  );
};

export default SettingsBackup;
