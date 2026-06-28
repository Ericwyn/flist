// modalRegistry 维护当前打开的弹窗栈（模块级，不触发组件重渲染）。
// 用于在浏览器物理前进/后退（popstate）时拦截导航：优先关闭最上层弹窗，
// 而不是切换底层目录，避免「弹窗浮在上层、底层目录却被前进后退切走」的错乱。
type Closer = () => void;

let seq = 0;
const stack: { id: number; close: Closer }[] = [];

// 栈变化订阅者（仅一个协调者：FileBrowser 的 popstate 管理）。
let listener: (() => void) | null = null;

function notify(): void {
  if (listener) listener();
}

// subscribeModal 订阅弹窗栈变化（开/关）。返回取消订阅函数。
export function subscribeModal(fn: () => void): () => void {
  listener = fn;
  return () => {
    if (listener === fn) listener = null;
  };
}

// nextModalId 分配一个稳定的弹窗 id（供组件在整个生命周期复用）。
export function nextModalId(): number {
  return ++seq;
}

// registerModal 在弹窗打开时入栈；同 id 重复注册会先移除旧项再入栈（置顶）。
export function registerModal(id: number, close: Closer): void {
  unregisterModal(id, true);
  stack.push({ id, close });
  notify();
}

// unregisterModal 在弹窗关闭/卸载时出栈。silent 为内部置顶复用，不触发通知。
export function unregisterModal(id: number, silent = false): void {
  const i = stack.findIndex((m) => m.id === id);
  if (i !== -1) {
    stack.splice(i, 1);
    if (!silent) notify();
  }
}

// hasOpenModal 是否存在打开的弹窗。
export function hasOpenModal(): boolean {
  return stack.length > 0;
}

// openModalCount 当前打开的弹窗数量（用于堆叠弹窗时维护守卫历史）。
export function openModalCount(): number {
  return stack.length;
}

// closeTopModal 关闭最上层弹窗（调用其 onClose）；栈空时返回 false。
export function closeTopModal(): boolean {
  const top = stack[stack.length - 1];
  if (!top) return false;
  top.close();
  return true;
}
