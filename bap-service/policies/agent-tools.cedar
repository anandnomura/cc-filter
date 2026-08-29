// BAP Service policy: explicit forbids always override permits.
forbid (
    principal is Agent,
    action,
    resource is ToolInvocation
) when {
    resource.protected || resource.outsideWorkspace || resource.destructive
};

// The local Claude agent can perform modeled, non-risky development operations.
permit (
    principal == Agent::"claude-code-local",
    action in [
        Action::"file.read",
        Action::"file.search",
        Action::"file.write",
        Action::"command.execute",
        Action::"network.access",
        Action::"mcp.invoke"
    ],
    resource is ToolInvocation
) when {
    principal.enabled &&
    !resource.protected &&
    !resource.outsideWorkspace &&
    !resource.destructive
};
