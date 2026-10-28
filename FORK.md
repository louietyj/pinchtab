# louietyj/pinchtab fork

This fork carries the captcha work behind the
[web-browser skill](https://github.com/louietyj/claude-skills/tree/main/web-browser):
CapSolver and 2Captcha solvers, CapSolver Vision, AliExpress's punish challenges,
captchas in child frames, and the tooling around them. Upstream ships the solver
as a stub. What each part does, and the evidence for it, is in that skill's
README under "Captcha solving"; this file is how the fork is worked on.

## Branch and releases

- Work goes straight onto `main`. (`capsolver-rebase` was the working branch
  through capsolver.37; `main` was fast-forwarded to it on 2026-10-28.)
- Upstream comes in by `git merge upstream/main`, not a rebase, despite the
  branch name: a rebase replays each conflict once per fork commit that touches
  it. The first merge (capsolver.34) is tagged `pre-upstream-sync-2026-10` before it.
- Every push to `main` publishes an edge release, `edge-<run>-<sha7>`, marked
  latest (`.github/workflows/fork-release.yml`), after `go vet` and the builds
  pass. The skill installs `releases/latest`, so a push reaches the next
  sandbox with no skill re-upload; to roll back, mark an older release latest.
  The newest 20 edge releases are kept. Each carries:
  - `pinchtab-linux-amd64`, which the skill's `setup.sh` runs in the sandbox;
  - `pinchtab-windows-amd64.exe`, the bridge the skill's Windows gatekeeper runs
    against Louie's Chrome (`pinchtab-windows`). The sandbox CLI and that bridge
    must be the same build, so they always ship together;
  - `pinchtab-linux-arm64` and `pinchtab-darwin-arm64`, for ARM hosts.
- All four are stamped with the tag, which `pinchtab --version` and `/health`
  report. Notes are the commit subjects since the previous release, so write
  commit messages for that reader.
- Claude sessions can push to `main` but may neither create releases nor push
  tags, so this is how they ship. `scripts/fork-release.sh <N> notes.md`, from
  Louie's host in Docker, still cuts a numbered `v0.15.2-capsolver.N` release
  (capsolver.37 was the last); builds before capsolver.32 report `dev`.
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
