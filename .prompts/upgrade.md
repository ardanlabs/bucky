WHISPER_VERSION = v1.9.2
BUCKY_VERSION = v1.0.8

Upgrade this Bucky repository to whisper.cpp <WHISPER_VERSION> and prepare
Bucky release <BUCKY_VERSION>.

Use the latest published Bucky tag before this work as the release-note
baseline. If <BUCKY_VERSION> already exists locally or on the remote, do not
move or replace the tag. Stop and select/recommend the next unused patch
version instead.

Carry the upgrade through implementation, runtime validation, benchmarks,
documentation, and release notes. Do not only change the version string.

## Preflight

1. Inspect git status and preserve unrelated worktree changes.
2. Inspect local and remote Bucky tags.
3. Identify the currently pinned whisper.cpp version.
4. Confirm that ardanlabs/bucky-builder has completed the required
   <WHISPER_VERSION> artifacts:
   - macOS Metal universal
   - Windows CPU amd64
   - Linux CPU amd64 and arm64
   - Linux CUDA amd64 and arm64
   - Linux Vulkan amd64 and arm64
5. Confirm any Windows CUDA artifact still obtained directly from upstream is
   available and that its fixed filename/version remains correct.
6. If required release assets are unavailable, report the missing artifacts
   and do not update the default yet.

## FFI and ABI audit

Compare ggml-org/whisper.cpp's public `include/whisper.h` between the current
pinned tag and <WHISPER_VERSION>.

Audit every ABI-sensitive type and function used by `pkg/whisper`, including:

- `whisper_context_params`
- `whisper_full_params`
- `whisper_token_data`
- `whisper_vad_params`
- `whisper_vad_context_params`
- enums used as FFI integers
- callback typedefs
- every function prepared through `lib.Prep`

Identify:

- struct fields added, removed, reordered, or retyped
- changed enum values
- changed function parameters or return types
- removed or renamed symbols
- newly added APIs that are optional rather than required for compatibility
- expected 64-bit struct sizes and alignment

Make the smallest required FFI changes. Do not add bindings for optional new
features unless needed for compatibility or clearly valuable to Bucky's
existing API.

Update the upgrade tests when layouts change. Ensure the tests verify
by-value and by-reference parameter handling where applicable.

## Pinned version and download matrix

Update all places that establish or describe the default whisper.cpp version,
including:

- `pkg/download.DefaultWhisperVersion`
- platform download-matrix tests
- installer CLI help
- Makefile examples
- README architecture/version references
- INSTALL source-build instructions
- relevant comments and examples
- CI behavior that relies on the default

Add or update an exact regression test asserting that
`DefaultWhisperVersion` equals <WHISPER_VERSION>.

Do not remove tests proving that explicitly requested older valid tags are
still accepted.

Search the entire repository for stale references to the previous Whisper
version. Distinguish historical benchmark provenance from active defaults:
do not change historical claims unless the benchmarks are actually rerun.

## README compatibility table

Determine whether changes in <WHISPER_VERSION> make it incompatible with the
previous Bucky release. If a breaking change requires users to upgrade Bucky,
update the known-compatible-versions table in `README.md`:

- replace the previous row's open-ended `+` range with the last whisper.cpp
  version compatible with that Bucky release
- add a row beginning with <WHISPER_VERSION> and identify <BUCKY_VERSION> as
  the minimum compatible Bucky release
- use explicit version ranges when later releases change either boundary

Do not change the table for a non-breaking whisper.cpp upgrade. In the final
report, state whether the compatibility table changed and why.

## Local runtime validation

The repository's `lib/` directory is ignored and may be updated.

Upgrade the local ignored library using Bucky's installer and the exact
bucky-builder artifact, for example:

    go run . install \
        -lib "$PWD/lib" \
        -p metal \
        -v <WHISPER_VERSION> \
        -u \
        -q

Adapt the processor for the current host if necessary.

Verify:

- the local library was replaced
- `whisper.Version()` reports the expected version without the leading `v`
- all required symbols load
- FFI struct-size/default-parameter tests pass
- model loading and transcription tests pass when fixtures are available

Do not validate only against a differently packaged Homebrew library when the
Bucky artifact can be downloaded.

## Benchmarks

Rerun every result recorded in `BENCHMARKS.md` using the exact local library
installed above:

1. End-to-end JFK transcription:

   BUCKY_LIB="$PWD/lib" \
    BUCKY_BENCH_MODEL="$HOME/models/ggml-tiny.bin" \
   BUCKY_TEST_AUDIO="$PWD/samples/jfk.wav" \
    go test -count=1 \
        -bench=BenchmarkFullJFK \
        -benchtime=10x \
        -run='^$' \
    ./pkg/whisper/

2. Upstream memcpy benchmark through `whisper.BenchMemcpyStr(4)`.

3. Upstream matrix-multiplication benchmark through
   `whisper.BenchGGMLMulMatStr(4)`.

4. Pure-Go audio benchmarks:

   BUCKY_TEST_AUDIO="$PWD/samples/jfk.wav" \
    go test -count=1 \
        -bench=. \
        -benchtime=2s \
        -run='^$' \
    -benchmem \
    ./pkg/audio/

Update `BENCHMARKS.md` with the actual output:

- artifact provenance
- end-to-end `ns/op`, audio duration, and RTF
- memcpy results
- selected matrix results
- audio `ns/op`, B/op, allocations, and correctly recalculated percentages

Keep or improve exact reproduction instructions. Never change only the
Whisper version label while retaining measurements from the previous
library.

## Bucky release version

Set Bucky's reported version to <BUCKY_VERSION> in `version.go`.

Update the version test to assert the exact expected value rather than merely
checking that the value is non-empty.

Do not create, move, force-update, or push a Git tag without explicit
approval.

## Verification

After all changes, run:

    gofmt -s -w <changed Go files>
    go vet ./...
    staticcheck ./...
    go build ./...
    go test ./...
    gopls check <changed Go files>
    git diff --check

Also run the `pkg/whisper` tests with `BUCKY_LIB="$PWD/lib"` so they exercise
the newly installed Whisper library.

Do not suppress diagnostics. Report skipped tests and missing model/audio/VAD
fixtures honestly.

## Release notes

Produce Markdown release notes for <BUCKY_VERSION> using the requested
baseline tag through <BUCKY_VERSION>, not merely the immediately preceding
commit.

Include, when applicable:

- the whisper.cpp upgrade
- platform artifact/download changes
- FFI and ABI changes or explicit compatibility confirmation
- dependency upgrades
- benchmark results
- regression coverage
- Bucky version reporting
- compatibility and breaking-change information
- upgrade commands
- a full changelog URL

Use this upgrade command:

    go install github.com/ardanlabs/bucky@<BUCKY_VERSION>
    bucky install -u -lib ./lib

Copy the final Markdown release notes into the macOS clipboard with `pbcopy`
and verify the clipboard heading.

## Final report

Summarize:

- files changed
- whether FFI changes were required
- the installed local library version
- benchmark highlights
- verification results
- release/tag state
- confirmation that the release notes are in the clipboard
