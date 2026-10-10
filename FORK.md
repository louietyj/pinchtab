# louietyj/pinchtab fork

This fork carries the captcha work behind the
[web-browser skill](https://github.com/louietyj/claude-skills/tree/main/web-browser):
CapSolver and 2Captcha solvers, CapSolver Vision, AliExpress's punish challenges,
captchas in child frames, and the tooling around them. Upstream ships the solver
as a stub. What each part does, and the evidence for it, is in that skill's
README under "Captcha solving"; this file is how the fork is worked on.

## Branch and releases

- Work goes straight onto `capsolver-rebase`; the fork's `main` is upstream's.
- Releases are GitHub releases tagged `v0.15.2-capsolver.N`, with two assets,
  both installed from `releases/latest` so a release needs no skill re-upload:
  - `pinchtab-linux-amd64`, which the skill's `setup.sh` runs in the sandbox;
  - `pinchtab-windows-amd64.exe`, the bridge the skill's Windows gatekeeper runs
    against Louie's Chrome (`pinchtab-windows`). The sandbox CLI and that bridge
    must be the same build, so they always ship together.
- `scripts/fork-release.sh <N> notes.md` builds both in Docker (the host is
  Windows), stamps `pinchtab --version` and `/health` with the tag, and publishes
  the release. Builds before capsolver.32 report `dev`.

- Notes: prepend a `**Since capsolver.N-1**: …` section to the previous
  release's body, so the latest release reads as the whole changelog.
- Upstream's tag-driven pipeline (RELEASE.md) is not how fork builds ship.

## Tests

- Run `go vet ./... && go test ./...` in the same `golang:1.26` container.
  The suite has failures that predate the fork's work, so compare failing test
  names against a baseline rather than expecting green.
- With `core.autocrlf=true` on Windows, run gofmt on CR-stripped copies of the
  changed files; otherwise every file reads as unformatted.
- Several tests enforce structure across the module and will name what they
  want:
  - `ChallengeType` is produced only in `challenge_detection.go`.
  - `Intent.Type` is read through `intentTypeOf`.
  - Code that resolves nodes in a page's main world is listed in
    `mainWorldResolvers` (`internal/bridge/isolated_world_test.go`), with a
    reason.
  - Solvers use the top-level `math/rand` functions, never their own
    `*rand.Rand`.
- A change a browser can show gets checked in the skill's local sandbox too, with
  the fixtures and procedure in the skill README's "Debugging" section.
  AliExpress only challenges claude.ai's IPs, so those checks are claude.ai runs.
