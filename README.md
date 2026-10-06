# Mantis 小幫手（Mantis Report Watcher）

自動彙整 Mantis 上指派給你的項目，有新項目或項目更新時跳出 Windows 通知。

v2 起改用 **Mantis 官方 API**（SOAP / REST）取得資料，不再解析網頁 HTML，也不需要再用 PowerShell 或終端機輸入帳號。

![icon](assets/app.png)

## 功能

- **單一 exe**：免安裝、免 PowerShell，雙擊就能用。
- **登入畫面**：輸入 Mantis 網址、帳號、密碼即可；可勾選「記住我」，密碼會用 Windows DPAPI 加密存在本機（只有你的 Windows 帳號能解密）。
- **依狀態分組**：進行中／已分配／待 CR／已完成／測試中／其他，點上方卡片可只看該分類。
- **新項目與更新提醒**
  - 新指派的項目標示 `NEW`。
  - 已讀過的項目若有變動，標示「有更新」並寫出原因，例如「狀態 已分配 → 待CR」「1 則新留言（陳雅婷）」。
  - 自己留言造成的更新不會提醒自己。
  - 展開項目就自動標為已讀，也可以「全部標為已讀」。
- **詳細內容**：描述、重現步驟、附加資訊、最近 5 則留言，支援常見 Markdown（標題、清單、程式碼區塊）；`#12345` 會自動變成連結。
- **搜尋與排序**：搜尋編號、標題、專案、版本（`Ctrl+F`）；依更新時間／編號／目標版本排序；「未讀」篩選。
- **背景執行**：關閉視窗會縮到系統匣，持續定時檢查（5～60 分鐘可調），有未讀時系統匣圖示會出現紅點。
- **開機自動啟動**：在設定中開啟即可（取代舊版的「工作排程器」）。
- **檢視範圍**：指派給我（預設，未解決）、我回報的、我監看的，或 Mantis 上儲存的篩選器。
- **深色／淺色主題**：預設跟隨 Windows。

## 使用方式

1. 執行 `MantisWatcher.exe`。
2. 在登入畫面填入：
   - **Mantis 網址**：例如 `http://伺服器/mantisbt`。也可以直接貼上瀏覽器網址列上任何 Mantis 頁面（例如舊版設定檔裡的 `search.php?...`），程式會自動找出根目錄。
   - **帳號／密碼**：與登入 Mantis 網頁相同。
3. 登入後就會看到整理好的清單。關閉視窗後程式仍在系統匣，左鍵點圖示可再次開啟，右鍵可「立即更新」或「結束」。

### 連線方式

| 方式 | 說明 |
| --- | --- |
| 自動（預設） | 先用 **SOAP API**（`api/soap/mantisconnect.php`，直接用帳號密碼）；若伺服器關閉 SOAP，改用 **REST API**（以帳號密碼登入取得工作階段）。 |
| API Token | 進階選項勾選「改用 API Token 登入」。Token 在 Mantis「我的帳號 → API Token」建立，走 **REST API**。 |

公司若使用自簽 HTTPS 憑證，可在進階選項勾選「略過 HTTPS 憑證檢查」。

### 系統需求

- Windows 10 / 11（64 位元）
- Microsoft Edge WebView2 Runtime：Windows 11 與大多數 Windows 10 已內建；若缺少，程式會提示並開啟微軟下載頁面。

## 資料存放位置

`%LOCALAPPDATA%\MantisWatcher\`

| 檔案 | 內容 |
| --- | --- |
| `settings.json` | 網址、帳號、偏好設定（不含密碼） |
| `credentials.bin` | DPAPI 加密的密碼／Token（未勾選「記住我」時不會建立） |
| `state.json` | 已讀狀態、通知紀錄 |
| `cache.json` | 上次同步的資料（開啟時立即顯示） |
| `logs\app.log` | 執行紀錄 |

設定面板的「資料與紀錄檔 → 開啟」可直接打開這個資料夾。要完全重設，結束程式後刪除此資料夾即可。

## 從舊版（PowerShell）升級

舊版的 PowerShell 腳本已移除。如果之前註冊過排程工作，請移除，避免舊腳本繼續執行：

```powershell
Unregister-ScheduledTask -TaskName MantisNewItemWatcher, MantisReport -Confirm:$false
```

## 疑難排解

- **連線逾時**：內網位址（192.168.x、10.x 等）一律直接連線，不經過公司 Proxy；其他網址依 Windows 的 Proxy 設定。錯誤訊息會標示是「直接連線」還是「經由 Proxy」。
- **伺服器沒裝 PHP SOAP 模組**：程式會自動改用 REST API，不需要另外設定。
- **重新開啟 exe 卻還是舊畫面**：程式同時只能執行一個，請先在系統匣圖示按右鍵 →「結束」，再開新版。

## 開發

需要 Go 1.22 以上。

```powershell
# Windows
.\build.ps1            # 產生 dist\MantisWatcher.exe
```

```sh
# macOS / Linux 交叉編譯
./build.sh
```

### 不用 Windows 也能測試介面

非 Windows 環境執行時，程式會改成在瀏覽器提供同一套介面，並搭配模擬的 Mantis 伺服器：

```sh
go run ./tools/mockmantis                  # 模擬 Mantis：帳號 demo / 密碼 demo，Token：demotoken
go run ./tools/mockmantis -addr 127.0.0.1:8098 -nosoap   # 模擬「SOAP 已停用」的伺服器
go run ./cmd/mantis-watcher                # 開啟 http://127.0.0.1:8765
```

模擬伺服器提供 `GET /mock/add`（新增一筆指派給 demo 的項目）與 `GET /mock/touch?id=N`（更新某筆項目並加一則留言），方便測試通知。

```sh
go test ./...
MANTIS_TEST_URL=http://127.0.0.1:8099/mantisbt go test ./internal/mantis   # 對模擬伺服器測試三種連線方式
```

### 專案結構

```
cmd/mantis-watcher/     主程式（host_windows.go：WebView2 視窗與系統匣；host_dev.go：瀏覽器開發模式）
internal/mantis/        Mantis API 用戶端（SOAP、REST、連線策略）
internal/app/           設定、加密憑證、定時同步、新項目／更新判斷
internal/ui/            介面（單一 HTML，內嵌在 exe 中）
internal/winapi/        Win32：DPAPI、系統匣通知、開機啟動、單一執行個體
assets/                 圖示（由 tools/genicon.py 產生）
tools/mockmantis/       測試用的模擬 Mantis 伺服器
```
