# BAP Edge component

BAP Edge is the Claude hook policy-enforcement component of
[BAP System](../docs/bap-system/README.md). Its executable entry point is
`cmd`, and its Edge runtime is protected under `internal/bapedge`. The small
`pkg/bapedge` facade exists only for cross-component rollout integration tests.

Build and test commands remain at the repository root:

```powershell
.\Build-BapEdge.ps1 -Runtime Native
.\Test-MVP0.ps1 -Runtime Native
```

The inherited cc-filter source remains at repository root for upstream fork
synchronization. See the [operator guide](../docs/bap-edge/README.md).
