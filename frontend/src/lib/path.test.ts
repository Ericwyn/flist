import assert from 'node:assert/strict';
import test from 'node:test';
import { kindOf } from './path';

function file(name: string) {
  return { name, type: 'file' as const };
}

test('文档扩展名映射到专用预览类型', () => {
  assert.equal(kindOf(file('README.md')), 'markdown');
  assert.equal(kindOf(file('data.CSV')), 'csv');
  assert.equal(kindOf(file('budget.xlsx')), 'spreadsheet');
  assert.equal(kindOf(file('legacy.xls')), 'spreadsheet');
  assert.equal(kindOf(file('report.docx')), 'document');
  assert.equal(kindOf(file('slides.pptx')), 'presentation');
});

test('普通文本与媒体类型映射保持不变', () => {
  assert.equal(kindOf(file('main.go')), 'text');
  assert.equal(kindOf(file('photo.png')), 'image');
  assert.equal(kindOf(file('manual.pdf')), 'pdf');
});
