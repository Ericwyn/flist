# 媒体预览前后切换实现细节

本文档对应 `20260831-media-preview-navigation-overview.md`，用于指导代码改造、测试和 review。

## 改动范围

| 模块 | 文件 | 改动 |
| --- | --- | --- |
| 媒体导航 helper | `frontend/src/lib/mediaNavigation.ts` | 媒体识别、目录 / 搜索队列和邻项计算 |
| 媒体导航测试 | `frontend/src/lib/mediaNavigation.test.ts` | 顺序、过滤、混合媒体和边界用例 |
| 预览 UI | `frontend/src/components/PreviewModal.tsx` | 派生队列、左右按钮、方向键、媒体重挂载 |
| React 类型与脚本 | `frontend/package.json` / `frontend/package-lock.json` | 增加 `npm test`、React 19 类型声明并修复传递依赖安全版本 |
| 通用模态框类型兼容 | `frontend/src/components/Modal.tsx` | 为 React 19 `useRef` 显式传入初始值 |
| 快捷键文档 | `README.md` | 记录预览内 `←` / `→` |
| 嵌入产物 | `web/dist` | 同步生产前端构建结果 |

不修改 `fsStore` 的状态结构，不修改 `FileBrowser` 的打开协议，也不修改任何 Go 源码或 HTTP API。

## 1. 媒体导航结构

`frontend/src/lib/mediaNavigation.ts` 新增：

```ts
export interface MediaPreviewItem {
  entry: FileEntry;
  path: string;
}

export interface MediaNavigation {
  currentIndex: number;
  total: number;
  previous: MediaPreviewItem | null;
  next: MediaPreviewItem | null;
}
```

职责边界：

1. helper 只负责建立有序候选队列和计算邻项。
2. helper 不读 Zustand store、不触发 React 状态、不发请求。
3. `PreviewModal` 负责选择目录 / 搜索数据源，并调用现有 `openPreview()`。

## 2. 可切换媒体判断

```ts
isSwitchableMedia(entry)
```

复用 `frontend/src/lib/path.ts` 的 `kindOf()`，仅接受：

```text
image | video | audio
```

不复制扩展名集合，避免文件图标、现有预览能力和媒体切换队列发生类型漂移。

## 3. 普通目录队列

```ts
buildDirectoryMediaItems(entries, currentPath)
```

处理顺序：

1. 遍历 `entries`，不调用 `sort()`。
2. 过滤 `entry.unreachable`。
3. 过滤非图片 / 视频 / 音频。
4. 用 `joinPath(currentPath, entry.name)` 生成完整 API 路径。
5. 以原数组顺序返回 `MediaPreviewItem[]`。

`entries` 来自：

```ts
api.fs.list(path, { sort, order, showHidden, pageSize: 1000 })
```

因此“不排序”是保证服从用户当前排序逻辑的关键，而不是遗漏排序。

## 4. 搜索结果队列

```ts
buildSearchMediaItems(searchResults)
```

搜索结果已有完整 `hit.path`，直接沿用。`SearchHit` 转换为 `FileEntry` 时补充：

```ts
isSymlink: false
```

其余 `name`、`type`、`size`、`mode`、`modTime` 保持原值。搜索页面没有独立排序控件，所以函数只过滤、不重排。

## 5. 邻项计算

```ts
getMediaNavigation(items, previewPath)
```

算法：

```text
currentIndex = items.findIndex(item.path == previewPath)
previous = currentIndex > 0 ? items[currentIndex - 1] : null
next = 0 <= currentIndex < total - 1 ? items[currentIndex + 1] : null
```

边界规则：

1. 当前路径不存在时 `currentIndex = -1`，两侧均为 `null`。
2. 首项 `previous = null`。
3. 末项 `next = null`。
4. 不做取模，不循环到另一端。

## 6. PreviewModal 接入

### Store 数据

`PreviewModal` 在原 `previewEntry / previewPath / closePreview` 基础上读取：

```ts
openPreview
currentPath
entries
searchOpen
searchResults
```

使用 `useMemo` 构建队列：

```ts
const mediaItems = searchOpen
  ? buildSearchMediaItems(searchResults)
  : buildDirectoryMediaItems(entries, currentPath)
```

随后以 `previewPath` 计算 `MediaNavigation`。`showMediaNavigation` 仅在当前文件位于队列且队列总数大于 1 时为 true。

### 切换动作

```ts
switchMedia(item) -> openPreview(item.entry, item.path)
```

沿用现有 store action，因而标题、下载链接和媒体 `src` 都由同一个 `previewEntry / previewPath` 原子更新，不增加第二套当前项状态。

## 7. 媒体元素生命周期

图片、视频、音频元素均增加：

```tsx
key={previewPath}
```

主要影响音视频：切换路径后 React 卸载旧播放器并挂载新播放器，原文件的播放进度、缓冲和错误状态不会泄漏到下一个文件。现有 `controls`、`autoPlay`、Range 下载和样式保持不变。

