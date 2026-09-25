# louietyj/pinchtab fork

This fork carries the captcha work behind the
[headless-browser skill](https://github.com/louietyj/claude-skills/tree/main/headless-browser):
CapSolver and 2Captcha solvers, CapSolver Vision, AliExpress's punish challenges,
captchas in child frames, and the tooling around them. Upstream ships the solver
as a stub. What each part does, and the evidence for it, is in that skill's
README under "Captcha solving"; this file is how the fork is worked on.

## Branch and releases

- Work goes straight onto `capsolver-rebase`; the fork's `main` is upstream's.
- Releases are GitHub releases tagged `v0.15.2-capsolver.N`. Each carries one
  asset, `pinchtab-linux-amd64`, the build the skill's `setup.sh` installs from
  `releases/latest`, so a release reaches claude.ai with no skill re-upload.
- Build the asset in Docker, since the host is Windows:

  ```bash
  docker run --rm -v "$PWD":/src -v ptgomod:/go/pkg/mod -v ptgocache:/root/.cache/go-build -w /src \
    -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=amd64 golang:1.26 go build -o /src/pinchtab-linux-amd64 ./cmd/pinchtab
  gh release create v0.15.2-capsolver.N -R louietyj/pinchtab --target capsolver-rebase \
    --title "v0.15.2 + CapSolver (fork build)" --notes-file notes.md --latest pinchtab-linux-amd64
  ```

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
