# Benchmarks

Performance numbers for `pkg/whisper`. Recorded on Apple M5 Max
(darwin/arm64) with the Metal backend from bucky-builder's
`whisper-v1.9.6-bin-darwin-metal-universal.zip` on 2026-11-24 using Go 1.27.4.
The Go benchmark and
the upstream ggml/memcpy helpers all run against the same
`lib/libwhisper.dylib` installed by Bucky, reporting `1.9.6-dev`.

Artifact provenance:

- Release: https://github.com/ardanlabs/bucky-builder/releases/tag/v1.9.6
- Manifest SHA-256: `65201f945ec1c04b8a7da162675ce3f89c017f5fc5df12c9f418e70d3fbbd99a`
- Archive SHA-256: `168d533bf6d4542d3b7120c481756e97c7c83d9135ca40ef879f9a1bf9e640a8`
- Installed dylib SHA-256: `053e1158bbbc79c88f740eb1830700ed6e8c4979255d281f0288a4e846a595ac`

Reproduce with:

```
go run . install -lib "$PWD/lib" -p metal -u -q \
    -v 'v1.9.6@sha256:65201f945ec1c04b8a7da162675ce3f89c017f5fc5df12c9f418e70d3fbbd99a'
go run . model get -y -o "$HOME/models" tiny
# Run the exact commands below for each benchmark.
```

## Methodology

- **Sample**: `samples/jfk.wav` — 11.0 s, 16 kHz mono 16-bit PCM (vendored
  from upstream whisper.cpp v1.9.2)
- **Driver**: `BenchmarkFullJFK` in `pkg/whisper/benchmark_test.go`. Greedy
  sampling, single-segment, no timestamp printing. One untimed warm-up
  iteration before `b.ResetTimer()` so Metal JIT/library init does not
  pollute the measurement.
- **Reported metrics**: `ns/op` (wall time per Full call), `audio_s`
  (length of the sample in seconds), and `rtf` (real-time factor =
  wall_seconds / audio_seconds; lower is faster, < 1 is faster than
  real-time playback).

## End-to-end transcription (greedy)

| Model     | Backend | b.N |      ns/op | audio_s |      RTF |
| --------- | ------- | --: | ---------: | ------: | -------: |
| ggml-tiny | Metal   |  10 | 28,260,996 |   11.00 | 0.002569 |

Run command:

```
BUCKY_LIB=$PWD/lib \
BUCKY_BENCH_MODEL=$HOME/models/ggml-tiny.bin \
BUCKY_TEST_AUDIO=$PWD/samples/jfk.wav \
go test -count=1 -bench=BenchmarkFullJFK -benchtime=10x -run='^$' ./pkg/whisper/
```

The first untimed warm-up excludes Metal library compilation and model
initialization from the measurement; this run finished in 0.590 s overall,
with warm runs of ~28.3 ms for 11 s of audio (819 B/op, 10 allocs/op).
No compiled Core ML encoder model was installed; inference used Metal. To
record numbers across `tiny`, `base`, `small`, etc., re-run with a
different `BUCKY_BENCH_MODEL`.

## Built-in upstream micro-benchmarks

`whisper.cpp` exposes two helpers that we surface as
`whisper.BenchMemcpyStr` and `whisper.BenchGGMLMulMatStr`. These are
useful for comparing backends or hosts without loading a model.

### `whisper_bench_memcpy_str(4)` — Apple M5 Max

```
memcpy:   62.77 GB/s (heat-up)
memcpy:   70.59 GB/s ( 1 thread)
memcpy:   70.83 GB/s ( 1 thread)
memcpy:  125.31 GB/s ( 2 thread)
memcpy:  162.46 GB/s ( 3 thread)
memcpy:  173.48 GB/s ( 4 thread)
```

### `whisper_bench_ggml_mul_mat_str(4)` — selected sizes

| Size      |         Q4_0 |         Q8_0 |          F16 |          F32 |
| --------- | -----------: | -----------: | -----------: | -----------: |
| 256x256   | 122.2 GFLOPS | 248.7 GFLOPS | 148.5 GFLOPS | 142.9 GFLOPS |
| 512x512   | 142.4 GFLOPS | 370.4 GFLOPS | 186.5 GFLOPS | 176.5 GFLOPS |
| 1024x1024 | 146.2 GFLOPS | 433.3 GFLOPS | 202.3 GFLOPS | 181.9 GFLOPS |
| 2048x2048 | 149.1 GFLOPS | 421.1 GFLOPS | 205.1 GFLOPS | 166.1 GFLOPS |
| 4096x4096 | 148.2 GFLOPS | 388.7 GFLOPS | 196.0 GFLOPS | 155.6 GFLOPS |

