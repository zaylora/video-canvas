# Contributing guide

Thanks for helping improve 连镜 (`video-canvas`). This guide explains how to file issues, commit code and open pull requests. For environment setup, architecture and check commands, see the [development guide](docs/development.en.md). Some linked documents are in Chinese.

[简体中文](CONTRIBUTING.md)

## Before you start

- **Small changes** (typos, clear bugs, documentation fixes) can go straight to a pull request.
- **Large changes** (new features, changes to existing interactions, refactors, new dependencies, database schema changes): please open an issue first and agree on the approach before writing code.
- Search existing issues and pull requests first to avoid duplicates.
- **Do not report security vulnerabilities in public issues.** Use the repository's Security tab ("Report a vulnerability") or contact a maintainer directly.

## Filing issues

Pick a type from the issue templates and fill it in.

| Type            | Use for                                                | Title format                    |
| --------------- | ------------------------------------------------------ | ------------------------------- |
| Bug report      | Something is broken, errors, results differ from spec  | `[Bug] one-line symptom`        |
| Feature request | New features, UX improvements, new model plugin needs  | `[Feature] what you want`       |

A good issue:

1. **Covers one topic.** File separate issues for separate problems.
2. **Has a specific title**: "Exported duration is wrong after trimming a video node", not "video is broken".
3. **Is reproducible (bugs)**: version (admin version settings page or `VERSION`), deployment (Docker / local), browser, steps to reproduce, actual and expected results. For failed generations include the task ID, plugin and model (never the key).
4. **Includes evidence**: screenshots, screen recordings, browser console errors, relevant backend log lines.
5. **Starts with the problem (feature requests)**: the scenario, how you do it today and why that hurts, then the solution you have in mind.
6. **Contains no secrets**: mask or remove API keys, tokens, `.env`, database connection strings and personal data.

Maintainers may apply labels such as `bug`, `enhancement`, `question`, `good first issue` and `needs-info`. Issues labelled `needs-info` will not move forward until the information is provided, and are closed after a long silence.

## Branches

1. Fork the repository (members with write access can branch directly) and branch from the latest `master`.
2. Name the branch `type/short-description`, with the same types as commit messages:

   ```text
   feature/canvas-group      # new feature
   fix/video-trim-duration   # bug fix
   docs/contributing         # documentation
   refactor/task-queue       # refactor
   ```

3. One branch, one purpose. Split unrelated changes into separate pull requests.
4. Never commit directly to `master`, and do not keep adding unrelated changes to a branch that has been merged.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```text
<type>(<scope>): <description>
```

- **Use an ASCII colon `:` after the type**, followed by a space.
- **type**: `feat` · `fix` · `perf` · `refactor` · `docs` · `test` · `build` · `ci` · `chore` · `revert`.
- **scope** is the module name and optional. Common ones: `web`, `canvas`, `agent`, `plugin`, `queue`, `task`, `backend`, `scripts`, `design` (design docs).
- **Description**: the project's commit history is written in Chinese, so write descriptions in Chinese where you can. English is accepted from contributors, but keep it to one line, no trailing period, saying what was done.
- Add a body after a blank line when the reason or trade-off needs explaining.
- Mark breaking changes with `!` after the type and a `BREAKING CHANGE:` paragraph in the body.

```text
feat(canvas): 图片节点新增标注（涂鸦）与宫格切分
fix(queue): 排队中 / 运行中按任务口径统计，并发为 1 时不再跳数字
docs(design): 3D 导演台补充面板可拖动调节的设计记录
```

Notes:

- **Commit messages go into the changelog verbatim** (CHANGELOG is generated from commits by git-cliff), so write something users can understand. No "wip", "fix bug" or "update".
- One commit, one independently understandable change. A schema change goes in the same commit as its migration file and tests.
- **Do not edit** `CHANGELOG.md` or `VERSION` by hand; the release process maintains them.

## Pre-submit checks

Run what matches your change (details in the [development guide](docs/development.en.md#tests-and-checks)):

```bash
# Backend (in backend/)
make fmt && make lint && make test

# Frontend (in web/)
bun run typecheck && bun run lint && bun run format:check && bun test

# Agent runtime (in agent/, only when you change it)
npm test
```

Also confirm:

- Features, fixes, behaviour changes and refactors come with tests, written failing first.
- No secrets, tokens, real connection strings or runtime data are committed.
- Schema changes add a new migration file and **never modify a released migration**.
- Changes to APIs, configuration or user-visible behaviour update the related docs.
- The frontend is formatted and linted only with the bundled oxfmt / oxlint, not Prettier.

Running `pre-commit install` is recommended; it runs most of these checks on commit.

## Opening a pull request

1. Push your branch to your fork and open a PR against `master` of `zaylora/video-canvas`.
2. **Title** follows the commit format: `feat(canvas): 支持节点分组`.
3. **Description** follows the PR template and must at least cover:
   - **Why**: the linked issue (`Closes #123`) or the problem being solved;
   - **What**: key changes and trade-offs, not a line-by-line recap;
   - **How verified**: commands you ran, what you checked by hand, and the results;
   - **UI changes**: before / after screenshots or a recording;
   - **Risk and compatibility**: migrations, API or configuration changes, effect on existing data.
4. **Keep it small and focused**: one purpose per PR, reviewable in one sitting. Split large changes, or open a draft PR early to discuss direction.
5. Mark unfinished work as **Draft** and switch to Ready for review when done.
6. CI runs backend and frontend checks and tests. **A PR with failing CI is not merged**, so look at the failure yourself first.

### Review and merge

- Maintainers aim to respond within a few days. Review comments are about the code; if you disagree, say why.
- Address feedback by pushing more commits to the same branch; do not close and reopen the PR. Reply once handled, or explain why you did not change it.
- Before merging the branch must include the latest `master` and have no conflicts.
- PRs are merged with a **merge commit**, so every commit on the branch stays in history. Squash noise such as "fix typo" before submitting.
- Issues linked with `Closes #xx` are closed automatically on merge.

## Releases

Maintainers handle releases (updating `VERSION` and `CHANGELOG.md`, tagging `v*`). Do not change those files in a PR. See the [development guide](docs/development.en.md#ci-and-releases).

## Code of conduct

Be kind and stay on topic. Be respectful to people and rigorous with code.
