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
    $otherText = New-Text @(0x5176, 0x4ED6)

    if ($status -match "feedback" -or $name.Contains($progressText)) {
        return [pscustomobject]@{ Key = "progress"; Order = 1; LabelText = $progressText; LabelHtml = "&#36914;&#34892;&#20013;"; Class = "status-progress"; Open = $true }
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

    return [pscustomobject]@{ Key = "other"; Order = 99; LabelText = $otherText; LabelHtml = "&#20854;&#20182;"; Class = "status-other"; Open = $false }
}

function Get-StatusDefinitions {
    $progressText = New-Text @(0x9032, 0x884C, 0x4E2D)
    $assignedText = New-Text @(0x5DF2, 0x5206, 0x914D)
    $reviewText = (New-Text @(0x5F85)) + "CR"
    $doneText = New-Text @(0x5DF2, 0x5B8C, 0x6210)
    $testText = New-Text @(0x6E2C, 0x8A66, 0x4E2D)
    $otherText = New-Text @(0x5176, 0x4ED6)

    return @(
        [pscustomobject]@{ Key = "progress"; Order = 1; LabelText = $progressText; LabelHtml = "&#36914;&#34892;&#20013;"; Class = "status-progress"; Open = $true },
        [pscustomobject]@{ Key = "assigned"; Order = 2; LabelText = $assignedText; LabelHtml = "&#24050;&#20998;&#37197;"; Class = "status-assigned"; Open = $false },
        [pscustomobject]@{ Key = "review"; Order = 3; LabelText = $reviewText; LabelHtml = "&#24453; CR"; Class = "status-review"; Open = $false },
        [pscustomobject]@{ Key = "done"; Order = 4; LabelText = $doneText; LabelHtml = "&#24050;&#23436;&#25104;"; Class = "status-done"; Open = $false },
        [pscustomobject]@{ Key = "testing"; Order = 5; LabelText = $testText; LabelHtml = "&#28204;&#35430;&#20013;"; Class = "status-testing"; Open = $false },
        [pscustomobject]@{ Key = "other"; Order = 99; LabelText = $otherText; LabelHtml = "&#20854;&#20182;"; Class = "status-other"; Open = $false }
    )
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
            TargetVersion = ""
            DetailHtml = ""
            Url = $uri.AbsoluteUri
        }
    }

    $items | Sort-Object Id -Unique
}

function Convert-PlainTextToHtml {
    param([string]$Text)

    $encoded = [System.Net.WebUtility]::HtmlEncode($Text.Trim())
    return ($encoded -replace "(`r`n|`n|`r)", "<br>")
}

function Get-VersionNumber {
    param([string]$Version)

    if ([string]::IsNullOrWhiteSpace($Version)) {
        return -1
    }

    $match = [regex]::Match($Version, '\d+(?:\.\d+)?')
    if (-not $match.Success) {
        return -1
    }

    return [double]::Parse($match.Value, [System.Globalization.CultureInfo]::InvariantCulture)
}

function Convert-DetailBlockText {
    param([string]$Html)

    $text = $Html -replace '(?i)<br\s*/?>', "`n"
    $text = $text -replace '(?i)</p\s*>', "`n"
    $text = $text -replace '<[^>]+>', ''
    $text = [System.Net.WebUtility]::HtmlDecode($text)
    $text = $text -replace "`r`n", "`n"
    $text = $text -replace "`r", "`n"
    $text = $text -replace "[ `t]+`n", "`n"
    $text = $text -replace "`n{3,}", "`n`n"
    return $text.Trim()
}

