# Benchmarks

Performance numbers for `pkg/whisper`. Recorded on Apple M5 Max
(darwin/arm64) with the Metal backend from bucky-builder's
`whisper-v1.9.2-bin-darwin-metal-universal.zip`. The Go benchmark and
the upstream ggml/memcpy helpers all run against the same
`lib/libwhisper.dylib` installed by Bucky.

Reproduce with:

```
make download-whisper.cpp VERSION=v1.9.2 # populates ./lib
make download-models                # populates ~/models
make bench                          # BUCKY_BENCH_MODEL=ggml-tiny by default
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
| ggml-tiny | Metal   |  10 | 28,971,246 |   11.00 | 0.002634 |

Run command:

```
BUCKY_LIB=$PWD/lib \
BUCKY_BENCH_MODEL=$HOME/models/ggml-tiny.bin \
BUCKY_TEST_AUDIO=$PWD/samples/jfk.wav \
go test -bench=BenchmarkFullJFK -benchtime=10x -run='^$' ./pkg/whisper/
```

The first un-timed warm-up dominates total wall time (~5–6 s) because of
Metal library compilation; warm runs are ~29 ms for 11 s of audio. To
record numbers across `tiny`, `base`, `small`, etc., re-run with a
different `BUCKY_BENCH_MODEL`.

## Built-in upstream micro-benchmarks

`whisper.cpp` exposes two helpers that we surface as
`whisper.BenchMemcpyStr` and `whisper.BenchGGMLMulMatStr`. These are
useful for comparing backends or hosts without loading a model.

### `whisper_bench_memcpy_str(4)` — Apple M5 Max

```
memcpy:   59.92 GB/s (heat-up)
memcpy:   70.04 GB/s ( 1 thread)
memcpy:   70.25 GB/s ( 1 thread)
memcpy:  120.94 GB/s ( 2 thread)
memcpy:  159.82 GB/s ( 3 thread)
memcpy:  171.06 GB/s ( 4 thread)
```

### `whisper_bench_ggml_mul_mat_str(4)` — selected sizes

| Size      |         Q4_0 |         Q8_0 |          F16 |          F32 |
| --------- | -----------: | -----------: | -----------: | -----------: |
| 256x256   | 117.5 GFLOPS | 243.2 GFLOPS | 214.9 GFLOPS | 142.2 GFLOPS |
| 512x512   | 140.8 GFLOPS | 368.4 GFLOPS | 312.1 GFLOPS | 172.6 GFLOPS |
| 1024x1024 | 138.5 GFLOPS | 361.7 GFLOPS | 355.6 GFLOPS | 175.8 GFLOPS |
| 2048x2048 | 140.8 GFLOPS | 401.6 GFLOPS | 353.3 GFLOPS | 163.0 GFLOPS |
| 4096x4096 | 142.1 GFLOPS | 372.1 GFLOPS | 317.2 GFLOPS | 149.8 GFLOPS |

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
| `BenchmarkDecodeWAV`     | 152,754 | 1,056,909 |         9 |                baseline |
| `BenchmarkDecodeWAVInto` | 128,165 |   352,393 |         8 | **-16% time, -67% mem** |
| `BenchmarkDecode`        | 151,468 | 1,057,028 |        13 |                baseline |
| `BenchmarkDecodeInto`    | 128,190 |   352,513 |        12 | **-15% time, -67% mem** |

The `Into` variants eliminate the per-call `[]float32` output allocation
(~705 KB for an 11 s clip), reducing runtime by 15–16% in this run. The
remaining 352 KB is the internal `[]byte` WAV chunk read by `readWAVData`
and could be pooled in a future change if needed.

Run command:

```
BUCKY_TEST_AUDIO=$PWD/samples/jfk.wav \
    go test -bench=. -benchtime=2s -run='^$' -benchmem ./pkg/audio/
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
