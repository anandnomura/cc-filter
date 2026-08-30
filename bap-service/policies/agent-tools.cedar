// BAP Service policy: explicit forbids always override permits.
forbid (
    principal is Agent,
    action,
    resource is ToolInvocation
) when {
    resource.protected ||
    resource.outsideWorkspace ||
    resource.destructive ||
    resource.privileged ||
    resource.exfiltration ||
    resource.obfuscated
};

// Hooks, managed settings, and VCS hooks are policy-controlled surfaces.
forbid (
    principal is Agent,
    action in [Action::"file.write", Action::"notebook.write"],
    resource is ToolInvocation
) when {
    resource.securityControl
};

// These families need a company registry, approval workflow, or explicit
// policy profile before they can be enabled. Keeping explicit forbids produces
// an actionable reason instead of silently broadening a permit.
forbid (
    principal is Agent,
    action in [
        Action::"network.fetch",
        Action::"mcp.invoke",
        Action::"agent.delegate",
        Action::"tool.unknown"
    ],
    resource is ToolInvocation
)
when { true };

// The local Claude agent can perform modeled, non-risky development operations.
// Network search is deliberately distinct from arbitrary URL fetches.
permit (
    principal == Agent::"claude-code-local",
    action in [
        Action::"file.read",
        Action::"file.search",
        Action::"file.write",
        Action::"notebook.write",
        Action::"command.execute",
        Action::"network.search"
    ],
    resource is ToolInvocation
) when {
    principal.enabled &&
    !resource.protected &&
    !resource.outsideWorkspace &&
    !resource.securityControl &&
    !resource.destructive &&
    !resource.privileged &&
    !resource.exfiltration &&
    !resource.obfuscated
};
