#!/usr/bin/env bash
# =============================================================================
# push-archgen.sh: publish the archgen CLI to GitHub as a clean, reviewable
# history (one commit per package, in dependency order, each one building).
#
# Usage:
#   GH_USER=<github-user-or-org> ./push-archgen.sh [path/to/archgen]
#
# Environment (all optional except GH_USER):
#   GH_USER       GitHub user or organization                (required)
#   REPO          repository name                            (default: archgen)
#   BRANCH        branch name                                (default: main)
#   VISIBILITY    public | private (used when creating repo) (default: public)
#   REMOTE_URL    override the remote (default: git@github.com:$GH_USER/$REPO.git)
#   MODULE_PATH   Go module path of the CLI                  (default: github.com/$GH_USER/$REPO)
#   KIT_PATH      archkit import path used by default in generated projects
#                 (default: github.com/$GH_USER/go-archkit, as published by push-go-archkit.sh)
#   ARCHKIT_PATH  local archkit checkout: the final tests also compile and test a generated project
#   CREATE_REPO   1 = create the repo with the GitHub CLI (gh) if missing (default: 1)
#   VERIFY        1 = run `go build ./...` after every commit, `go vet` + `go test` at the end (default: 1)
#   TAG           tag to create and push, empty to skip       (default: v0.1.0)
#   PUSH          0 = build the local history only            (default: 1)
#   FORCE         1 = allow running on a directory that is already a git repository
# =============================================================================
set -euo pipefail

SRC="${1:-./archgen}"
: "${GH_USER:?Set GH_USER to your GitHub user or organization (e.g. GH_USER=acme ./push-archgen.sh)}"
REPO="${REPO:-archgen}"
BRANCH="${BRANCH:-main}"
VISIBILITY="${VISIBILITY:-public}"
REMOTE_URL="${REMOTE_URL:-https://github.com/${GH_USER}/${REPO}.git}"
MODULE_PATH="${MODULE_PATH:-github.com/${GH_USER}/${REPO}}"
KIT_PATH="${KIT_PATH:-github.com/${GH_USER}/go-archkit}"
CREATE_REPO="${CREATE_REPO:-1}"
VERIFY="${VERIFY:-1}"
TAG="${TAG-v0.1.0}"
PUSH="${PUSH:-1}"
FORCE="${FORCE:-0}"
OLD_KIT="github.com/yourorg/archkit"

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m ✓\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# ----------------------------------------------------------------------------- checks
command -v git >/dev/null || die "git is not installed"
[ -f "$SRC/go.mod" ] && [ -d "$SRC/cmd/archgen" ] || die "$SRC does not look like the archgen CLI (no go.mod or cmd/archgen)"
cd "$SRC"
if [ -d .git ] && [ "$FORCE" != "1" ]; then
  die "$SRC is already a git repository. Re-run with FORCE=1 to add commits on top of it."
fi
git config user.name  >/dev/null || die "set your identity first: git config --global user.name 'Your Name'"
git config user.email >/dev/null || die "set your identity first: git config --global user.email you@example.com"
if [ "$VERIFY" = "1" ]; then command -v go >/dev/null || die "go is not installed (or run with VERIFY=0)"; fi

# replace_all <old> <new>: in go.mod, .go, .md and .tmpl files (portable sed: GNU and BSD)
replace_all() {
  grep -rl --include='*.go' --include='go.mod' --include='*.md' --include='*.tmpl' "$1" . 2>/dev/null | while read -r f; do
    sed -i.bak "s#${1}#${2}#g" "$f" && rm -f "$f.bak"
  done
}

# ----------------------------------------------------------------------------- module paths
CURRENT_MODULE="$(awk '/^module /{print $2; exit}' go.mod)"
if [ "$CURRENT_MODULE" != "$MODULE_PATH" ]; then
  say "Rewriting module path $CURRENT_MODULE -> $MODULE_PATH"
  replace_all "$CURRENT_MODULE" "$MODULE_PATH"
  ok "module path updated"
