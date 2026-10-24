// WKWebView 在窗口遮挡或后台启动时会暂停动画帧，但终端输出仍须及时呈现。
// 保留正常的逐帧刷新，100ms 后由计时器补偿；两个回调只执行一次，也可正常取消。
export function installFrameFallback(scope) {
  const request = scope.requestAnimationFrame.bind(scope);
  const cancel = scope.cancelAnimationFrame.bind(scope);
  const pending = new Map();
  let next = 0;
  scope.requestAnimationFrame = (callback) => {
    const id = ++next;
    const entry = {};
    const finish = (time) => {
      if (!pending.delete(id)) return;
      cancel(entry.frame);
      scope.clearTimeout(entry.timer);
      callback(time);
    };
    pending.set(id, entry);
    entry.frame = request(finish);
    entry.timer = scope.setTimeout(() => finish(scope.performance.now()), 100);
    return id;
  };
  scope.cancelAnimationFrame = (id) => {
    const entry = pending.get(id);
    if (!entry) return;
    pending.delete(id);
    cancel(entry.frame);
    scope.clearTimeout(entry.timer);
  };
}

if (typeof window !== "undefined" && window.location.protocol === "wails:") installFrameFallback(window);
