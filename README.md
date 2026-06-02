# Mantis 項目提醒工具

這個工具會抓取指定的 Mantis 待辦清單，記錄已看過的項目。之後只要清單出現新的項目編號，就會跳 Windows 通知。

也可以產生 HTML 報表，方便查看目前待辦。

## 第一次使用

```powershell
cd C:\Users\A25228\Documents\Mantis
Copy-Item .\config.sample.json .\config.json
powershell -ExecutionPolicy Bypass -File .\scripts\Set-MantisCredential.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1 -InitializeOnly
```

`Set-MantisCredential.ps1` 會要求輸入 Mantis 帳號密碼。  
`-InitializeOnly` 會把目前既有項目記成已看過，避免第一次全部跳通知。

## 日常使用

檢查是否有新項目：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1
```

產生 HTML 報表：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1 -HtmlReport
```

報表位置：

```text
C:\Users\A25228\Documents\Mantis\reports\mantis-items.html
```

測試通知：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1 -TestNotification
```

建立每 10 分鐘自動檢查：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Register-MantisWatcherTask.ps1 -Minutes 10
```

## 常用檔案

- `config.json`: Mantis 網址與工具設定。
- `data\mantis-seen-items.json`: 已看過的項目編號。
- `reports\mantis-items.html`: HTML 報表。
- `logs\mantis-watcher.log`: 執行紀錄。
- `secrets\mantis-credential.xml`: 本機加密後的帳密。

## 重新設定

如果換了 Mantis 篩選條件，請重新初始化：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Check-MantisNewItems.ps1 -InitializeOnly
```

如果帳密錯誤，重新輸入帳密：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\Set-MantisCredential.ps1
```

## 注意

`secrets\mantis-credential.xml` 只能由目前 Windows 使用者在這台電腦解密，不要分享給其他人。