fi
if [ "$KIT_PATH" != "$OLD_KIT" ] && grep -rq --include='*.go' --include='*.md' "$OLD_KIT" .; then
  say "Setting the default archkit import path: $OLD_KIT -> $KIT_PATH"
  replace_all "$OLD_KIT" "$KIT_PATH"
  ok "default --kit updated"
fi
if [ "$VERIFY" = "1" ] && [ -n "$(gofmt -l . 2>/dev/null)" ]; then gofmt -w .; fi

# ----------------------------------------------------------------------------- helpers
[ -d .git ] || git init -q -b "$BRANCH"
git checkout -q -B "$BRANCH"

# commit "<subject>" "<detail 1>" "<detail 2>" ... -- <paths...>
# The message follows the format:
#   <subject>
#
#   * <detail 1>
#   * <detail 2>
commit() {
  local subject="$1"; shift
  local details=()
  while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do details+=("$1"); shift; done
  [ "${1:-}" = "--" ] && shift
  local paths=("$@")
  local existing=()
  for p in "${paths[@]}"; do [ -e "$p" ] && existing+=("$p"); done
  [ "${#existing[@]}" -gt 0 ] || { echo "   (skipped: nothing to commit for \"$subject\")"; return 0; }
  git add -- "${existing[@]}"
  if git diff --cached --quiet; then echo "   (skipped: no changes for \"$subject\")"; return 0; fi
  local body=""
  for d in "${details[@]}"; do body+="* ${d}"$'\n'; done
  git commit -q -m "$subject" -m "${body%$'\n'}"
  ok "$subject"
  if [ "$VERIFY" = "1" ]; then
    go build ./... || die "build failed after: $subject"
  fi
}

# ----------------------------------------------------------------------------- history
say "Creating commits"

commit "chore: initialize the archgen module" \
  "go.mod targeting Go 1.24 with no third-party dependencies" \
  "Makefile with build, install, vet and test targets; .gitignore for bin/" \
  -- go.mod go.sum .gitignore Makefile

commit "feat(naming): add identifier conversion and pluralization" \
  "Words splitter for camelCase, PascalCase, snake_case, kebab-case and acronyms (userID -> user id)" \
  "Pascal and Camel with Go initialisms (ID, URL, UUID, HTTP...), JSON, snake, kebab, package and human forms" \
  "English Plural/Singular with irregular and uncountable words, preserving the input case" \
  "Go keyword detection and table-driven tests" \
  -- internal/naming

commit "feat(fields): add the field DSL parser" \
  "name:type[:modifier...] specs: string, text, email, url, uuid, int, int64, float, bool, time, enum(a|b|c)" \
  "optional, unique and index modifiers; precise errors for unknown types, modifiers and duplicates" \
  "per-field Go type, SQL column definition, validation rules and sample values for generated tests" \
  -- internal/fields

commit "feat(gen): add a safe file writer" \
  "never overwrites existing files without --force; dry-run mode records the plan without touching disk" \
  "gofmt validation of every generated Go file, with numbered source on generator bugs" \
  "idempotent snippet insertion at // archgen:<marker> comments, keeping indentation" \
  "import insertion, unused-import pruning and std/third-party/local import grouping" \
  -- internal/gen

commit "feat(project): add project discovery and configuration" \
  "finds the project root by walking up to go.mod and reads the module path" \
  ".archgen.json with archkit path, driver, API prefix, modules and migrations directories and versioning style" \
  "module lookup and listing helpers" \
  -- internal/project

commit "feat(tmpl): add the template engine and the project skeleton" \
  "embedded text/template set with naming helpers and typed Project, Module, Resource and Data models" \
  "cmd/api entry point, bootstrap, config, dependencies (composition root), router and RBAC" \
  "shared kernel, database, HTTP, logging and observability packages" \
  "embedded migrations with the outbox table, Makefile, docker-compose, .env.example, .gitignore and README" \
  -- internal/tmpl/tmpl.go internal/tmpl/templates/project_*.tmpl

commit "feat(tmpl): add business module templates" \
  "module.go with Deps, New, RegisterRoutes and Subscribe, plus layer docs for domain, commands, queries, ports, persistence and HTTP" \
  "contract package for the module's public API" \
  "migration creating the module's own PostgreSQL schema" \
  -- internal/tmpl/templates/module_*.tmpl

