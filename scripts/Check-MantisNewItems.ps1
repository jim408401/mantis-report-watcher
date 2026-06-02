param(
    [string]$ConfigPath = ".\config.json",
    [switch]$InitializeOnly,
    [switch]$ListItems,
    [switch]$HtmlReport,
    [switch]$TestNotification,
    [string]$ReportPath = ".\reports\mantis-items.html"
)

$ErrorActionPreference = "Stop"

function Resolve-FromBase {
    param(
        [string]$BasePath,
        [string]$Path
    )

    if ([System.IO.Path]::IsPathRooted($Path)) {
        return $Path
    }

    return Join-Path $BasePath $Path
}

function Write-Log {
    param(
        [string]$Path,
        [string]$Message
    )

    $dir = Split-Path -Parent $Path
    if (-not (Test-Path -LiteralPath $dir)) {
        New-Item -ItemType Directory -Path $dir | Out-Null
    }

    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    Add-Content -LiteralPath $Path -Encoding UTF8 -Value "[$timestamp] $Message"
}

function Show-Balloon {
    param(
        [string]$Title,
        [string]$Text
    )

    Add-Type -AssemblyName System.Windows.Forms
    Add-Type -AssemblyName System.Drawing

    $notifyIcon = New-Object System.Windows.Forms.NotifyIcon
    $notifyIcon.Icon = [System.Drawing.SystemIcons]::Information
    $notifyIcon.BalloonTipTitle = $Title
    $notifyIcon.BalloonTipText = $Text
    $notifyIcon.Visible = $true
    $notifyIcon.ShowBalloonTip(10000)
    Start-Sleep -Seconds 12
    $notifyIcon.Dispose()
}

if ($TestNotification) {
    Show-Balloon `
        -Title "Mantis has 1 new item" `
        -Text "#9999 Test notification from Mantis watcher"
    Write-Host "Test notification sent."
    return
}

function Convert-CellText {
    param([string]$CellHtml)

    return [System.Net.WebUtility]::HtmlDecode(
        ($CellHtml -replace '<[^>]+>', ' ' -replace '\s+', ' ').Trim()
    )
}

function New-Text {
    param([int[]]$Codes)

    return -join ($Codes | ForEach-Object { [char]$_ })
}

function Get-StatusInfo {
    param([string]$StatusName)

    $name = if ($null -eq $StatusName) { "" } else { $StatusName }
    $status = $name.ToLowerInvariant()

    $progressText = New-Text @(0x9032, 0x884C, 0x4E2D)
    $assignedText = New-Text @(0x5DF2, 0x5206, 0x914D)
    $reviewText = (New-Text @(0x5F85)) + "CR"
    $doneText = New-Text @(0x5DF2, 0x5B8C, 0x6210)
    $testText = New-Text @(0x6E2C, 0x8A66, 0x4E2D)

    if ($status -match "feedback" -or $name.Contains($progressText)) {
        return [pscustomobject]@{ Key = "progress"; Order = 1; LabelText = $progressText; LabelHtml = "&#36914;&#34892;&#20013;"; Class = "status-progress"; Open = $false }
    }
    if ($status -match "assigned" -or $name.Contains($assignedText)) {
        return [pscustomobject]@{ Key = "assigned"; Order = 2; LabelText = $assignedText; LabelHtml = "&#24050;&#20998;&#37197;"; Class = "status-assigned"; Open = $false }
    }
    if ($status -match "review" -or $name.Contains($reviewText)) {
        return [pscustomobject]@{ Key = "review"; Order = 3; LabelText = $reviewText; LabelHtml = "&#24453; CR"; Class = "status-review"; Open = $false }
    }
    if ($status -match "confirmed|resolved" -or $name.Contains((New-Text @(0x5B8C, 0x6210)))) {
        return [pscustomobject]@{ Key = "done"; Order = 4; LabelText = $doneText; LabelHtml = "&#24050;&#23436;&#25104;"; Class = "status-done"; Open = $false }
    }
    if ($status -match "acknowledged|tested|testing|test") {
        return [pscustomobject]@{ Key = "testing"; Order = 5; LabelText = $testText; LabelHtml = "&#28204;&#35430;&#20013;"; Class = "status-testing"; Open = $false }
    }

    return [pscustomobject]@{ Key = "other"; Order = 99; LabelText = $name; LabelHtml = [System.Net.WebUtility]::HtmlEncode($name); Class = "status-other"; Open = $false }
}

