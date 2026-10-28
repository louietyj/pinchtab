# louietyj/pinchtab fork

This fork carries the captcha work behind the
[web-browser skill](https://github.com/louietyj/claude-skills/tree/main/web-browser):
CapSolver and 2Captcha solvers, CapSolver Vision, AliExpress's punish challenges,
captchas in child frames, and the tooling around them. Upstream ships the solver
as a stub. What each part does, and the evidence for it, is in that skill's
README under "Captcha solving"; this file is how the fork is worked on.

## Branch and releases

- Work goes straight onto `capsolver-rebase`; the fork's `main` is upstream's.
- Upstream comes in by `git merge upstream/main`, not a rebase, despite the
  branch name: a rebase replays each conflict once per fork commit that touches
  it. The first merge (capsolver.34) is tagged `pre-upstream-sync-2026-10` before it.
- Releases are GitHub releases tagged `v0.15.2-capsolver.N`, installed from
  `releases/latest` so a release needs no skill re-upload:
  - `pinchtab-linux-amd64`, which the skill's `setup.sh` runs in the sandbox;
  - `pinchtab-windows-amd64.exe`, the bridge the skill's Windows gatekeeper runs
    against Louie's Chrome (`pinchtab-windows`). The sandbox CLI and that bridge
    must be the same build, so they always ship together;
  - `pinchtab-linux-arm64` and `pinchtab-darwin-arm64`, for ARM hosts.
- Pushing an annotated tag publishes the release: `git tag -a v0.15.2-capsolver.N
  -m '<notes section>' && git push origin v0.15.2-capsolver.N`.
  `.github/workflows/fork-release.yml` builds all four, stamps `pinchtab
  --version` and `/health` with the tag, and publishes. This is how Claude
  sessions release: they can push a tag but not create a release.
  `scripts/fork-release.sh <N> notes.md` does the same from Louie's host, in
  Docker. Builds before capsolver.32 report `dev`.
- Notes: a `**Since capsolver.N-1**: …` section prepended to the previous
  release's body, so the latest release reads as the whole changelog. The
  workflow does the prepending; the tag message is the section alone.
- Upstream's tag-driven pipeline (RELEASE.md) is not how fork builds ship.

## Tests

- Run `go vet ./... && go test ./...` in the same `golang:1.26` container.
  The suite has failures that predate the fork's work, so compare failing test
  names against a baseline rather than expecting green. As of capsolver.34:
  - Both places: `TestEveryDeclaredRemedyResolvesInTheCLI` (its command table
    lacks `download -o`, which the real CLI has).
  - In the container (a worktree checkout):
    - It runs as root, so tests expecting a permission failure get none:
      `TestRecordConfigVersionAnnouncesItselfAndReportsAFailedWrite`,
      `TestLoadingLeavesAReadOnlyConfigsModeAlone`,
      `TestProfileRenameRollsBackMetadataWhenTheDirectoryRenameFails`,
      `TestProfileDirCleanupToleratesAFailedRemoval`.
    - `git ls-files` exits 128 from a worktree: `TestEveryAutomaticTriggerCanActuallyFire`,
      `TestEveryEnrolledAreaIsCoveredByTheMap`, `TestEveryEscalationRuleMatchesATrackedPath`.
    - No Chrome: `TestReportBrowsers_StatusReady`, `…StatusReadyGhostChrome`.
  - On the Windows host (`go test` outside the container):
    `TestAnUnwritableStateDirectoryFallsBackToPrinting` and
    `TestVocabStoreIsOneBoundedFilePerServer` (Windows permissions),
    `TestSafeRecordPath_RejectsSymlink` (symlinks need a privilege), and
    `TestDefaultConfigHint` (a real `%APPDATA%\pinchtab\config.json`).
- Upstream's `.gitattributes` checks text out as LF, so a checkout made before it
  needs `git rm -r --cached -q . && git reset --hard` once.
- Upstream adds census tests that scan the module and name what they want: a new
  route placed for the dialog guard, `X-PinchTab-*` names in `api/types`, each
  `output.Hint`/`Advisory` site classified, capped reads classified, vocab
  published by any handler that can re-epoch refs.
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
