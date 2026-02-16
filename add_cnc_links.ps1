$baseUrl = 'http://localhost:8080/api/v1'

$links = @(
    @('e6abcaa0-f5ba-4e35-9283-2a1fe109d996', '59357995-27c9-41b2-857f-207b041771df', 'verifies', 'E-stop response test verifies emergency stop requirement'),
    @('4453327f-d230-43b8-9950-82aa5cad8be0', '0bd06a48-28c9-41d0-ae79-cb2846580f24', 'verifies', 'Frame stiffness test verifies rigid frame requirement'),
    @('2236644e-ca73-4655-85d9-828b5df639fa', 'c934129f-542c-4a48-88d4-cb6aaa9929a2', 'verifies', 'Job resume test verifies job resume requirement'),
    @('1f612b89-56bb-44d6-a650-7cacfdcd2cb7', '5e140ae9-f6bd-4ddc-b9c2-b1a528914546', 'verifies', 'Spindle runout test verifies runout requirement'),
    @('95cd67ac-d544-4727-93f8-a64b769ea65c', '0e291027-e1b2-4823-8cfd-d24fce8016a7', 'verifies', 'Thermal shutdown test verifies thermal monitoring requirement'),
    @('6554706e-402e-4c29-992c-319304d13c5d', '384c4dcb-47eb-4c72-9e28-3c7b73b2aec0', 'verifies', 'Travel measurement test verifies travel limits requirement'),
    @('85168909-ef0a-483a-a0e2-e70b70916bb9', '7efc3254-5fd8-4443-957b-ed294b5f206a', 'satisfies', 'Spindle assembly design satisfies spindle control requirement'),
    @('85168909-ef0a-483a-a0e2-e70b70916bb9', 'edd19191-31c4-4f57-8f44-4d3e3277125e', 'satisfies', 'Spindle assembly design satisfies spindle speed requirement'),
    @('85168909-ef0a-483a-a0e2-e70b70916bb9', '5e140ae9-f6bd-4ddc-b9c2-b1a528914546', 'satisfies', 'Spindle assembly design satisfies runout requirement'),
    @('97ca36a0-4ebe-4c23-9daf-362605b88a21', '495760a4-4d50-43aa-9255-b3dd6f083d33', 'relates-to', 'Flying debris hazard relates to guarding requirement'),
    @('97ca36a0-4ebe-4c23-9daf-362605b88a21', '2ee572bc-2ead-4ee9-9727-3014ca40ea5d', 'relates-to', 'Flying debris hazard relates to door interlock requirement'),
    @('97ca36a0-4ebe-4c23-9daf-362605b88a21', '59357995-27c9-41b2-857f-207b041771df', 'mitigates', 'Emergency stop can mitigate flying debris exposure'),
    @('255538fe-d294-4be5-a40d-d711b56ef218', '97ca36a0-4ebe-4c23-9daf-362605b88a21', 'mitigates', 'Dust management mitigates flying debris exposure')
)

Write-Host "Creating $($links.Count) links for CNC project..."
$created = 0
$failed = 0

foreach ($link in $links) {
    $fromId = $link[0]
    $toId = $link[1]
    $type = $link[2]
    $desc = $link[3]
    
    $payload = @{
        from_id = $fromId
        to_id = $toId
        type = $type
        attributes = @{}
    } | ConvertTo-Json
    
    try {
        $response = Invoke-WebRequest -Uri "$baseUrl/links" -Method POST -Body $payload -ContentType "application/json" -UseBasicParsing
        Write-Host "[+] $desc"
        $created++
    }
    catch {
        Write-Host "[-] $desc - $($_.Exception.Message)"
        $failed++
    }
}

Write-Host ""
Write-Host "Results: $created created, $failed failed"