function Get-CellHtml {
    param(
        [string]$RowHtml,
        [string]$ColumnClass
    )

    $cellMatch = [regex]::Match(
        $RowHtml,
        '<td[^>]*class=["''][^"'']*\b' + [regex]::Escape($ColumnClass) + '\b[^"'']*["''][^>]*>(?<value>.*?)</td>',
        [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
    )

    if ($cellMatch.Success) {
        return $cellMatch.Groups["value"].Value
    }

    return ""
}

function Get-MantisItemsFromHtml {
    param(
        [string]$Html,
        [string]$BaseUrl
    )

    $tableMatch = [regex]::Match(
        $Html,
        '<table[^>]+id=["'']buglist["''][^>]*>.*?<tbody>(?<body>.*?)</tbody>',
        [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
    )

    if (-not $tableMatch.Success) {
        return @()
    }

    $rowMatches = [regex]::Matches(
        $tableMatch.Groups["body"].Value,
        '<tr[^>]*>(?<row>.*?)</tr>',
        [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
    )

    $items = foreach ($rowMatch in $rowMatches) {
        $row = $rowMatch.Groups["row"].Value
        $idCell = Get-CellHtml -RowHtml $row -ColumnClass "column-id"
        $idMatch = [regex]::Match(
            $idCell,
            '<a[^>]+href=["''](?<href>[^"'']*view\.php\?id=(?<id>\d+)[^"'']*)["''][^>]*>',
            [System.Text.RegularExpressions.RegexOptions]::IgnoreCase
        )

        if (-not $idMatch.Success) {
            continue
        }

        $statusCell = Get-CellHtml -RowHtml $row -ColumnClass "column-status"
        $status = Convert-CellText $statusCell
        $statusNameMatch = [regex]::Match(
            $statusCell,
            '<span[^>]*>(?<status>.*?)</span>',
            [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
        )
        $handlerMatch = [regex]::Match(
            $statusCell,
            '<a[^>]+view_user_page\.php\?id=\d+[^>]*>(?<handler>.*?)</a>',
            [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
        )

        $id = $idMatch.Groups["id"].Value
        $href = [System.Net.WebUtility]::HtmlDecode($idMatch.Groups["href"].Value)
        $uri = [System.Uri]::new([System.Uri]$BaseUrl, $href)

        [pscustomobject]@{
            Id = $id
            Title = Convert-CellText (Get-CellHtml -RowHtml $row -ColumnClass "column-summary")
            Status = $status
            StatusName = if ($statusNameMatch.Success) { Convert-CellText $statusNameMatch.Groups["status"].Value } else { $status }
            Handler = if ($handlerMatch.Success) { Convert-CellText $handlerMatch.Groups["handler"].Value } else { "" }
            Category = Convert-CellText (Get-CellHtml -RowHtml $row -ColumnClass "column-category")
            Severity = Convert-CellText (Get-CellHtml -RowHtml $row -ColumnClass "column-severity")
            LastModified = Convert-CellText (Get-CellHtml -RowHtml $row -ColumnClass "column-last-modified")
            Url = $uri.AbsoluteUri
        }
    }

    $items | Sort-Object Id -Unique
}

function New-MantisHtmlReport {
    param(
        [object[]]$Items,
        [string]$Path
    )

    $reportFile = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Path)
    $reportDir = Split-Path -Parent $reportFile
    if (-not (Test-Path -LiteralPath $reportDir)) {
        New-Item -ItemType Directory -Path $reportDir | Out-Null
    }

    $generatedAt = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $viewItems = foreach ($item in $Items) {
        $statusInfo = Get-StatusInfo $item.StatusName
        [pscustomobject]@{
            Item = $item
            StatusKey = $statusInfo.Key
            StatusOrder = $statusInfo.Order
            StatusLabelText = $statusInfo.LabelText
            StatusLabelHtml = $statusInfo.LabelHtml
            StatusClass = $statusInfo.Class
            StatusOpen = $statusInfo.Open
        }
    }

    $groups = $viewItems |
        Group-Object StatusKey |
        Sort-Object { ($_.Group | Select-Object -First 1).StatusOrder }

    $summaryCards = foreach ($group in $groups) {
        $first = $group.Group | Select-Object -First 1
@"
      <button class="metric-card metric-$($first.StatusKey)" type="button" onclick="openStatusSection('$($first.StatusKey)')">
        <div class="metric-bar"></div>
        <div class="metric-inner">
          <div class="metric-num">$($group.Count)</div>
          <div class="metric-label">$($first.StatusLabelHtml)</div>
        </div>
      </button>
"@
    }

    $sections = foreach ($group in $groups) {
        $first = $group.Group | Select-Object -First 1
        $headerClass = if ($first.StatusOpen) { "section-header" } else { "section-header collapsed" }
        $bodyClass = if ($first.StatusOpen) { "section-body" } else { "section-body collapsed-body" }
        $iconClass = if ($first.StatusOpen) { "chevron open" } else { "chevron" }
        $rows = foreach ($viewItem in ($group.Group | Sort-Object {[int]$_.Item.Id})) {
            $item = $viewItem.Item
            $id = [System.Net.WebUtility]::HtmlEncode($item.Id)
            $lastModified = [System.Net.WebUtility]::HtmlEncode($item.LastModified)
            $title = [System.Net.WebUtility]::HtmlEncode($item.Title)
            $url = [System.Net.WebUtility]::HtmlEncode($item.Url)
@"
          <tr>
            <td class="col-id"><a href="$url" target="_blank">#$id</a></td>
            <td class="col-title">$title</td>
            <td class="col-date">$lastModified</td>
          </tr>
"@
        }

@"
    <section class="section" data-status="$($first.StatusKey)">
      <button class="$headerClass" type="button" onclick="toggleSection(this)">
        <span class="section-left">
          <span class="$iconClass"></span>
          <span class="pill $($first.StatusClass)">$($first.StatusLabelHtml)</span>
        </span>
        <span class="section-count"><strong>$($group.Count)</strong> &#31558;</span>
      </button>
      <div class="$bodyClass">
        <table>
          <thead>
            <tr>
              <th>&#38917;&#30446;&#32232;&#34399;</th>
              <th>&#38917;&#30446;&#21517;&#31281;</th>
              <th>&#26356;&#26032;&#26178;&#38291;</th>
            </tr>
          </thead>
          <tbody>
$($rows -join "`n")
          </tbody>
        </table>
      </div>
    </section>
"@
    }

    $html = @"
<!doctype html>
<html lang="zh-Hant">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Mantis &#22577;&#21578;&#25972;&#29702;</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #1f1f1d;
      --surface: #2c2d2a;
      --surface-2: #252623;
      --text: #f0f0ea;
      --muted: #c7c7bd;
      --hint: #9c9d95;
      --line: rgba(255,255,255,0.13);
      --line-2: rgba(255,255,255,0.27);
      --link: #9ec5ff;
      --r-md: 8px;
      --r-lg: 10px;
      --bar-progress: #f5a623;
      --bar-assigned: #5ea3f5;
      --bar-review: #b07ef5;
      --bar-done: #8f99ac;
      --bar-testing: #3dbf68;
      --bar-other: #22d3ee;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font-family: "Microsoft JhengHei", "Segoe UI", Arial, sans-serif;
      font-size: 14px;
      line-height: 1.5;
    }
    main {
      width: min(792px, calc(100% - 84px));
      margin: 50px auto 56px;
    }
    .topbar {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 16px;
      min-height: 98px;
      padding: 24px 28px;
      margin-bottom: 18px;
      background: var(--surface);
      border: 1px solid var(--line-2);
      border-radius: 10px;
    }
    h1 {
      margin: 0;
      font-size: 22px;
      font-weight: 700;
      letter-spacing: 0;
    }
    .meta {
      color: var(--muted);
      font-size: 12px;
      text-align: right;
    }
    .meta-line {
      display: flex;
      align-items: center;
      gap: 6px;
      flex-wrap: wrap;
      margin-top: 6px;
      color: var(--hint);
      font-size: 13px;
    }
    .topbar-right {
      display: flex;
      flex-direction: column;
      align-items: flex-end;
      gap: 5px;
    }
    .badge-total {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      padding: 6px 12px;
      border-radius: 999px;
      background: var(--surface-2);
      border: 1px solid var(--line-2);
      color: var(--muted);
      font-size: 13px;
      white-space: nowrap;
    }
    .badge-total strong {
      color: var(--text);
      font-weight: 500;
    }
    .metric-grid {
      display: flex;
      gap: 10px;
      margin: 0 0 18px;
      overflow-x: auto;
      padding-bottom: 2px;
    }
    .metric-card {
      display: flex;
      flex-direction: column;
      flex: 1 0 0;
      min-width: 110px;
      min-height: 82px;
      padding: 0;
      background: var(--surface);
      border: 1px solid var(--line-2);
      border-radius: var(--r-md);
      overflow: hidden;
      color: var(--text);
      cursor: pointer;
      font: inherit;
      text-align: left;
      transition: transform 140ms ease, border-color 140ms ease, background 140ms ease;
    }
    .metric-card:hover {
      transform: translateY(-1px);
      border-color: rgba(255,255,255,0.42);
    }
    .metric-card:focus-visible {
      outline: 2px solid rgba(255,255,255,0.6);
      outline-offset: 2px;
    }
    .metric-bar {
      width: 100%;
      height: 4px;
      flex: 0 0 auto;
    }
    .metric-inner {
      display: grid;
      align-content: center;
      gap: 5px;
      height: 100%;
      padding: 12px 14px 13px;
    }
    .metric-num {
      font-size: 29px;
      font-weight: 600;
      line-height: 1;
      letter-spacing: 0;
    }
    .metric-label {
      color: var(--muted);
      font-size: 12px;
      font-weight: 600;
    }
    .metric-progress .metric-bar { background: var(--bar-progress); }
    .metric-assigned .metric-bar { background: var(--bar-assigned); }
    .metric-review .metric-bar { background: var(--bar-review); }
    .metric-done .metric-bar { background: var(--bar-done); }
    .metric-testing .metric-bar { background: var(--bar-testing); }
    .metric-other .metric-bar { background: var(--bar-other); }
    .sections {
      display: flex;
      flex-direction: column;
      gap: 9px;
    }
    .section {
      background: var(--surface);
      border: 1px solid var(--line);
      border-radius: 9px;
      overflow: hidden;
    }
    .section-header {
      width: 100%;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 14px;
      min-height: 48px;
      padding: 12px 20px;
      color: var(--text);
      background: var(--surface);
      border: 0;
      border-bottom: 1px solid var(--line);
      cursor: pointer;
      user-select: none;
      font: inherit;
      text-align: left;
    }
    .section-header.collapsed {
      border-bottom: 0;
    }
    .section-left {
      display: inline-flex;
      align-items: center;
      gap: 14px;
      min-width: 0;
    }
    .chevron {
      width: 8px;
      height: 8px;
      border-right: 2px solid var(--hint);
      border-bottom: 2px solid var(--hint);
      transform: rotate(45deg);
      transition: transform 160ms ease;
      margin-top: -3px;
    }
    .chevron.open {
      transform: rotate(-135deg);
      margin-top: 3px;
    }
    .section-count {
      color: var(--hint);
      font-size: 14px;
      white-space: nowrap;
    }
    .section-count strong {
      color: var(--muted);
      font-weight: 700;
    }
    .section-body {
      overflow-x: auto;
    }
    .collapsed-body {
      display: none;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      min-height: 24px;
      padding: 3px 8px;
      border-radius: 999px;
      font-size: 13px;
      font-weight: 700;
      border: 1px solid transparent;
      white-space: nowrap;
    }
    .status-progress { background:#fff0d7; color:#8a4c00; border-color:#ffd59b; }
    .status-assigned { background:#dbe9ff; color:#25559c; border-color:#bcd2ff; }
    .status-review { background:#ead7ff; color:#5f33a6; border-color:#d5b7ff; }
    .status-done { background:#e9edf3; color:#53606f; border-color:#d7dde7; }
    .status-testing { background:#d9f4e3; color:#1c6b37; border-color:#bce7ca; }
    .status-wait { background:#fff1f3; color:#b42318; border-color:#ffc9cf; }
    .status-other { background:#e6f9fb; color:#195a63; border-color:#9de6ee; }
    table {
      width: 100%;
      border-collapse: collapse;
    }
    th, td {
      padding: 10px 20px;
      border-bottom: 1px solid var(--line);
      text-align: left;
      vertical-align: middle;
    }
    th {
      color: var(--hint);
      font-size: 12px;
      font-weight: 700;
      background: var(--surface-2);
      white-space: nowrap;
      position: sticky;
      top: 0;
      z-index: 1;
    }
    tr:last-child td { border-bottom: 0; }
    tbody tr:hover td { background: var(--surface-2); }
    a {
      color: var(--link);
      text-decoration: none;
      font-weight: 500;
    }
    a:hover { text-decoration: underline; }
    .col-id {
      white-space: nowrap;
      width: 90px;
    }
    .col-id a {
      font-size: 12px;
    }
    .col-title {
      min-width: 360px;
      font-size: 13px;
    }
    .col-date {
      white-space: nowrap;
      width: 130px;
      color: var(--hint);
      font-size: 13px;
      text-align: right;
    }
    @media (max-width: 900px) {
      main { width: calc(100% - 24px); margin-top: 20px; }
      .topbar { display: block; }
      .topbar-right { align-items: flex-start; margin-top: 10px; }
      .meta { text-align: left; }
      table { min-width: 680px; }
    }
    @media (max-width: 560px) {
      h1 { font-size: 20px; }
      .topbar { padding: 16px; }
    }
  </style>
</head>
<body>
  <main>
    <div class="topbar">
      <div>
        <h1>Mantis &#22577;&#21578;&#25972;&#29702;</h1>
        <div class="meta-line">
          <span>Issue tracker</span>
          <span>&gt;</span>
          <span>&#25105;&#30340;&#24453;&#36774;&#28165;&#21934;</span>
        </div>
      </div>
      <div class="topbar-right">
        <div class="badge-total">&#32317;&#35336; <strong>$($Items.Count)</strong> &#31558;</div>
        <div class="meta">&#29986;&#29983;&#26178;&#38291;: $generatedAt</div>
      </div>
    </div>
    <div class="metric-grid">
$($summaryCards -join "`n")
    </div>
    <div class="sections">
$($sections -join "`n")
    </div>
  </main>
  <script>
    function toggleSection(header) {
      var body = header.nextElementSibling;
      var icon = header.querySelector('.chevron');
      if (body.classList.contains('collapsed-body')) {
        body.classList.remove('collapsed-body');
        header.classList.remove('collapsed');
        icon.classList.add('open');
      } else {
        body.classList.add('collapsed-body');
        header.classList.add('collapsed');
        icon.classList.remove('open');
      }
    }
    function openStatusSection(statusKey) {
      var section = document.querySelector('.section[data-status="' + statusKey + '"]');
      if (!section) return;

      var header = section.querySelector('.section-header');
      var body = section.querySelector('.section-body');
      var icon = section.querySelector('.chevron');
      var isOpen = !body.classList.contains('collapsed-body');

      if (isOpen) {
        body.classList.add('collapsed-body');
        header.classList.add('collapsed');
        icon.classList.remove('open');
      } else {
        body.classList.remove('collapsed-body');
        header.classList.remove('collapsed');
        icon.classList.add('open');
        section.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }
    }
  </script>
</body>
</html>
"@

    Set-Content -LiteralPath $reportFile -Encoding UTF8 -Value $html
    return $reportFile
}

function Invoke-MantisRequest {
    param(
        [string]$Url,
        [pscredential]$Credential
    )

    $session = New-Object Microsoft.PowerShell.Commands.WebRequestSession
    $baseUri = [System.Uri]$Url
    $loginPage = "$($baseUri.Scheme)://$($baseUri.Authority)/mantisbt/login_page.php"
    $loginAction = "$($baseUri.Scheme)://$($baseUri.Authority)/mantisbt/login.php"

    Invoke-WebRequest -Uri $loginPage -WebSession $session -UseBasicParsing | Out-Null
    Invoke-WebRequest `
        -Uri $loginAction `
        -Method Post `
        -WebSession $session `
        -UseBasicParsing `
        -Body @{
            username = $Credential.UserName
            password = $Credential.GetNetworkCredential().Password
            secure_session = "on"
            return = $Url
        } | Out-Null

    return Invoke-WebRequest -Uri $Url -WebSession $session -UseBasicParsing
}

$configFile = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($ConfigPath)
if (-not (Test-Path -LiteralPath $configFile)) {
    throw "Config file not found: $configFile. Copy config.sample.json to config.json first."
}

$basePath = Split-Path -Parent $configFile
$config = Get-Content -LiteralPath $configFile -Raw -Encoding UTF8 | ConvertFrom-Json

$credentialPath = Resolve-FromBase $basePath $config.CredentialPath
$statePath = Resolve-FromBase $basePath $config.StatePath
$logPath = Resolve-FromBase $basePath $config.LogPath

if (-not (Test-Path -LiteralPath $credentialPath)) {
    throw "Mantis credential not found: $credentialPath. Run scripts\Set-MantisCredential.ps1 first."
}

$credential = Import-Clixml -LiteralPath $credentialPath
$response = Invoke-MantisRequest -Url $config.MantisUrl -Credential $credential
$items = @(Get-MantisItemsFromHtml -Html $response.Content -BaseUrl $config.MantisUrl)

if ($items.Count -eq 0) {
    Write-Log -Path $logPath -Message "No Mantis items found. Login may have failed or page markup may be different."
    throw "No Mantis items found. Check credentials, filter URL, or page markup."
}

if ($ListItems) {
    $viewItems = foreach ($item in $items) {
        $statusInfo = Get-StatusInfo $item.StatusName
        [pscustomobject]@{
            Id = $item.Id
            Title = $item.Title
            LastModified = $item.LastModified
            StatusKey = $statusInfo.Key
            StatusOrder = $statusInfo.Order
            StatusLabelText = $statusInfo.LabelText
        }
    }

    $statusGroups = $viewItems |
        Group-Object StatusKey |
        Sort-Object { ($_.Group | Select-Object -First 1).StatusOrder }

    $idHeader = New-Text @(0x9805, 0x76EE, 0x7DE8, 0x865F)
    $titleHeader = New-Text @(0x9805, 0x76EE, 0x540D, 0x7A31)
    $updatedHeader = New-Text @(0x66F4, 0x65B0, 0x6642, 0x9593)
    $countUnit = New-Text @(0x7B46)

    foreach ($group in $statusGroups) {
        $first = $group.Group | Select-Object -First 1
        Write-Host ""
        Write-Host ("[{0}] {1} {2}" -f $first.StatusLabelText, $group.Count, $countUnit)
        Write-Host ("-" * 80)
        $group.Group |
            Sort-Object {[int]$_.Id} |
            Format-Table `
                @{Label=$idHeader; Expression={$_.Id}},
                @{Label=$titleHeader; Expression={$_.Title}},
                @{Label=$updatedHeader; Expression={$_.LastModified}} `
                -AutoSize
    }

    Write-Host "Total: $($items.Count) item(s)."
    return
}

if ($HtmlReport) {
    $reportFile = New-MantisHtmlReport -Items $items -Path $ReportPath
    Write-Host "HTML report created: $reportFile"
    return
}

$stateDir = Split-Path -Parent $statePath
if (-not (Test-Path -LiteralPath $stateDir)) {
    New-Item -ItemType Directory -Path $stateDir | Out-Null
}

$seenIds = @{}
if (Test-Path -LiteralPath $statePath) {
    $saved = Get-Content -LiteralPath $statePath -Raw -Encoding UTF8 | ConvertFrom-Json
    foreach ($id in @($saved.SeenIds)) {
        $seenIds[[string]$id] = $true
    }
}

$newItems = @($items | Where-Object { -not $seenIds.ContainsKey([string]$_.Id) })

$newState = [pscustomobject]@{
    CheckedAt = (Get-Date).ToString("o")
    SeenIds = @($items.Id | Sort-Object -Unique)
}
$newState | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $statePath -Encoding UTF8

if ($InitializeOnly) {
    Write-Log -Path $logPath -Message "Initialized with $($items.Count) items."
    Write-Host "Initialized with $($items.Count) items. No notification sent."
    return
}

if ($newItems.Count -eq 0) {
    Write-Log -Path $logPath -Message "No new items. Current item count: $($items.Count)."
    Write-Host "No new items."
    return
}

$lines = $newItems | Select-Object -First 5 | ForEach-Object { "#$($_.Id) $($_.Title)" }
$message = ($lines -join "`n")
if ($newItems.Count -gt 5) {
    $message += "`n...and $($newItems.Count - 5) more"
}

Write-Log -Path $logPath -Message "Found $($newItems.Count) new items: $($newItems.Id -join ', ')"
Write-Host $message

if ($config.Notify) {
    Show-Balloon -Title "Mantis has $($newItems.Count) new item(s)" -Text $message
}
