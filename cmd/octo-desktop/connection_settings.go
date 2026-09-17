package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// desktopConnectionService exposes only the desktop client's connection
// preference to the local Wails settings window.
type desktopConnectionService struct {
	bridge *nativeBridge
}

type desktopConnectionConfig struct {
	Mode       string `json:"mode"`
	RemoteURL  string `json:"remote_url"`
	Configured bool   `json:"configured"`
}

// Get returns the current connection preference without exposing credentials.
func (s *desktopConnectionService) Get() desktopConnectionConfig {
	s.bridge.settingsMu.Lock()
	defer s.bridge.settingsMu.Unlock()
	return desktopConnectionConfig{
		Mode:       string(s.bridge.settings.ConnectionMode),
		RemoteURL:  s.bridge.settings.RemoteURL,
		Configured: s.bridge.settings.ConnectionConfigured,
	}
}

// ValidateRemote verifies that the configured root is an Octo service before
// the desktop app persists a remote connection or restarts into it.
func (s *desktopConnectionService) ValidateRemote(remoteURL string) error {
	_, normalizedURL, err := normalizedConnection(desktopConnectionRemote, remoteURL)
	if err != nil {
		return err
	}
	return validateRemoteOctoService(normalizedURL)
}

// validateRemoteOctoService requires the Octo health endpoint to return its
// expected unauthenticated response before the URL can be saved.
func validateRemoteOctoService(remoteURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL+"/api/health", nil)
	if err != nil {
		return fmt.Errorf("创建远程服务校验请求: %w", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接远程 Octo 服务: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("远程地址不是可用的 Octo 服务（健康检查返回 %s）", res.Status)
	}

	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(res.Body).Decode(&health); err != nil {
		return fmt.Errorf("远程地址不是可用的 Octo 服务（健康检查响应无效）")
	}
	if health.Status != "ok" {
		return fmt.Errorf("远程地址不是可用的 Octo 服务（健康检查状态无效）")
	}
	return nil
}

func (s *desktopConnectionService) Save(mode, remoteURL string) error {
	connectionMode, normalizedURL, err := normalizedConnection(desktopConnectionMode(mode), remoteURL)
	if err != nil {
		return err
	}
	if connectionMode == desktopConnectionRemote {
		if err := validateRemoteOctoService(normalizedURL); err != nil {
			return err
		}
	}

	s.bridge.settingsMu.Lock()
	settings := s.bridge.settings
	settings.ConnectionMode = connectionMode
	settings.RemoteURL = normalizedURL
	settings.ConnectionConfigured = true
	s.bridge.settings = settings
	s.bridge.settingsMu.Unlock()
	if err := saveDesktopSettings(settings); err != nil {
		return fmt.Errorf("保存桌面连接设置: %w", err)
	}
	if err := relaunchSelf(); err != nil {
		return fmt.Errorf("重启 Octo: %w", err)
	}
	s.bridge.allowQuit.Store(true)
	s.bridge.app.Quit()
	return nil
}

// desktopConnectionAssets serves the local-only settings page. The main window
// continues to load the selected Octo service directly.
func desktopConnectionAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/desktop-connection" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(desktopConnectionPage))
	})
}

