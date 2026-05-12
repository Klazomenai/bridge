# Makefile for klazomenai/bridge. Operator helpers; the canonical build
# path remains the `CGO_ENABLED=0 go build -tags goolm` invocation
# documented in CONTRIBUTING.md.
#
# Run `make` (with no args) for a summary.

.PHONY: help sync-skills

.DEFAULT_GOAL := help

# Sibling-checkout default; override with `make sync-skills DOTFILES_DIR=/path/to/dotfiles`.
DOTFILES_DIR ?= ../dotfiles
EMBEDDED_DIR := internal/crew/skills/embedded

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z_-]+:.*## / { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

sync-skills: ## Re-bundle $(EMBEDDED_DIR)/ from a sibling dotfiles checkout
	@test -d "$(DOTFILES_DIR)" || { \
	  echo "error: DOTFILES_DIR ($(DOTFILES_DIR)) not found — clone klazomenai/dotfiles as a sibling or pass DOTFILES_DIR=/path/to/dotfiles"; \
	  exit 1; \
	}
	@test -f "$(DOTFILES_DIR)/claude/profiles/_universal.md" || { \
	  echo "error: $(DOTFILES_DIR) does not look like a klazomenai/dotfiles checkout (claude/profiles/_universal.md missing)"; \
	  exit 1; \
	}
	@mkdir -p "$(EMBEDDED_DIR)/github"
	@cp "$(DOTFILES_DIR)/claude/profiles/_universal.md"  "$(EMBEDDED_DIR)/_universal.md"
	@cp "$(DOTFILES_DIR)/claude/skills/github/SKILL.md"  "$(EMBEDDED_DIR)/github/SKILL.md"
	@cp "$(DOTFILES_DIR)/claude/profiles/github.md"      "$(EMBEDDED_DIR)/github/profile.md"
	@echo "Synced $(EMBEDDED_DIR)/ from $(DOTFILES_DIR)."
	@echo "After bumping DOTFILES_REF in Dockerfile, run 'make sync-skills' and commit"
	@echo "both edits in one PR. The skills-drift CI workflow catches mismatches."
