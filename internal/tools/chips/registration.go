package chips

import "klazomenai/bridge/internal/tools"

// RegisterChipsTools registers the full Chips tool roster on reg.
//
// Tool registration is intentionally a fixed roster: high-risk mutations
// such as `gh_pr_merge` and `gh_pr_ready` are deliberately neither
// implemented nor registered — those are reserved as human decisions
// after peer review. See `_universal.md` ("High-Risk Mutations —
// Additional Confirmation Required"): for tools listed as "refuse
// outright", the persona must not register them as callable at all,
// regardless of operator confirmation.
func RegisterChipsTools(reg *tools.Registry, execFn ExecFn, allowlist RepoAllowlist, token string) {
	reg.Register(NewGHIssueListTool(execFn, allowlist, token))
	reg.Register(NewGHIssueViewTool(execFn, allowlist, token))
	reg.Register(NewGHIssueCreateTool(execFn, allowlist, token))
	reg.Register(NewGHPRListTool(execFn, allowlist, token))
	reg.Register(NewGHPRViewTool(execFn, allowlist, token))
	reg.Register(NewGHPRChecksTool(execFn, allowlist, token))
	reg.Register(NewGitLogTool(execFn, token))
	reg.Register(NewGitDiffTool(execFn, token))
}
