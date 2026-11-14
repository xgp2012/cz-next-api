$ErrorActionPreference = "Continue"
$base = "http://127.0.0.1:53190"

function PJ($sess, $url, $obj) {
    $b = [System.Text.Encoding]::UTF8.GetBytes(($obj | ConvertTo-Json -Depth 8))
    try {
        $r = Invoke-WebRequest -Uri $url -Method POST -Body $b -ContentType "application/json; charset=utf-8" -WebSession $sess -UseBasicParsing -TimeoutSec 25
        return [System.Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray())
    }
    catch { return "ERR: $($_.Exception.Message)" }
}

function GJ($sess, $url) {
    $r = Invoke-WebRequest -Uri $url -WebSession $sess -UseBasicParsing -TimeoutSec 20
    return [System.Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray())
}

function IsOk($s) { return ($s -match '"success":true') }

$admin = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$lg = Invoke-WebRequest -Uri "$base/api/user/login" -Method POST -Body (@{username = "root"; password = "123456" } | ConvertTo-Json) -ContentType "application/json" -WebSession $admin -UseBasicParsing -TimeoutSec 20
Write-Host "[1] login                : $($lg.StatusCode)"

$pr = Invoke-WebRequest -Uri "$base/api/pricing" -UseBasicParsing -TimeoutSec 20
Write-Host "[2] public pricing wall  : $($pr.StatusCode)"

$rule = @{
    name = "gpt-4o-rule"; model = "gpt-4o"; group = ""; channel_type = -1
    billing_mode = "per_token"; input_ratio = 1.5; output_ratio = 6.0
    cache_hit_ratio = 0.5; status = 1; priority = 100
}
Write-Host "[3] create pricing rule  : $(if (IsOk (PJ $admin "$base/api/pricing_rules/" $rule)) { 'ok' } else { 'FAIL' })"

$ch = @{name = "e2e"; type = 1; key = "sk-test"; base_url = "http://127.0.0.1:53181"; models = "gpt-4o"; group = "default"; status = 1 }
Write-Host "[4] create channel       : $(if (IsOk (PJ $admin "$base/api/channel/" $ch)) { 'ok' } else { 'FAIL' })"

$aff = (GJ $admin "$base/api/user/aff" | ConvertFrom-Json).data
Write-Host "[5] aff overview object  : aff_code='$($aff.aff_code)' (is object: $($aff -is [PSCustomObject]))"

$reg = PJ $admin "$base/api/user/register" @{username = "carol"; password = "test123456"; email = ""; verification_code = ""; aff_code = $aff.aff_code }
Write-Host "[6] register invitee     : $(if (IsOk $reg) { 'ok' } else { "FAIL $reg" })"

$b2 = [System.Text.Encoding]::UTF8.GetBytes((@{id = 2; username = "carol"; password = ""; display_name = "carol"; role = 1; status = 1; quota = 10000000; group = "default" } | ConvertTo-Json))
Invoke-WebRequest -Uri "$base/api/user/" -Method PUT -Body $b2 -ContentType "application/json" -WebSession $admin -UseBasicParsing -TimeoutSec 20 | Out-Null

$carol = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$lg2 = PJ $carol "$base/api/user/login" @{username = "carol"; password = "test123456" }
Write-Host "[7] invitee login        : $(if (IsOk $lg2) { 'ok' } else { "FAIL $lg2" })"

PJ $carol "$base/api/token/" @{name = "k"; remain_quota = 0; unlimited_quota = $true; expired_time = -1 } | Out-Null
$key = ((GJ $carol "$base/api/token/?p=0" | ConvertFrom-Json).data)[0].key

$q0 = (GJ $carol "$base/api/user/self" | ConvertFrom-Json).data.quota
$p0 = (GJ $admin "$base/api/user/aff" | ConvertFrom-Json).data.pending_quota
Write-Host "[8] invitee quota before : $q0"
Write-Host "[9] inviter pending before: $p0"

$body = '{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"max_tokens":5}'
$enc = [System.Text.Encoding]::UTF8.GetBytes($body)
try {
    $r = Invoke-WebRequest -Uri "$base/v1/chat/completions" -Method POST -Body $enc -ContentType "application/json" -Headers @{Authorization = "Bearer $key" } -UseBasicParsing -TimeoutSec 30
    Write-Host "[10] relay request       : $($r.StatusCode)"
}
catch { Write-Host "[10] relay request       : ERR $($_.Exception.Message)" }

Start-Sleep -Seconds 2
$q1 = (GJ $carol "$base/api/user/self" | ConvertFrom-Json).data.quota
$p1 = (GJ $admin "$base/api/user/aff" | ConvertFrom-Json).data.pending_quota
$consumed = $q0 - $q1
Write-Host "[11] consumed quota      : $consumed  (expected 4500)"
Write-Host "[12] inviter pending     : $p1  (expected 450 = 10% of 4500)"
Write-Host "     PASS consumed : $(if ($consumed -eq 4500) { 'YES' } else { 'NO' })"
Write-Host "     PASS commission: $(if ($p1 -eq 450) { 'YES' } else { 'NO' })"

# 转正
$w0 = (GJ $admin "$base/api/user/self" | ConvertFrom-Json).data.quota
$tr = PJ $admin "$base/api/user/aff_transfer" @{}
$w1 = (GJ $admin "$base/api/user/self" | ConvertFrom-Json).data.quota
$aff2 = (GJ $admin "$base/api/user/aff" | ConvertFrom-Json).data
Write-Host "[13] transfer            : $tr"
Write-Host "[14] wallet delta        : $($w1 - $w0)  (expected $p1)"
Write-Host "[15] pending after       : $($aff2.pending_quota)  history: $($aff2.history_quota)"

# 营收看板
$rev = (GJ $admin "$base/api/admin/revenue?days=7" | ConvertFrom-Json).data
Write-Host "[16] revenue days        : $($rev.days) (daily rows: $($rev.daily.Count))"
Write-Host "[17] revenue note        : $($rev.note.Substring(0, [Math]::Min(40, $rev.note.Length)))..."