function Convert-MantisDescriptionToHtml {
    param([string]$DescriptionHtml)

    if ([string]::IsNullOrWhiteSpace($DescriptionHtml)) {
        return '<div class="detail-empty">No detail content.</div>'
    }

    $sections = New-Object System.Collections.Generic.List[string]
    $liMatches = [regex]::Matches(
        $DescriptionHtml,
        '<li[^>]*>(?<value>.*?)</li>',
        [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
    )

    foreach ($liMatch in $liMatches) {
        $block = $liMatch.Groups["value"].Value
        $headingMatch = [regex]::Match(
            $block,
            '<h[1-6][^>]*>(?<value>.*?)</h[1-6]>',
            [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
        )
        $contentMatch = [regex]::Match(
            $block,
            '<(?:pre|code)[^>]*>(?<value>.*?)</(?:pre|code)>',
            [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
        )

        $heading = if ($headingMatch.Success) { Convert-CellText $headingMatch.Groups["value"].Value } else { "" }
        $content = if ($contentMatch.Success) { Convert-DetailBlockText $contentMatch.Groups["value"].Value } else { Convert-CellText $block }

        if ([string]::IsNullOrWhiteSpace($heading) -and [string]::IsNullOrWhiteSpace($content)) {
            continue
        }

        $headingHtml = [System.Net.WebUtility]::HtmlEncode($heading)
        $contentHtml = Convert-PlainTextToHtml $content
        $sections.Add(@"
              <section class="detail-section">
                <h4>$headingHtml</h4>
                <div class="detail-text">$contentHtml</div>
              </section>
"@)
    }

    if ($sections.Count -eq 0) {
        $fallback = Convert-PlainTextToHtml (Convert-CellText $DescriptionHtml)
        return @"
              <section class="detail-section">
                <div class="detail-text">$fallback</div>
              </section>
"@
    }

    return ($sections -join "`n")
}

function Get-MantisDetailFieldsFromHtml {
    param([string]$Html)

    $targetVersionMatch = [regex]::Match(
        $Html,
        '<td[^>]*class=["''][^"'']*\bbug-target-version\b[^"'']*["''][^>]*>(?<value>.*?)</td>',
        [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
    )

    $targetVersion = if ($targetVersionMatch.Success) {
        Convert-CellText $targetVersionMatch.Groups["value"].Value
    } else {
        ""
    }

    $descriptionMatch = [regex]::Match(
        $Html,
        '<td[^>]*class=["''][^"'']*\bbug-description\b[^"'']*["''][^>]*>(?<value>.*?)</td>',
        [System.Text.RegularExpressions.RegexOptions]::IgnoreCase -bor [System.Text.RegularExpressions.RegexOptions]::Singleline
    )
    $detailHtml = if ($descriptionMatch.Success) {
        Convert-MantisDescriptionToHtml $descriptionMatch.Groups["value"].Value
    } else {
        '<div class="detail-empty">No detail content.</div>'
    }

    return [pscustomobject]@{
        TargetVersion = $targetVersion
        DetailHtml = $detailHtml
    }
}

function Add-MantisDetailFields {
    param(
        [object[]]$Items,
        [Microsoft.PowerShell.Commands.WebRequestSession]$Session
    )

    foreach ($item in $Items) {
        try {
            $detailResponse = Invoke-WebRequest -Uri $item.Url -WebSession $Session -UseBasicParsing
            $detail = Get-MantisDetailFieldsFromHtml -Html $detailResponse.Content
            $item.TargetVersion = $detail.TargetVersion
            $item.DetailHtml = $detail.DetailHtml
        } catch {
            $item.TargetVersion = ""
            $item.DetailHtml = '<div class="detail-empty">Failed to load detail content.</div>'
        }
    }

    return $Items
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

    $itemsByStatus = @{}
    foreach ($viewItem in $viewItems) {
        if (-not $itemsByStatus.ContainsKey($viewItem.StatusKey)) {
            $itemsByStatus[$viewItem.StatusKey] = @()
        }
        $itemsByStatus[$viewItem.StatusKey] += $viewItem
    }

    $groups = foreach ($definition in Get-StatusDefinitions) {
        $rawGroupItems = if ($itemsByStatus.ContainsKey($definition.Key)) {
            @($itemsByStatus[$definition.Key])
        } else {
            @()
        }
        $groupItems = @($rawGroupItems)

        [pscustomobject]@{
            Key = $definition.Key
            Order = $definition.Order
            LabelText = $definition.LabelText
            LabelHtml = $definition.LabelHtml
            Class = $definition.Class
            Open = $definition.Open
            Items = $groupItems
            ItemCount = @($groupItems).Count
        }
    }

    $summaryCards = foreach ($group in $groups) {
@"
      <button class="metric-card metric-$($group.Key)" type="button" onclick="openStatusSection('$($group.Key)')">
        <div class="metric-bar"></div>
        <div class="metric-inner">
          <div class="metric-num">$($group.ItemCount)</div>
          <div class="metric-label">$($group.LabelHtml)</div>
        </div>
      </button>
"@
    }

    $sections = foreach ($group in $groups) {
        $headerClass = if ($group.Open) { "section-header" } else { "section-header collapsed" }
        $bodyClass = if ($group.Open) { "section-body" } else { "section-body collapsed-body" }
        $iconClass = if ($group.Open) { "chevron open" } else { "chevron" }
        $rows = foreach ($viewItem in (@($group.Items) | Sort-Object @{Expression={$_.Item.LastModified}; Descending=$true}, @{Expression={[int]$_.Item.Id}; Descending=$true})) {
            $item = $viewItem.Item
            $id = [System.Net.WebUtility]::HtmlEncode($item.Id)
            $lastModified = [System.Net.WebUtility]::HtmlEncode($item.LastModified)
            $targetVersion = [System.Net.WebUtility]::HtmlEncode($item.TargetVersion)
            $targetVersionNumber = Get-VersionNumber $item.TargetVersion
            $title = [System.Net.WebUtility]::HtmlEncode($item.Title)
            $url = [System.Net.WebUtility]::HtmlEncode($item.Url)
            $detailId = "detail-$id"
            $detailHtml = if ([string]::IsNullOrWhiteSpace($item.DetailHtml)) {
                '<div class="detail-empty">No detail content.</div>'
            } else {
                $item.DetailHtml
            }
@"
          <tr class="issue-row" data-id="$id" data-updated="$lastModified" data-target-version="$targetVersionNumber">
            <td class="col-id"><a href="$url" target="_blank">#$id</a></td>
            <td class="col-title"><button class="issue-title-button" type="button" onclick="toggleIssueDetail('$detailId')">$title</button></td>
            <td class="col-target">$targetVersion</td>
            <td class="col-date">$lastModified</td>
          </tr>
          <tr id="$detailId" class="issue-detail-row hidden-detail">
            <td colspan="4">
              <div class="issue-detail-panel">
$detailHtml
              </div>
            </td>
          </tr>
"@
        }

        if ($group.ItemCount -eq 0) {
            $rows = @"
          <tr>
            <td colspan="4" class="empty-row">No items.</td>
          </tr>
"@
        }

@"
    <section class="section" data-status="$($group.Key)">
      <button class="$headerClass" type="button" onclick="toggleSection(this)">
        <span class="section-left">
          <span class="$iconClass"></span>
          <span class="pill $($group.Class)">$($group.LabelHtml)</span>
        </span>
        <span class="section-count"><strong>$($group.ItemCount)</strong> &#31558;</span>
      </button>
      <div class="$bodyClass">
        <table>
          <thead>
            <tr>
              <th>&#38917;&#30446;&#32232;&#34399;</th>
              <th>&#38917;&#30446;&#21517;&#31281;</th>
              <th>&#30446;&#27161;&#29256;&#26412;</th>
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
      --bar-done: #3dbf68;
      --bar-testing: #ef4444;
      --bar-other: #8f99ac;
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
      position: relative;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 24px;
      padding: 20px 24px 20px 22px;
      margin-bottom: 18px;
      background: var(--surface);
      border: 1px solid rgba(255,255,255,0.10);
      border-radius: 12px;
      overflow: hidden;
    }
    .topbar-accent {
      position: absolute;
      inset: 0 auto 0 0;
      width: 3px;
      border-radius: 12px 0 0 12px;
      background: linear-gradient(180deg, var(--bar-progress) 0%, var(--bar-assigned) 50%, var(--bar-review) 100%);
    }
    .topbar-left {
      display: flex;
      align-items: center;
      gap: 14px;
      position: relative;
      z-index: 1;
    }
    .topbar-icon {
      width: 42px;
      height: 42px;
      border-radius: 10px;
      background: rgba(245,166,35,0.12);
      border: 1px solid rgba(245,166,35,0.25);
      display: flex;
      align-items: center;
      justify-content: center;
      flex-shrink: 0;
    }
    .topbar-icon svg {
      width: 20px;
      height: 20px;
      stroke: #f5a623;
      fill: none;
      stroke-width: 1.8;
      stroke-linecap: round;
      stroke-linejoin: round;
    }
    .topbar-title-group {
      display: flex;
      flex-direction: column;
      gap: 3px;
    }
    h1 {
      margin: 0;
      font-size: 17px;
      font-weight: 600;
      color: var(--text);
      letter-spacing: 0;
      line-height: 1.2;
    }
    .topbar-sub {
      display: flex;
      align-items: center;
      gap: 5px;
      font-size: 12px;
      color: var(--hint);
    }
    .topbar-sub .sep { opacity: 0.5; }
    .topbar-right {
      display: flex;
      align-items: center;
      gap: 20px;
      position: relative;
      z-index: 1;
    }
    .topbar-divider {
      width: 1px;
      height: 32px;
      background: var(--line);
    }
    .stat-item {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 2px;
    }
    .stat-num {
      font-size: 20px;
      font-weight: 700;
      color: var(--text);
      line-height: 1;
    }
    .stat-label {
      font-size: 11px;
      color: var(--hint);
      white-space: nowrap;
    }
    .dot-stack {
      display: flex;
      gap: 4px;
      align-items: center;
    }
    .dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
    }
    .topbar-controls {
      display: flex;
      flex-direction: column;
      align-items: flex-end;
      gap: 5px;
    }
    .sort-control {
      display: inline-flex;
      align-items: center;
      gap: 7px;
      color: var(--hint);
      font-size: 12px;
      white-space: nowrap;
    }
    .sort-control select {
      padding: 4px 10px;
      color: var(--text);
      background: var(--surface-2);
      border: 1px solid rgba(255,255,255,0.18);
      border-radius: 6px;
      font: inherit;
      font-size: 12px;
      cursor: pointer;
    }
    .sort-control select:focus-visible {
      outline: 2px solid rgba(255,255,255,0.55);
      outline-offset: 2px;
    }
    .meta {
      color: var(--hint);
      font-size: 11px;
      text-align: right;
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
      background: #343530;
      border-color: rgba(255,255,255,0.48);
      box-shadow: 0 8px 18px rgba(0,0,0,0.22);
    }
    .metric-card:hover .metric-label {
      color: var(--text);
    }
    .metric-card:active {
      transform: translateY(0);
      box-shadow: none;
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
    .section-header:hover {
      background: #343530;
    }
    .section-header:hover .chevron {
      border-color: var(--text);
    }
    .section-header:focus-visible {
      outline: 2px solid rgba(255,255,255,0.55);
      outline-offset: -2px;
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
      transform: rotate(-45deg);
      transition: transform 160ms ease;
      margin-top: 0;
    }
    .chevron.open {
      transform: rotate(45deg);
      margin-top: -3px;
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
    .status-done { background:#d9f4e3; color:#1c6b37; border-color:#bce7ca; }
    .status-testing { background:#fee2e2; color:#991b1b; border-color:#fecaca; }
    .status-wait { background:#fff1f3; color:#b42318; border-color:#ffc9cf; }
    .status-other { background:#e9edf3; color:#53606f; border-color:#d7dde7; }
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
    .empty-row {
      color: var(--hint);
      text-align: center;
      padding: 18px 20px;
    }
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
    .issue-title-button {
      width: 100%;
      padding: 0;
      color: var(--text);
      background: transparent;
      border: 0;
      font: inherit;
      text-align: left;
      cursor: pointer;
    }
    .issue-title-button:hover {
      color: var(--link);
      text-decoration: underline;
    }
    .issue-title-button:focus-visible {
      outline: 2px solid rgba(158,197,255,0.7);
      outline-offset: 3px;
      border-radius: 4px;
    }
    .hidden-detail {
      display: none;
    }
    .issue-detail-row td {
      padding: 16px 20px 16px 110px;
      background: rgba(255,255,255,0.025);
    }
    .issue-detail-panel {
      display: grid;
      gap: 12px;
      padding: 14px 16px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--surface-2);
    }
    .detail-section {
      display: grid;
      gap: 5px;
    }
    .detail-section h4 {
      margin: 0;
      color: var(--muted);
      font-size: 13px;
      font-weight: 700;
    }
    .detail-text {
      color: var(--text);
      font-size: 13px;
      line-height: 1.65;
      white-space: normal;
    }
    .detail-empty {
      color: var(--hint);
      font-size: 13px;
    }
    .col-date {
      white-space: nowrap;
      width: 130px;
      color: var(--hint);
      font-size: 13px;
      text-align: left;
    }
    .col-target {
      white-space: nowrap;
      width: 110px;
      color: var(--muted);
      font-size: 13px;
      text-align: left;
    }
    @media (max-width: 900px) {
      main { width: calc(100% - 24px); margin-top: 20px; }
      .topbar { flex-wrap: wrap; }
      .topbar-right { flex-wrap: wrap; gap: 12px; }
      .meta { text-align: left; }
      table { min-width: 790px; }
    }
    @media (max-width: 560px) {
      .topbar { padding: 16px; }
      h1 { font-size: 15px; }
    }
  </style>
</head>
<body>
  <main>
    <div class="topbar">
      <div class="topbar-accent"></div>

      <div class="topbar-left">
        <div class="topbar-icon">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2"/>
            <rect x="9" y="3" width="6" height="4" rx="1"/>
            <path d="M9 12h6M9 16h4"/>
          </svg>
        </div>
        <div class="topbar-title-group">
          <h1>Mantis &#22577;&#21578;&#25972;&#29702;</h1>
          <div class="topbar-sub">
            <span>Issue tracker</span>
            <span class="sep">&#8250;</span>
            <span>&#25105;&#30340;&#24453;&#36774;&#28165;&#21934;</span>
          </div>
        </div>
      </div>

      <div class="topbar-right">
        <div class="stat-item">
          <span class="stat-num">$($Items.Count)</span>
          <span class="stat-label">&#32317;&#35336;&#31558;&#25976;</span>
        </div>
        <div class="topbar-divider"></div>
        <div class="dot-stack" aria-hidden="true">
          <div class="dot" style="background:var(--bar-progress);"></div>
          <div class="dot" style="background:var(--bar-assigned);"></div>
          <div class="dot" style="background:var(--bar-review);"></div>
          <div class="dot" style="background:var(--bar-done);"></div>
          <div class="dot" style="background:var(--bar-testing);"></div>
          <div class="dot" style="background:var(--bar-other);"></div>
        </div>
        <div class="topbar-divider"></div>
        <div class="topbar-controls">
          <label class="sort-control">
            <span>&#25490;&#24207;</span>
            <select id="sortMode" onchange="sortIssueRows(this.value)">
              <option value="updated" selected>&#26356;&#26032;&#26178;&#38291;</option>
              <option value="id">&#38917;&#30446;&#32232;&#34399;</option>
              <option value="target">&#30446;&#27161;&#29256;&#26412;</option>
            </select>
          </label>
          <div class="meta">&#29986;&#29983;&#26178;&#38291;: $generatedAt</div>
        </div>
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
      var section = header.closest('.section');
      var body = header.nextElementSibling;
      var icon = header.querySelector('.chevron');
      if (!section) return;
      if (body.classList.contains('collapsed-body')) {
        collapseIssueDetails(section);
        body.classList.remove('collapsed-body');
        header.classList.remove('collapsed');
        icon.classList.add('open');
      } else {
        body.classList.add('collapsed-body');
        header.classList.add('collapsed');
        icon.classList.remove('open');
        collapseIssueDetails(section);
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
        collapseIssueDetails(section);
      } else {
        body.classList.remove('collapsed-body');
        header.classList.remove('collapsed');
        icon.classList.add('open');
        section.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }
    }
    function toggleIssueDetail(detailId) {
      var row = document.getElementById(detailId);
      if (!row) return;
      row.classList.toggle('hidden-detail');
    }
    function collapseIssueDetails(section) {
      section.querySelectorAll('.issue-detail-row').forEach(function(row) {
        row.classList.add('hidden-detail');
      });
    }
    function sortIssueRows(mode) {
      document.querySelectorAll('.section-body tbody').forEach(function(tbody) {
        var pairs = [];
        var rows = Array.prototype.slice.call(tbody.querySelectorAll('tr'));

        rows.forEach(function(row) {
          if (!row.classList.contains('issue-row')) return;
          pairs.push({
            issue: row,
            detail: row.nextElementSibling && row.nextElementSibling.classList.contains('issue-detail-row') ? row.nextElementSibling : null
          });
        });

        pairs.sort(function(a, b) {
          if (mode === 'id') {
            return Number(b.issue.dataset.id || 0) - Number(a.issue.dataset.id || 0);
          }
          if (mode === 'target') {
            var targetDiff = Number(b.issue.dataset.targetVersion || -1) - Number(a.issue.dataset.targetVersion || -1);
            if (targetDiff !== 0) return targetDiff;
            return Number(b.issue.dataset.id || 0) - Number(a.issue.dataset.id || 0);
          }

          var updatedDiff = String(b.issue.dataset.updated || '').localeCompare(String(a.issue.dataset.updated || ''));
          if (updatedDiff !== 0) return updatedDiff;
          return Number(b.issue.dataset.id || 0) - Number(a.issue.dataset.id || 0);
        });

        pairs.forEach(function(pair) {
          tbody.appendChild(pair.issue);
          if (pair.detail) tbody.appendChild(pair.detail);
        });
      });
    }
    sortIssueRows('updated');
  </script>
</body>
</html>
"@

    Set-Content -LiteralPath $reportFile -Encoding UTF8 -Value $html
    return $reportFile
}

function New-MantisWebSession {
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

    return $session
}

function Invoke-MantisRequest {
    param(
        [string]$Url,
        [pscredential]$Credential
    )

    $session = New-MantisWebSession -Url $Url -Credential $Credential
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
$session = New-MantisWebSession -Url $config.MantisUrl -Credential $credential
$response = Invoke-WebRequest -Uri $config.MantisUrl -WebSession $session -UseBasicParsing
$items = @(Get-MantisItemsFromHtml -Html $response.Content -BaseUrl $config.MantisUrl)

if ($items.Count -eq 0) {
    Write-Log -Path $logPath -Message "No Mantis items found. Login may have failed or page markup may be different."
    throw "No Mantis items found. Check credentials, filter URL, or page markup."
}

if ($ListItems -or $HtmlReport) {
    $items = @(Add-MantisDetailFields -Items $items -Session $session)
}

if ($ListItems) {
    $viewItems = foreach ($item in $items) {
        $statusInfo = Get-StatusInfo $item.StatusName
        [pscustomobject]@{
            Id = $item.Id
            Title = $item.Title
            LastModified = $item.LastModified
            TargetVersion = $item.TargetVersion
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
    $targetVersionHeader = New-Text @(0x76EE, 0x6A19, 0x7248, 0x672C)
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
                @{Label=$updatedHeader; Expression={$_.LastModified}},
                @{Label=$targetVersionHeader; Expression={$_.TargetVersion}} `
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
