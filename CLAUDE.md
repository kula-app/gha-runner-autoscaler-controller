## Project Guidelines

**ALWAYS adhere to AGENTS.md at all times.** This file contains comprehensive development patterns, conventions, and best practices for the Shipable Deployer project.

## Critical Rules

1. **Read AGENTS.md**: Familiarize yourself with all guidelines before making changes
2. **No AI References**: NEVER mention AI assistant names (Claude, ChatGPT, Cursor, etc.) in:
   - Git commit messages
   - Pull request titles or descriptions
   - Code comments (unless technically relevant)
   - Co-authored-by tags
   - Generated-with footers

3. **Follow Conventions**: All code, commits, and PRs must follow the patterns documented in AGENTS.md:
   - Conventional Commits 1.0.0 for commit messages
   - Typed DTOs instead of generic maps
   - Validator tags for struct validation
   - Proper logging patterns (service logger, not fmt.Print)
   - CLI flag naming standards
   - Certificate serial number handling (hexadecimal format)

## Quick Reference

- Use `make help` to discover available commands
- Test changes against dev server: `make dev`
- Build CLI: `make build`
- Follow file structure and naming conventions in AGENTS.md
- Create focused, atomic commits with clear messages
