# Native Windows runbook

Use this on a company Windows laptop when Docker and Podman are unavailable.
It builds Windows executables with installed Go and the Spring gateway JAR with
Java/Maven. It does not create OCI images.

## Prerequisites

- ordinary PowerShell for build/test;
- elevated PowerShell only for managed Claude settings installation/removal;
- Go 1.23.12 or newer, Java 21, Maven 3.9 or newer, Git and `curl.exe`;
- company Claude launcher only for the final interactive test.

Verify:

```powershell
go version
java -version
mvn -version
curl.exe --version
```

## Build

```powershell
cd C:\path\to\bap-system
.\Build-Bap.ps1 -Runtime Native
.\Build-ResourcePEPs.ps1 -Runtime Native
```

Expected artifacts:

```text
dist\bap-edge-windows-amd64.exe
dist\bap-service-windows-amd64.exe
dist\bap-mcp-pep-windows-amd64.exe
dist\bap-mock-resources-windows-amd64.exe
dist\bap-api-gateway-springcloud.jar
```

Optional Linux cross-builds from Windows:

```powershell
.\Build-BapEdge.ps1 -Runtime Native -Targets All
.\Build-BapService-Native.ps1 -Target All -Architecture All
.\Build-ResourcePEPs.ps1 -Runtime Native -Target All -Architecture amd64
```

Linux binaries have no `.exe` suffix and must be run on Linux. The Java JAR is
platform-independent when a matching Java 21 runtime is installed.

## Automated acceptance

```powershell
.\Test-AgentGrant.ps1 -Runtime Native
.\Test-SessionCapabilities.ps1 -Runtime Native
.\Test-ResourcePEPs.ps1 -Runtime Native
.\Demo-ResourcePEPs.ps1 -Runtime Native
.\Test-MVP0.ps1 -Runtime Native
```

The demo starts temporary Service, Edge, Spring PEP, MCP PEP and mock-resource
processes and stops them in `finally`. Evidence is retained beneath
`.bap\resource-pep-demo\<run-id>\`.

If a previous interrupted test owns a port, identify the exact process before
stopping it:

```powershell
Get-NetTCPConnection -State Listen |
  Where-Object LocalPort -In 18443,19443,18765,19090 |
  Select-Object LocalPort,OwningProcess
Get-CimInstance Win32_Process -Filter 'ProcessId=<PID>' |
  Select-Object ProcessId,ExecutablePath,CommandLine
```

Stop it only when `ExecutablePath` points to this checkout's BAP test artifact.

## Interactive local Claude test

When managed hooks are not installed:

```powershell
.\Start-BapNativeLocal.bat -Rebuild
```

When company managed hooks are installed, use the company-managed launcher and
managed Service configuration. Do not substitute a bare `claude` command. To
temporarily return a test laptop to unmanaged mode, run as Administrator:

```powershell
.\Install-ManagedSettings.ps1 -Undo
```

Close all Claude sessions before restarting. Restore enforcement by running
the normal `Install-ManagedSettings.ps1` procedure again.

## Interactive protected MCP test

Deploy/start the MCP PEP against a non-production upstream, register it as
`bap_mcp_pep` in managed MCP configuration, and follow the exact prompt and
evidence checks in the
[protected-resource acceptance guide](../bap-system/protected-resource-acceptance.md#human-mcp-test-with-claude-code).

## Shutdown and recovery

The one-command demo is self-cleaning. For PEPs started independently:

```powershell
.\Stop-ResourcePEPs.ps1
```

Native development state can be preserved for evidence or archived from
`.bap\native-local\runs\` and `.bap\resource-pep-demo\`. Do not reuse a failed
or corrupt audit chain; start a new isolated development run.

## Native promotion gate

Do not promote the native development launcher to a production service. For
production, provision external certificates, durable MySQL, managed identities,
service supervision, central audit and network controls using the
[production runbook](production.md).
