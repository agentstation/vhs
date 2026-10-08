# Demo capture review

Reviewed on 2026-10-07. Use AgentStation VHS for terminal SVG demos.
Update the existing timing PR before release. Keep GIF fallbacks for README embeds.
Use browser recordings for the Starport Console.

## Source comparison

| Source | Reviewed revision | Finding |
| --- | --- | --- |
| AgentStation main | `038df4f741b658981a9176671cd14850ff859d8c` | Supports native animated SVG, GIF, MP4, and WebM. |
| Charm main | `24fa2254a9806091e6ee6a980e9f3bcfe0a9ba53` | Release v0.12.1. Supports raster recordings and has no native SVG output. |
| Charm module | Go 1.26.7 | Includes dependency and browser updates absent from the fork. |
| AgentStation module | Go 1.24.1 | Retains 17 fork commits and lacks 31 upstream commits. |

The counts compare the two revisions above. They do not include this local patch.
[AgentStation source](https://github.com/agentstation/vhs/tree/038df4f741b658981a9176671cd14850ff859d8c)
and [Charm source](https://github.com/charmbracelet/vhs/tree/24fa2254a9806091e6ee6a980e9f3bcfe0a9ba53) support this comparison.

Charm still captures raster frames at the requested frame rate.
It encodes those frames at that rate, even when capture is slower.
The local patch preserves visible elapsed time for all supported recording formats.

Charm added encoder error propagation in
[PR #788](https://github.com/charmbracelet/vhs/pull/788).
The local patch also returns encoder failures.
Charm added browser startup and Windows rendering fixes in
[PR #781](https://github.com/charmbracelet/vhs/pull/781).
This patch keeps the current launcher and adds an explicit browser path.
Chrome passed local capture checks. Windows remains unverified.

Port PR #781 before a Windows release.
Its bounded DevTools probe addresses a startup hang, separate from frame timing.
The current product captures target macOS, so a bulk upstream merge is unnecessary for this rollout.

The fork also lacks upstream viewport scrolling, Ctrl+arrow support, and rows/columns settings.
These features are not prerequisites for the product demos.
A separate upstream synchronization needs compatibility and release checks.

## Existing AgentStation PRs

| PR | Reviewed revision | Decision |
| --- | --- | --- |
| [#6: timing](https://github.com/agentstation/vhs/pull/6) | `1e430f095a75a8dce41ed8f3b37d076a93b697df` | Update this PR with the timing and error fixes. |
| [#7: grid](https://github.com/agentstation/vhs/pull/7) | `ee7844d7d6516a95dc6998387d87ebb75b43eaab` | Optional viewport adjustment. Does not fix font portability. |
| [#8: fonts](https://github.com/agentstation/vhs/pull/8) | `fb3666d439234c95a7804703a1214bfc4fd16829` | Prefer an exact font file for reproducible capture. |
| [#9: progress](https://github.com/agentstation/vhs/pull/9) | `3b7b233e9eb594e9804d8e7b748f224147a6db93` | Optional feature. Not needed for accurate demos. |

PR #6 uses elapsed timestamps for SVG keyframes.
It estimates an average raster frame rate only when SVG frames also exist.
An average rate cannot preserve uneven capture intervals.
The PR also uses the last capture timestamp as the recording duration.
That omits the final visible hold.

PR #6 still captures PNG layers for SVG output.
It detects hidden spans only during capture ticks.
It logs capture failures and continues.
The local patch addresses these cases and tests their behavior.

PR #8 resolves fonts through `fc-match` and optionally subsets them through `pyftsubset`.
A fallback font from `fc-match` can differ from the font that Chrome selects.
Without those tools, the PR can omit the embedded font.
The local option loads the same exact font into Chrome and the generated SVG.
Glyphs absent from that font still require fallback rendering.

## Local capture contract

- SVG capture skips raster layers unless another output or screenshot requires them.
- Capture timestamps measure visible elapsed time.
- `Hide` and `Show` exclude hidden time at command transitions.
- SVG duration includes the final visible hold.
- Raster output repeats captured states at the requested playback rate.
- Close SVG timestamps retain distinct CSS keyframes after state deduplication.
- Capture and encoder failures return errors.
- Evaluate checks capture failures between commands.
- The browser remains open until the active command finishes and Evaluate closes it.
- `LoopOffset` uses a percentage for SVG and raster output.

Select Chrome through `--browser-path` or `VHS_BROWSER_PATH`.
The command-line option takes precedence.
Without either value, VHS retains automatic browser discovery.
[Charm issue #88](https://github.com/charmbracelet/vhs/issues/88) records browser-dependent timing reports.

The original reporter saw no improvement after switching to Chrome.
[2022 report](https://github.com/charmbracelet/vhs/issues/88#issuecomment-1295874027)

A later reporter said the Chrome change fixed their issue.
[2026 report](https://github.com/charmbracelet/vhs/issues/88#issuecomment-5388034558)
These reports do not establish Brave as the only cause.
A browser choice alone does not prove correct playback timing.

Use `--svg-font-file` or `VHS_SVG_FONT_FILE` for an exact font file.
Supported formats are WOFF2, WOFF, TrueType, and OpenType.
This option overrides `Set FontFamily` for capture and SVG output.
The caller must supply a font whose license permits embedding.
The file loads before capture, without `fc-match` or `pyftsubset`.

## Validation

The Go suite and race checks pass. Focused regressions cover slow capture, hidden spans,
invalid frame rates, capture errors, raster repetition, screenshots, and sparse SVG timestamps.
Tests also cover exact font bytes and encoder failure propagation.
A browser integration test forces a frame write failure during typing.
Evaluate returns that capture error and stops before the next command.

All 103 example tapes outside `examples/errors` pass syntax validation.

The welcome tape now invokes `gum spin` and declares its Gum dependency.
The meta tape declares its nested VHS and Gum dependencies.
[Current Gum source](https://github.com/charmbracelet/gum/blob/main/gum.go) defines `spin` as a subcommand.
The other external programs in inherited examples did not receive a complete runtime audit.

A mixed GIF/SVG capture with a two-second hidden span produced a 3.09-second SVG.
The GIF lasted 3.04 seconds, within one 100-millisecond frame interval.
The PNG screenshot contained the terminal command output.
An SVG-only capture also passed.

A repeat capture with the exact Geist Mono WOFF2 produced an embedded font rule.
Its SVG lasted 3.10 seconds and its GIF lasted 3.12 seconds.
The capture browser was Google Chrome with ttyd 1.7.7.

The final build also passed the same tape with Brave and Chrome on macOS.
Each browser produced a 4.69-second SVG and a 4.64-second GIF.
Both excluded a two-second hidden span and used the embedded Geist Mono font.
These results qualify the selected local browser builds. They do not close upstream issue #88.

The installed golangci-lint is v2.13.1. CI pins v2.3.0.
The full local lint reports 50 existing findings.
The changed-code lint and `go vet ./...` pass.
GitHub image rendering still needs direct verification for each final asset.
Keep the GIF fallback until that check passes.

## GitHub recommendation

Update the existing timing PR with the tested local fixes.
Review the explicit font option with the existing font PR.
Run the repository checks and structured review before publishing a revision.
Publish a tested release, then bind demo generation to its immutable revision and binary hash.
This review does not publish a PR, push a branch, merge changes, or create a release.

SVG dimensions fit the recorded terminal rows and columns. Fractional cell sizes round up to a full pixel. Padding, window bars, and margins remain in the output. SVG sizing preserves capture settings and rounded corner transparency.