commit "feat(tmpl): add the aggregate CRUD slice templates" \
  "aggregate with private fields, invariants, domain events, snapshot/restore and an errors catalog" \
  "repository port with filtering and sorting; create, update and delete commands; get and list queries with a view model" \
  "PostgreSQL repository with optimistic locking, outbox events and unique-constraint to domain-error mapping" \
  "in-memory repository, HTTP endpoints, table migration and domain, application and HTTP tests" \
  -- internal/tmpl/templates/resource_*.tmpl

commit "feat(tmpl): add use case, event, contract and port templates" \
  "standalone command and query use cases with validation, permissions and registration" \
  "internal domain events and public integration events, plus idempotent event subscribers" \
  "cross-module read contracts with their adapters, application ports with infrastructure adapter stubs" \
  -- internal/tmpl/templates/x_*.tmpl

commit "feat(check): add the architecture linter" \
  "layer-direction: domain <- application <- interfaces, nothing depends on infrastructure" \
  "layer-technology: no HTTP, SQL, drivers, ORMs or brokers in the domain; no HTTP/SQL in the application layer" \
  "module-boundary and domain-isolation: other modules only through their contract package, never from the domain" \
  "shared-no-business: internal/shared never imports business modules; composition root and cmd/ exempt" \
  -- internal/check

commit "feat(cli): add the archgen commands" \
  "new, module and resource scaffold a project, a module with its schema, and a full CRUD slice" \
  "command, query, event, subscribe, contract, use, port and migration extend existing modules and wire them" \
  "timestamped or sequential migrations with collision handling" \
  "check (text or JSON, exit code 1 on violations for CI), list and version" \
  "flags anywhere among arguments, --dir, --dry-run and --force; end-to-end scaffold-and-check tests" \
  -- internal/cli cmd/archgen

commit "docs: add the README" \
  "installation, quick start and command reference" \
  "field DSL, mapping of the generated code to the architecture, check rules and safety guarantees" \
  -- README.md

# anything left over (new files added later, etc.)
if [ -n "$(git status --porcelain)" ]; then
  git add -A
  git commit -q -m "chore: add remaining files" -m "* files not covered by the previous commits"
  ok "chore: add remaining files"
fi

if [ "$VERIFY" = "1" ]; then
  say "Verifying the final tree"
  go vet ./...  || die "go vet failed"
  ARCHKIT_PATH="${ARCHKIT_PATH:-}" go test -count=1 ./... || die "go test failed"
  ok "vet and tests pass"
fi

echo
git --no-pager log --oneline --reverse

[ -n "$TAG" ] && { git tag -a "$TAG" -m "$TAG" 2>/dev/null || true; ok "tag $TAG"; }

# ----------------------------------------------------------------------------- push
if [ "$PUSH" != "1" ]; then
  say "PUSH=0: local history created, nothing pushed"
  exit 0
fi

if [ "$CREATE_REPO" = "1" ] && command -v gh >/dev/null; then
  if ! gh repo view "${GH_USER}/${REPO}" >/dev/null 2>&1; then
    say "Creating ${VISIBILITY} repository ${GH_USER}/${REPO}"
    gh repo create "${GH_USER}/${REPO}" "--${VISIBILITY}" \
      --description "Code generator for Go modular monoliths built with Clean Architecture and archkit" >/dev/null
    ok "repository created"
  fi
elif [ "$CREATE_REPO" = "1" ]; then
  say "GitHub CLI (gh) not found: make sure https://github.com/${GH_USER}/${REPO} exists (empty, no README)"
fi

if git remote get-url origin >/dev/null 2>&1; then
  git remote set-url origin "$REMOTE_URL"
else
  git remote add origin "$REMOTE_URL"
fi

say "Pushing to $REMOTE_URL"
git push -u origin "$BRANCH"
[ -n "$TAG" ] && git push origin "$TAG"
ok "done: https://github.com/${GH_USER}/${REPO}"
echo
echo "Install it:   go install ${MODULE_PATH}/cmd/archgen@${TAG:-latest}"
echo "Start a project: archgen new backend --module github.com/${GH_USER}/backend --kit ${KIT_PATH}"