$key = (Get-Content .\.bap\runtime\docker\edge-api-key.txt -Raw).Trim()

$body = @{
    edge_instance_id  = "visual-demo"
    edge_version      = "1"
    installed_version = 0
    installed_digest  = ""
    revocation_epoch  = 0
    nonce             = [guid]::NewGuid().ToString("N")
} | ConvertTo-Json -Compress

# Preserve JSON quotes when PowerShell passes it to curl.exe
$curlBody = $body.Replace('"', '\"')

$raw = curl.exe --silent --show-error --fail-with-body `
    --ssl-no-revoke `
    --cacert .\.bap\runtime\docker\dev-ca.pem `
    -H "Authorization: Bearer $key" `
    -H "Content-Type: application/json" `
    --data-binary $curlBody `
    https://127.0.0.1:8443/bap/v1/edge/sync

$response = $raw | ConvertFrom-Json

$response.directive
$response.envelope.payload |
    Select-Object version,rules_digest,issued_at,expires_at,
        refresh_after_seconds,max_offline_seconds,
        force_update,kill_switch |
    Format-List