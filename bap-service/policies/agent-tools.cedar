// MVP-0 policy: every tool is classified; explicit forbids override permits.
forbid (principal is Agent, action, resource is ToolInvocation) when {
    resource.protected || resource.outsideWorkspace || resource.destructive ||
    resource.privileged || resource.exfiltration || resource.obfuscated
};

forbid (
    principal is Agent,
    action in [Action::"file.write", Action::"notebook.write"],
    resource is ToolInvocation
) when { resource.securityControl };

// Registry-gated capabilities remain denied until an administrator approves
// their exact domain, MCP tool name, or subagent type in protected Edge config.
forbid (principal is Agent, action == Action::"network.fetch", resource is ToolInvocation)
when { !resource.approvedNetwork };
forbid (principal is Agent, action == Action::"mcp.invoke", resource is ToolInvocation)
when { !resource.approvedMCP };
forbid (principal is Agent, action == Action::"agent.delegate", resource is ToolInvocation)
when { !resource.approvedDelegate };
forbid (principal is Agent, action == Action::"command.execute", resource is ToolInvocation)
when { !resource.shellApproved };

// Unknown and higher-impact current Claude tools require a future policy slice.
forbid (
    principal is Agent,
    action in [
        Action::"tool.unknown", Action::"mcp.resource.list", Action::"mcp.resource.read",
        Action::"mcp.server.wait", Action::"worktree.manage", Action::"task.manage",
        Action::"task.control", Action::"schedule.manage", Action::"artifact.publish",
        Action::"workflow.execute", Action::"agent.message", Action::"agent.control",
        Action::"remote.trigger", Action::"notification.send", Action::"file.send",
        Action::"findings.report", Action::"feedback.prepare"
    ],
    resource is ToolInvocation
) when { true };

// Read-only is suitable for audit/review sessions and forbids mutation even if
// another permit is added later.
forbid (
    principal is Agent,
    action in [
        Action::"file.write", Action::"notebook.write", Action::"command.execute",
        Action::"network.fetch", Action::"network.search", Action::"mcp.invoke",
        Action::"agent.delegate", Action::"skill.invoke"
    ],
    resource is ToolInvocation
) when { resource.policyProfile == "read-only" };

permit (
    principal == Agent::"claude-code-local",
    action in [Action::"file.read", Action::"file.search", Action::"user.interact", Action::"session.plan", Action::"code.intelligence", Action::"tool.discover", Action::"task.read", Action::"agent.observe", Action::"schedule.read"],
    resource is ToolInvocation
) when { principal.enabled };

permit (
    principal == Agent::"claude-code-local",
    action in [Action::"file.write", Action::"notebook.write", Action::"command.execute", Action::"network.search", Action::"skill.invoke"],
    resource is ToolInvocation
) when {
    principal.enabled && resource.policyProfile == "standard-developer" &&
    !resource.protected && !resource.outsideWorkspace && !resource.securityControl &&
    !resource.destructive && !resource.privileged && !resource.exfiltration && !resource.obfuscated
};

permit (principal == Agent::"claude-code-local", action == Action::"network.fetch", resource is ToolInvocation)
when { principal.enabled && resource.policyProfile == "standard-developer" && resource.approvedNetwork };
permit (principal == Agent::"claude-code-local", action == Action::"mcp.invoke", resource is ToolInvocation)
when { principal.enabled && resource.policyProfile == "standard-developer" && resource.approvedMCP };
permit (principal == Agent::"claude-code-local", action == Action::"agent.delegate", resource is ToolInvocation)
when { principal.enabled && resource.policyProfile == "standard-developer" && resource.approvedDelegate };