These are fresh measurements, not a controlled before/after comparison.
F16 results are lower than the previous recorded run; the cause has not been
isolated, and both the native runtime and Go toolchain have changed.

Full output is produced by the `BenchMemcpyStr` / `BenchGGMLMulMatStr`
wrappers. The recorded values were generated with this temporary driver:

```shell
cat >/tmp/bucky-upstream-bench.go <<'EOF'
package main

import (
    "fmt"
    "log"
    "os"

    "github.com/ardanlabs/bucky/pkg/whisper"
)

func main() {
    lib := os.Getenv("BUCKY_LIB")
    if err := whisper.Load(lib); err != nil {
        log.Fatal(err)
    }
    if err := whisper.Init(lib); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("whisper.cpp %s\n", whisper.Version())
    fmt.Print(whisper.BenchMemcpyStr(4))
    fmt.Print(whisper.BenchGGMLMulMatStr(4))
}
EOF
BUCKY_LIB=$PWD/lib go run /tmp/bucky-upstream-bench.go
rm /tmp/bucky-upstream-bench.go
```

## Audio decode (pure Go, no FFI)

`pkg/audio` exposes both an allocating form (`Decode` / `DecodeWAV`) and a
buffer-reusing form (`DecodeInto` / `DecodeWAVInto`) for callers that
process many clips and want to avoid per-call allocations. The bundled
`samples/jfk.wav` (11.0 s, 16 kHz mono 16-bit PCM, ~352 KB on disk) drives
the benchmarks.

| Benchmark                |   ns/op |      B/op | allocs/op |           vs allocating |
| ------------------------ | ------: | --------: | --------: | ----------------------: |
| `BenchmarkDecodeWAV`     | 142,065 | 1,056,907 |         9 |                baseline |
| `BenchmarkDecodeWAVInto` | 116,125 |   352,392 |         8 | **-18.26% time, -66.66% mem** |
| `BenchmarkDecode`        | 140,431 | 1,057,028 |        13 |                baseline |
| `BenchmarkDecodeInto`    | 116,012 |   352,513 |        12 | **-17.39% time, -66.65% mem** |

The `Into` variants eliminate the per-call `[]float32` output allocation
(~705 KB for an 11 s clip), reducing runtime by 17–18% in this run. Percentages
are calculated as `(Into / allocating - 1) * 100` for each pair. The
remaining 352 KB is the internal `[]byte` WAV chunk read by `readWAVData`
and could be pooled in a future change if needed.

Run command:

```
BUCKY_TEST_AUDIO=$PWD/samples/jfk.wav \
    go test -count=1 -bench=. -benchtime=2s -run='^$' -benchmem ./pkg/audio/
```

## Profiling

`make profile-whisper` and `make profile-audio` capture CPU + memory
profiles for the matching benchmark and write them to `./profiles/`:

```
make profile-whisper                   # BenchmarkFullJFK + pprof artifacts
make profile-audio                     # BenchmarkDecode / BenchmarkDecodeWAV
make profile                           # both, in sequence
```

Override `PROFILE_BENCHTIME` (default `5s`, time-based) to control how
long the benchmark runs. The default is time-based on purpose: pprof
samples CPU at 10 ms granularity, so a benchmark that finishes in a
few ms produces an empty profile. Pass e.g. `PROFILE_BENCHTIME=100x` to
fall back to a fixed iteration count.

Inspect with the standard `go tool pprof` web UI:

```
go tool pprof -http=:0 profiles/whisper.cpu.prof
go tool pprof -http=:0 profiles/whisper.mem.prof
go tool pprof -http=:0 profiles/audio.cpu.prof
go tool pprof -http=:0 profiles/audio.mem.prof
```

What to expect:

- **`whisper.cpu.prof`** is dominated by `purego.SyscallN` /
  `ffi.Fun.Call` trampolines (almost all real work happens inside the
  loaded `libwhisper.dylib`, which pprof cannot see). The Go-side time
  is the FFI marshalling cost.
- **`whisper.mem.prof`** is small — `Full` itself does not allocate;
  the only Go allocations per iteration are the params struct copy and
  the `WhisperFullParams` value passed by libffi.
- **`audio.cpu.prof`** is the right place to look for real hot Go
  code (WAV header parse, `decodeWAVData`, `DownmixToMono`,
  `ResampleLinear`).
- **`audio.mem.prof`** shows the `[]float32` allocations from
  `DecodeWAV` and the resample buffer.

The captured `*.test` binaries and `*.prof` files are gitignored.
