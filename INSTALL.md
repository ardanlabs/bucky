# Installing whisper.cpp libraries for bucky

bucky loads `whisper.cpp` at runtime via [purego](https://github.com/ebitengine/purego)
and [jupiterrider/ffi](https://github.com/JupiterRider/ffi) — there is **no
CGo** in this repository. That means you need a prebuilt shared library on
disk before any FFI call will succeed.

Set `BUCKY_LIB` (or pass `-lib <path>`) to the directory that contains the
shared library. The expected filenames are:

| OS              | Filename                                  |
| --------------- | ----------------------------------------- |
| linux / freebsd | `libwhisper.so`                           |
| darwin          | `libwhisper.dylib`                        |
| windows         | `whisper.dll` (plus `ggml*.dll` siblings) |

## macOS (arm64 / amd64)

```
make build
./bucky install -lib ./lib

# Authenticate the publisher manifest before installing.
./bucky install -lib ./lib -version 'v1.9.3@sha256:faefb03cc7142acfc2513257302bcdb559ea2ec5f4b2f69ff607f483396b1012'
```

Every install verifies the selected archive against its release manifest before
extraction. When no version is supplied, Bucky also authenticates that manifest
with its built-in SHA-256 pin. `bucky-builder` publishes pins for other versions
with each release.

This downloads the macOS-only universal xcframework from
[`ardanlabs/bucky-builder`](https://github.com/ardanlabs/bucky-builder) and
extracts its `macos-arm64_x86_64` Mach-O dylib into
`./lib/libwhisper.dylib`. Metal acceleration is included.

## Windows (amd64)

```
make build
.\bucky.exe install -lib .\lib
```

This downloads `whisper-vX.Y.Z-bin-windows-cpu-x64.zip` and extracts its
DLLs (`whisper.dll`, `ggml.dll`, `ggml-base.dll`, and the CPU variants).

For CUDA builds use `-p cuda`; that downloads
`whisper-vX.Y.Z-bin-windows-cuda-x64.zip` instead. The CUDA runtime DLLs
are included, so only a compatible NVIDIA driver is required on the host.

> **Windows ABI verification.** `pkg/whisper.WhisperFullParams` is sized
> assuming LLP64 with 4-byte `int` and 8-byte `size_t`/pointer — exactly
> what MSVC produces on Windows amd64. The
> [`Windows`](.github/workflows/windows.yml) GitHub Actions job runs the
> full FFI smoke on every push: `bucky install`, `bucky model get tiny`,
> `go test -count=1 ./...` (which exercises `TestWhisperFullParamsSize`,
> `TestVadParamsSize`, `TestVadContextParamsSize`, and `TestFullWithState`
> against the real `whisper.dll`), and `examples/hello samples/jfk.wav`.
> Watch the badge in [README.md](./README.md) for regressions; if you see
> `unsafe.Sizeof(WhisperFullParams) = N, want 304` with N != 304 the
> `_padN` fields in `pkg/whisper/params.go` need adjustment for the
> Windows ABI.

## Linux (amd64 / arm64)

```
make build
./bucky install -lib ./lib
```

Linux libraries are produced by the
[`ardanlabs/bucky-builder`](https://github.com/ardanlabs/bucky-builder)
companion repo. The builder checks twice daily for new whisper.cpp tags and
publishes six purpose-built Linux artifacts per release:

| Backend   | amd64                                         | arm64                                           |
| --------- | --------------------------------------------- | ----------------------------------------------- |
| CPU       | `whisper-vX.Y.Z-bin-ubuntu-cpu-x64.tar.gz`    | `whisper-vX.Y.Z-bin-ubuntu-cpu-arm64.tar.gz`    |
| CUDA 12.9 | `whisper-vX.Y.Z-bin-ubuntu-cuda-x64.tar.gz`   | `whisper-vX.Y.Z-bin-ubuntu-cuda-arm64.tar.gz`   |
| Vulkan    | `whisper-vX.Y.Z-bin-ubuntu-vulkan-x64.tar.gz` | `whisper-vX.Y.Z-bin-ubuntu-vulkan-arm64.tar.gz` |

`bucky install` auto-detects CUDA via `nvidia-smi` and downloads the
matching artifact. Pass `-p vulkan` to opt into the Vulkan build, or
`-p cpu` to force the CPU bundle. CUDA arm64 targets Jetson Orin (sm_87);
CUDA amd64 targets sm_86 + sm_89 (consumer Ampere / Ada GPUs).

The tarball unpacks `libwhisper.so`, `libggml.so`, `libggml-base.so`,
`libggml-cpu.so`, and (for cuda / vulkan variants) `libggml-cuda.so` /
`libggml-vulkan.so` into `lib/`. RPATH is `$ORIGIN`, so the libraries are
self-contained regardless of where you point `BUCKY_LIB`.

If you'd rather build whisper.cpp yourself:

```
git clone https://github.com/ggml-org/whisper.cpp.git
cd whisper.cpp
git checkout v1.9.3
cmake -B build -DBUILD_SHARED_LIBS=ON
cmake --build build --config Release -j$(nproc)
mkdir -p ../bucky/lib
cp build/src/libwhisper.so ../bucky/lib/
cp build/ggml/src/libggml*.so ../bucky/lib/
```