const desktopConnectionPage = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Octo 桌面连接</title>
<script src="/wails/runtime.js" type="module"></script>
<style>
:root { color-scheme: light dark; font-family: system-ui, sans-serif; }
body { margin: 0; padding: 28px; background: Canvas; color: CanvasText; }
main { max-width: 520px; margin: auto; }
h1 { margin: 0 0 8px; font-size: 22px; }
p { line-height: 1.5; color: GrayText; }
label { display: block; margin-top: 20px; font-weight: 600; }
select, input, button { box-sizing: border-box; width: 100%; margin-top: 8px; min-height: 40px; font: inherit; }
input, select { padding: 8px; border: 1px solid color-mix(in srgb, CanvasText 25%, transparent); border-radius: 6px; background: Canvas; color: CanvasText; }
button { margin-top: 24px; border: 0; border-radius: 6px; background: #2563eb; color: white; cursor: pointer; }
button:disabled { cursor: wait; opacity: .7; }
#status { min-height: 24px; margin-top: 12px; color: #b91c1c; }
#status.success { color: #15803d; }
</style>
</head>
<body>
<main>
<h1>桌面连接</h1>
<p>选择由桌面应用启动本机 Octo 服务，或直接连接远程 Octo 服务。保存后会自动重启应用。</p>
<label for="mode">连接方式</label>
<select id="mode"><option value="local">本机服务</option><option value="remote">远程服务</option></select>
<label for="url">远程服务地址</label>
<input id="url" type="url" placeholder="https://octo.example.com" autocomplete="url">
<button id="save">保存并重启</button>
<div id="status" role="alert"></div>
</main>
<script>
const mode = document.getElementById('mode');
const url = document.getElementById('url');
const save = document.getElementById('save');
const status = document.getElementById('status');
const call = (...args) => globalThis.wails?.Call?.ByName(...args);
const update = () => { url.disabled = mode.value !== 'remote'; };
const validate = async () => {
  if (mode.value !== 'remote') return true;
  status.className = '';
  status.textContent = '正在验证远程 Octo 服务…';
  save.disabled = true;
  try {
    await call('main.desktopConnectionService.ValidateRemote', url.value);
    status.className = 'success';
    status.textContent = '远程 Octo 服务验证成功。';
    save.disabled = false;
    return true;
  } catch (error) {
    fail('远程地址不可用：' + String(error));
    return false;
  }
};
const fail = message => {
  status.className = '';
  status.textContent = message;
  save.disabled = false;
};
const ready = async () => {
  for (let i = 0; i < 50 && !globalThis.wails?.Call?.ByName; i++) {
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  if (!globalThis.wails?.Call?.ByName) {
    fail('桌面连接功能未加载，请关闭后重新打开此窗口。');
    return false;
  }
  return true;
};
mode.addEventListener('change', update);
(async () => {
  if (!await ready()) return;
  try {
    const config = await call('main.desktopConnectionService.Get');
    mode.value = config.mode || 'local';
    url.value = config.remote_url || '';
    update();
  } catch (error) {
    fail('无法读取桌面连接设置：' + String(error));
  }
})();
save.addEventListener('click', async () => {
  status.className = '';
  status.textContent = '';
  save.disabled = true;
  try {
    if (!await ready()) return;
    if (!await validate()) return;
    save.disabled = true;
    status.className = '';
    status.textContent = '正在保存并重启…';
    await call('main.desktopConnectionService.Save', mode.value, url.value);
    status.className = 'success';
    status.textContent = '设置已保存，正在重启并连接服务…';
  } catch (error) {
    fail('保存失败：' + String(error));
  }
});
</script>
</body>
</html>`

// connectionSettingsWindow opens the local Wails page that owns the desktop
// connection preference, independent from the selected Octo service.
func (b *nativeBridge) connectionSettingsWindow() {
	if b.app == nil {
		return
	}
	if window, ok := b.app.Window.GetByName("desktop-connection"); ok {
		window.Show()
		window.Focus()
		return
	}
	b.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "desktop-connection",
		Title:     "Octo 桌面连接",
		Frameless: runtime.GOOS != "darwin",
		Width:     580,
		Height:    480,
		MinWidth:  480,
		MinHeight: 400,
		URL:       "/desktop-connection",
	})
}

// isRemoteConnection reports whether this desktop process is a remote client.
func (b *nativeBridge) isRemoteConnection() bool {
	b.settingsMu.Lock()
	defer b.settingsMu.Unlock()
	return b.settings.ConnectionMode == desktopConnectionRemote
}

// connectionLabel returns the selected remote host for native status surfaces.
func (b *nativeBridge) connectionLabel() string {
	b.settingsMu.Lock()
	defer b.settingsMu.Unlock()
	if b.settings.ConnectionMode != desktopConnectionRemote {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(b.settings.RemoteURL, "https://"), "http://")
}