## 8. 导航按钮

`MediaNavigationButton` 接收：

```ts
direction: 'previous' | 'next'
item: MediaPreviewItem | null
onSelect(item)
```

实现规则：

1. 使用 `ChevronLeft` / `ChevronRight`。
2. 绝对定位在媒体内容区 `left-3` / `right-3`，垂直居中。
3. 尺寸 `h-10 w-10`，圆形边框、半透明背景、轻阴影和原有蓝色 focus ring。
4. 暗色样式沿用 `slate-900 / slate-700`。
5. `item == null` 时设置原生 `disabled` 和禁用透明度。
6. `aria-label` / `title` 包含目标文件名；边界态使用“已经是第一个 / 最后一个媒体”。
7. 使用 `sr-only aria-live="polite"` 宣告当前位置，不加入新的可见计数器。

## 9. 键盘处理

组件打开且至少有两个媒体时，在 `window` 注册 `keydown`，卸载或条件失效时移除。

只处理无 Ctrl / Meta / Alt / Shift 的：

```text
ArrowLeft  -> previous
ArrowRight -> next
```

以下目标不接管：

```text
input
textarea
select
audio
video
[contenteditable="true"]
.cm-editor
```

按钮焦点不在排除列表中，因此用户点击一次侧边按钮后，可直接继续按方向键浏览；音视频元素本身获得焦点时仍保留浏览器原生行为。

## 10. 保持不变的行为

1. `Modal` 的打开 / 关闭、浏览器历史守卫和未保存文本确认不变。
2. 文本编辑、过大文本 fallback、PDF blob 预览不变。
3. 媒体下载 URL 和鉴权方式不变。
4. `FileBrowser.openEntry()` / `openHit()` 的入口签名不变。
5. 排序仍由现有 `fsStore.setSort()` / `toggleOrder()` 触发后端 list 请求。
6. 视图模式、缩放、隐藏文件和搜索逻辑不变。

## 11. 测试实现

`frontend/src/lib/mediaNavigation.test.ts` 使用 Node 内置 `node:test` 和 `assert`，由现有 `tsx` 执行：

```json
{
  "test": "tsx --test src/lib/mediaNavigation.test.ts"
}
```

当前自动化用例：

1. 输入中混有目录、文本、不可达音频时，只保留可达图片 / 视频 / 音频且顺序不变。
2. 搜索结果跨路径时保留展示顺序和原始完整路径。
3. 首项、中间项、末项的 previous / next 正确，不循环。

## 12. 生产构建产物

执行：

```text
make embed-frontend
```

流程为：

```text
npm install
  -> npm run build
  -> frontend/dist
  -> 替换 web/dist
```

Vite 内容 hash 变化会删除旧文件并新增对应的新文件；`web/dist/index.html` 指向新入口、样式和 icon chunk。Go 的 `web/embed.go` 无需修改。

## 13. 完成判定

只有以下证据全部成立才视为完成：

1. 概述和实现 spec 存在且与代码行为一致。
2. 图片、视频、音频预览均显示左右按钮并可跨类型切换。
3. 名称升序 / 降序的实际切换顺序与文件列表一致。
4. 非媒体项不进入队列，不可达条目被跳过。
5. 首尾禁用、不循环，按钮有无障碍名称和键盘操作。
6. 音视频控件获得焦点时不被全局方向键处理干扰。
7. 前端单元测试、相关 TypeScript 校验、生产构建、Go 测试和 vet 通过。
8. 新前端产物同步到 `web/dist`。

## 14. 本次实施验证结果

1. `npm test`：3 个媒体队列 / 边界测试全部通过。
2. 补齐 `@types/react` / `@types/react-dom` 后，全量 `npm run lint` 通过；`Modal` 的 ID ref 按 React 19 类型要求显式初始化为 `undefined`。
3. `npm run build` 和 `make embed-frontend` 通过，产物已同步到 `web/dist`。
4. `go test ./...`、`go vet ./...` 通过。
5. `git diff --check` 通过。
6. 浏览器以实际图片、WAV、MP4 和第二张图片验证名称升序顺序：`01 -> 02 -> 03 -> 04`。
7. 切换名称降序后验证顺序反转：`04 -> 03 -> 02 -> 01`。
8. 首尾禁用态、鼠标点击、点击后继续方向键、视频焦点方向键避让均通过。
9. 图片、音频和视频三种布局均完成视觉检查，未引入新视觉体系；浏览器控制台无 warning / error。
10. `npm audit` 为 0 vulnerabilities；`body-parser`、`nanoid`、`postcss`、`protobufjs` 通过非强制补丁级传递依赖升级消除审计提示。

## 15. 回滚

1. 还原 `PreviewModal.tsx`、`Modal.tsx`、媒体导航 helper / 测试、前端依赖文件和 README。
2. 重新执行前端生产构建并同步 `web/dist`，避免源码和嵌入产物版本不一致。
3. 无数据库、配置、服务端协议或持久数据需要回滚。
